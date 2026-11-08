package requests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

var listSlug = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)
var listNumber = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)
var traktClientID = regexp.MustCompile(`^[A-Za-z0-9]{20,200}$`)
var mdblistKey = regexp.MustCompile(`^[A-Za-z0-9_-]{16,256}$`)

func validPrivateToken(token string) bool {
	return len(token) >= 20 && len(token) <= 4096 && regexp.MustCompile(`^[A-Za-z0-9._~-]+$`).MatchString(token)
}

var ErrList = errors.New("Choose a valid list and provider credentials.")

type ImportList struct {
	ID          int64  `json:"id"`
	Provider    string `json:"provider"`
	Owner       string `json:"owner"`
	ListID      string `json:"list_id"`
	Enabled     bool   `json:"enabled"`
	LastError   string `json:"last_error"`
	LastChecked int64  `json:"last_checked"`
}
type Lists struct {
	db      *sql.DB
	secrets *securestore.Store
	client  *http.Client
}
type listTitle struct {
	Kind  string
	ID    int64
	Title string
}

func NewLists(db *sql.DB, secrets *securestore.Store) *Lists {
	return &Lists{db: db, secrets: secrets, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirects disabled") }}}
}
func (s *Lists) All(ctx context.Context, account int64) ([]ImportList, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,provider,owner,list_id,enabled,last_error,last_checked FROM request_lists WHERE account_id=? ORDER BY id`, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImportList{}
	for rows.Next() {
		var v ImportList
		if err = rows.Scan(&v.ID, &v.Provider, &v.Owner, &v.ListID, &v.Enabled, &v.LastError, &v.LastChecked); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Lists) Add(ctx context.Context, account int64, provider, owner, id, key string, accessToken ...string) (ImportList, error) {
	base := strings.TrimSuffix(provider, "_private")
	private := base != provider
	token := ""
	if len(accessToken) > 0 {
		token = accessToken[0]
	}
	if private && (!validPrivateToken(token) || base != "tmdb" && base != "trakt" && base != "trakt_watchlist") {
		return ImportList{}, ErrList
	}
	if !private && token != "" {
		return ImportList{}, ErrList
	}
	if account < 1 || (base != "tmdb" && base != "trakt" && base != "imdb_mdblist" && base != "trakt_watchlist") || (base == "tmdb" && (!listNumber.MatchString(id) || owner != "" || key != "")) || (base == "trakt" && (!listSlug.MatchString(owner) || !listSlug.MatchString(id) || !traktClientID.MatchString(key))) || (base == "imdb_mdblist" && (!listNumber.MatchString(id) || owner != "" || !mdblistKey.MatchString(key))) || (base == "trakt_watchlist" && (!listSlug.MatchString(owner) || !traktClientID.MatchString(key))) {
		return ImportList{}, ErrList
	}
	if base == "trakt_watchlist" {
		id = "watchlist"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ImportList{}, err
	}
	defer tx.Rollback()
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_accounts WHERE id=? AND disabled=0`, account).Scan(&n); err != nil {
		return ImportList{}, err
	}
	if n != 1 {
		return ImportList{}, sql.ErrNoRows
	}
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_lists WHERE account_id=?`, account).Scan(&n); err != nil {
		return ImportList{}, err
	}
	if n >= 10 {
		return ImportList{}, ErrList
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO request_lists(account_id,provider,owner,list_id,enabled) VALUES(?,?,?,?,1)`, account, provider, owner, id)
	if err != nil {
		return ImportList{}, ErrList
	}
	rowID, err := result.LastInsertId()
	if err != nil {
		return ImportList{}, err
	}
	if private {
		if base == "tmdb" {
			key = token
		} else {
			raw, _ := json.Marshal(privateListCredentials{ClientID: key, AccessToken: token})
			key = string(raw)
		}
	}
	if key != "" {
		sealed, e := s.secrets.Seal(fmt.Sprintf("request-list:%d", rowID), []byte(key))
		if e != nil {
			return ImportList{}, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE request_lists SET ciphertext=? WHERE id=?`, sealed, rowID); e != nil {
			return ImportList{}, e
		}
	}
	if err = tx.Commit(); err != nil {
		return ImportList{}, err
	}
	return ImportList{ID: rowID, Provider: provider, Owner: owner, ListID: id, Enabled: true}, nil
}
func (s *Lists) Delete(ctx context.Context, account, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM request_lists WHERE id=? AND account_id=?`, id, account)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}
func (s *Lists) Refresh(ctx context.Context, account, id int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE request_lists SET next_check=0,revision=revision+1,lease_until=0 WHERE id=? AND account_id=? AND enabled=1`, id, account)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return nil
}

type privateListCredentials struct {
	ClientID    string `json:"client_id"`
	AccessToken string `json:"access_token"`
}

func (s *Lists) fetch(ctx context.Context, provider, owner, id, key string) ([]listTitle, error) {
	private := strings.HasSuffix(provider, "_private")
	provider = strings.TrimSuffix(provider, "_private")
	var token string
	if private {
		if provider == "tmdb" {
			token = key
		} else {
			var c privateListCredentials
			if json.Unmarshal([]byte(key), &c) != nil {
				return nil, ErrList
			}
			key = c.ClientID
			token = c.AccessToken
		}
		if !validPrivateToken(token) {
			return nil, ErrList
		}
	}
	if provider == "imdb_mdblist" {
		if owner != "" || !listNumber.MatchString(id) || !mdblistKey.MatchString(key) {
			return nil, ErrList
		}
		return s.fetchIMDbMDBList(ctx, id, key)
	}
	if provider == "trakt_watchlist" {
		if !listSlug.MatchString(owner) || !traktClientID.MatchString(key) {
			return nil, ErrList
		}
		return s.fetchTraktWatchlist(ctx, owner, key, token)
	}
	if provider == "tmdb" {
		if !listNumber.MatchString(id) || owner != "" || len(key) < 20 || len(key) > 4096 {
			return nil, ErrList
		}
	} else if provider == "trakt" {
		if !listSlug.MatchString(owner) || !listSlug.MatchString(id) || !traktClientID.MatchString(key) {
			return nil, ErrList
		}
	} else {
		return nil, ErrList
	}
	items := []listTitle{}
	for page := 1; page <= 10; page++ {
		var endpoint string
		if provider == "tmdb" {
			endpoint = "https://api.themoviedb.org/4/list/" + id + "?page=" + strconv.Itoa(page)
		} else {
			endpoint = "https://api.trakt.tv/users/" + url.PathEscape(owner) + "/lists/" + url.PathEscape(id) + "/items?limit=100&page=" + strconv.Itoa(page)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, ErrList
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "ReelScarey/0.20")
		if provider == "tmdb" {
			req.Header.Set("Authorization", "Bearer "+key)
		} else {
			req.Header.Set("trakt-api-key", key)
			req.Header.Set("trakt-api-version", "2")
			if private {
				req.Header.Set("Authorization", "Bearer "+token)
			}
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, errors.New("The list provider could not be reached.")
		}
		body, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
		resp.Body.Close()
		if e != nil || len(body) > 2<<20 {
			return nil, errors.New("The list provider returned an unreadable response.")
		}
		if resp.StatusCode != 200 {
			return nil, errors.New("The list is unavailable. Check visibility, access-token expiry and provider settings.")
		}
		last := false
		if provider == "tmdb" {
			var data struct {
				Public     bool `json:"public"`
				Page       int  `json:"page"`
				TotalPages int  `json:"total_pages"`
				Results    []struct {
					ID        int64  `json:"id"`
					MediaType string `json:"media_type"`
					Title     string `json:"title"`
					Name      string `json:"name"`
				} `json:"results"`
			}
			if json.Unmarshal(body, &data) != nil || (!data.Public && !private) || data.Page != page || data.TotalPages < 0 || data.TotalPages > 10 || len(data.Results) > 100 {
				return nil, errors.New("The TMDb list response could not be verified.")
			}
			for _, v := range data.Results {
				kind, title := v.MediaType, v.Title
				if kind == "tv" {
					kind = "series"
					title = v.Name
				}
				if kind != "movie" && kind != "series" {
					continue
				}
				if v.ID < 1 || strings.TrimSpace(title) == "" {
					return nil, errors.New("The list contains an invalid title.")
				}
				items = append(items, listTitle{kind, v.ID, title})
			}
			last = page >= data.TotalPages
		} else {
			var data []struct {
				Type  string `json:"type"`
				Movie *struct {
					Title string `json:"title"`
					IDs   struct {
						TMDb int64 `json:"tmdb"`
					} `json:"ids"`
				} `json:"movie"`
				Show *struct {
					Title string `json:"title"`
					IDs   struct {
						TMDb int64 `json:"tmdb"`
					} `json:"ids"`
				} `json:"show"`
			}
			if json.Unmarshal(body, &data) != nil || len(data) > 100 {
				return nil, errors.New("The Trakt list response could not be verified.")
			}
			for _, v := range data {
				if v.Type == "movie" && v.Movie != nil {
					items = append(items, listTitle{"movie", v.Movie.IDs.TMDb, v.Movie.Title})
				} else if v.Type == "show" && v.Show != nil {
					items = append(items, listTitle{"series", v.Show.IDs.TMDb, v.Show.Title})
				}
			}
			pages, e := strconv.Atoi(resp.Header.Get("X-Pagination-Page-Count"))
			if e != nil || pages < 1 || pages > 10 {
				return nil, errors.New("The Trakt list pagination could not be verified.")
			}
			last = page >= pages
		}
		if len(items) > 200 {
			return nil, errors.New("A list can contain at most 200 movies and shows.")
		}
		for _, v := range items {
			if v.ID < 1 || len(strings.TrimSpace(v.Title)) == 0 || len(v.Title) > 400 {
				return nil, errors.New("A list item is missing a TMDb identity.")
			}
		}
		if last {
			return items, nil
		}
	}
	return nil, errors.New("The list exceeds the supported page limit.")
}

func (s *Lists) fetchTraktWatchlist(ctx context.Context, owner, key, token string) ([]listTitle, error) {
	items := []listTitle{}
	seen := map[string]bool{}
	for _, media := range []string{"movies", "shows"} {
		for page := 1; page <= 10; page++ {
			endpoint := "https://api.trakt.tv/users/" + url.PathEscape(owner) + "/watchlist/" + media + "/added?limit=100&page=" + strconv.Itoa(page)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
			if err != nil {
				return nil, ErrList
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "ReelScarey/0.20")
			req.Header.Set("trakt-api-key", key)
			req.Header.Set("trakt-api-version", "2")
			if token != "" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			resp, err := s.client.Do(req)
			if err != nil {
				return nil, errors.New("The Trakt watchlist could not be reached.")
			}
			body, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20+1))
			resp.Body.Close()
			if e != nil || len(body) > 2<<20 {
				return nil, errors.New("The Trakt watchlist returned an unreadable response.")
			}
			if resp.StatusCode != 200 {
				return nil, errors.New("The Trakt watchlist is unavailable. Check the profile visibility and client ID.")
			}
			var data []struct {
				Type  string `json:"type"`
				Movie *struct {
					Title string `json:"title"`
					IDs   struct {
						TMDb int64 `json:"tmdb"`
					} `json:"ids"`
				} `json:"movie"`
				Show *struct {
					Title string `json:"title"`
					IDs   struct {
						TMDb int64 `json:"tmdb"`
					} `json:"ids"`
				} `json:"show"`
			}
			if json.Unmarshal(body, &data) != nil || len(data) > 100 {
				return nil, errors.New("The Trakt watchlist response could not be verified.")
			}
			for _, v := range data {
				var item listTitle
				if v.Type == "movie" && v.Movie != nil {
					item = listTitle{"movie", v.Movie.IDs.TMDb, v.Movie.Title}
				} else if v.Type == "show" && v.Show != nil {
					item = listTitle{"series", v.Show.IDs.TMDb, v.Show.Title}
				} else {
					continue
				}
				if item.ID < 1 || len(strings.TrimSpace(item.Title)) == 0 || len(item.Title) > 400 {
					return nil, errors.New("A watchlist item is missing a TMDb identity.")
				}
				itemKey := fmt.Sprintf("%s:%d", item.Kind, item.ID)
				if seen[itemKey] {
					continue
				}
				seen[itemKey] = true
				items = append(items, item)
				if len(items) >= 200 {
					return items, nil
				}
			}
			pages, e := strconv.Atoi(resp.Header.Get("X-Pagination-Page-Count"))
			if e != nil || pages < 0 || pages > 10 {
				return nil, errors.New("The Trakt watchlist pagination could not be verified.")
			}
			if pages < 1 || page >= pages {
				break
			}
		}
	}
	return items, nil
}

func (s *Lists) Tick(ctx context.Context) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id, account, revision int64
	var provider, owner, listID string
	var sealed []byte
	err = tx.QueryRowContext(ctx, `SELECT l.id,l.account_id,l.revision,l.provider,l.owner,l.list_id,l.ciphertext FROM request_lists l JOIN request_accounts a ON a.id=l.account_id AND a.disabled=0 WHERE l.enabled=1 AND l.next_check<=unixepoch() AND l.lease_until<=unixepoch() ORDER BY l.next_check,l.id LIMIT 1`).Scan(&id, &account, &revision, &provider, &owner, &listID, &sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	lease := time.Now().Add(3 * time.Minute).Unix()
	if _, err = tx.ExecContext(ctx, `UPDATE request_lists SET lease_until=? WHERE id=? AND revision=?`, lease, id, revision); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	var key string
	if provider == "tmdb" {
		key, err = s.secrets.Get(ctx)
		if err == nil && key == "" {
			err = errors.New("The administrator must connect TMDb before importing this list.")
		}
	} else {
		var plain []byte
		plain, err = s.secrets.Open(fmt.Sprintf("request-list:%d", id), sealed)
		key = string(plain)
		if err != nil {
			err = errors.New("List provider credentials could not be read.")
		}
	}
	var titles []listTitle
	if err == nil {
		titles, err = s.fetch(ctx, provider, owner, listID, key)
	}
	if err != nil {
		_, saveErr := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE request_lists SET last_error=?,last_checked=unixepoch(),next_check=unixepoch()+3600,lease_until=0 WHERE id=? AND revision=? AND lease_until=?`, err.Error(), id, revision, lease)
		return true, saveErr
	}
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		return true, err
	}
	defer tx.Rollback()
	var allowed int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_lists l JOIN request_accounts a ON a.id=l.account_id AND a.disabled=0 WHERE l.id=? AND l.account_id=? AND l.revision=? AND l.lease_until=? AND l.enabled=1`, id, account, revision, lease).Scan(&allowed); err != nil {
		return true, err
	}
	if allowed != 1 {
		return true, nil
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_requests WHERE account_id=?`, account).Scan(&count); err != nil {
		return true, err
	}
	newTitles := 0
	seen := map[string]bool{}
	for _, v := range titles {
		key := fmt.Sprintf("%s:%d", v.Kind, v.ID)
		if seen[key] {
			continue
		}
		seen[key] = true
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_requests WHERE account_id=? AND kind=? AND tmdb_id=?`, account, v.Kind, v.ID).Scan(&exists); err != nil {
			return true, err
		}
		if exists == 0 {
			newTitles++
		}
	}
	if count+newTitles > 1000 {
		_, err = tx.ExecContext(ctx, `UPDATE request_lists SET last_error='Request limit reached.',last_checked=unixepoch(),next_check=unixepoch()+3600,lease_until=0 WHERE id=?`, id)
		if err != nil {
			return true, err
		}
		return true, tx.Commit()
	}
	for _, v := range titles {
		if _, err = tx.ExecContext(ctx, `INSERT INTO media_requests(account_id,kind,tmdb_id,title,created_at,updated_at) VALUES(?,?,?,?,unixepoch(),unixepoch()) ON CONFLICT(account_id,kind,tmdb_id) DO NOTHING`, account, v.Kind, v.ID, strings.TrimSpace(v.Title)); err != nil {
			return true, err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE request_lists SET last_error='',last_checked=unixepoch(),next_check=unixepoch()+86400,lease_until=0 WHERE id=?`, id); err != nil {
		return true, err
	}
	return true, tx.Commit()
}
func (s *Lists) Run(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		for ctx.Err() == nil {
			tickCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			did, err := s.Tick(tickCtx)
			cancel()
			if err != nil || !did {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
