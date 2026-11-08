package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"
)

type IndexerStatus struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Kind          string `json:"kind"`
	URL           string `json:"url"`
	Enabled       bool   `json:"enabled"`
	Revision      int    `json:"revision"`
	UserAgent     string `json:"user_agent"`
	FreeleechOnly bool   `json:"freeleech_only"`
	HasCookie     bool   `json:"has_cookie"`
	HasFeed       bool   `json:"has_feed"`
	HasKey        bool   `json:"has_api_key"`
	Legacy        bool   `json:"legacy"`
	LastTest      int64  `json:"last_test_at"`
	LastSearch    int64  `json:"last_search_at"`
	LastError     string `json:"last_error"`
	LastCount     int    `json:"last_result_count"`
}
type ManagedIndexerUpdate struct {
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	URL           string  `json:"url"`
	Enabled       bool    `json:"enabled"`
	Revision      int     `json:"revision"`
	UserAgent     string  `json:"user_agent"`
	FreeleechOnly bool    `json:"freeleech_only"`
	Cookie        *string `json:"cookie"`
	APIKey        *string `json:"api_key"`
	FeedURL       *string `json:"feed_url"`
}
type indexerRecord struct {
	IndexerStatus
	config IndexerConfig
}

func (s *Service) adapter(c IndexerConfig) Indexer {
	if c.Kind == "" || c.Kind == "torznab" {
		return s.indexer
	}
	return s.adapters[c.Kind]
}
func (s *Service) managedIndexer(ctx context.Context, id string) (indexerRecord, error) {
	var out indexerRecord
	var cipher []byte
	e := s.db.QueryRowContext(ctx, `SELECT id,name,kind,enabled,revision,ciphertext,generation,last_test_at,last_search_at,last_error,last_result_count FROM indexers WHERE id=?`, id).Scan(&out.ID, &out.Name, &out.Kind, &out.Enabled, &out.Revision, &cipher, &out.config.Generation, &out.LastTest, &out.LastSearch, &out.LastError, &out.LastCount)
	if e != nil {
		return out, e
	}
	raw, e := s.secrets.Open("indexer:"+id, cipher)
	if e != nil {
		return out, e
	}
	generation := out.config.Generation
	if e = json.Unmarshal(raw, &out.config); e != nil {
		return out, e
	}
	out.config.ID = id
	out.config.Name = out.Name
	out.config.Kind = out.Kind
	out.config.Generation = generation
	out.URL = out.config.URL
	out.UserAgent = out.config.UserAgent
	out.FreeleechOnly = out.config.FreeleechOnly
	out.HasCookie = out.config.Cookie != ""
	out.HasFeed = out.config.FeedURL != ""
	out.HasKey = out.config.APIKey != ""
	return out, nil
}
func (s *Service) Indexers(ctx context.Context) ([]IndexerStatus, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id FROM indexers ORDER BY name COLLATE NOCASE,id`)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []IndexerStatus{}
	if c, e := s.indexerConfig(ctx); e == nil {
		out = append(out, IndexerStatus{ID: "legacy", Name: "Torznab (existing connection)", Kind: "torznab", URL: c.URL, Enabled: true, Revision: 1, HasKey: c.APIKey != "", Legacy: true})
	} else {
		var p *Error
		if !errors.As(e, &p) || p.Code != "indexer_not_configured" {
			return nil, e
		}
	}
	for _, id := range ids {
		r, e := s.managedIndexer(ctx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, r.IndexerStatus)
	}
	return out, nil
}
func validPrivateURL(raw string) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 8192 || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || strings.ContainsAny(raw, "\r\n\x00\\") {
		return nil, problem("invalid_settings", "Enter a valid feed URL without a username, password or fragment.")
	}
	return u, nil
}
func (s *Service) SaveIndexer(ctx context.Context, id string, in ManagedIndexerUpdate) (IndexerStatus, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if !textOK(strings.TrimSpace(in.Name), 80) {
		return IndexerStatus{}, problem("invalid_settings", "Give the indexer a name with 1–80 characters.")
	}
	old := indexerRecord{}
	if id != "" {
		var e error
		old, e = s.managedIndexer(ctx, id)
		if e != nil {
			return IndexerStatus{}, e
		}
		if old.Revision != in.Revision {
			return IndexerStatus{}, problem("revision_conflict", "This indexer changed. Reload it before saving.")
		}
		if in.Kind != old.Kind {
			return IndexerStatus{}, problem("invalid_settings", "Create a new connection to change the indexer type.")
		}
	} else {
		var n int
		if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM indexers`).Scan(&n); e != nil {
			return IndexerStatus{}, e
		}
		if n >= 12 {
			return IndexerStatus{}, problem("invalid_settings", "This installation supports up to twelve indexers.")
		}
		id = ID()
	}
	u, e := endpoint(in.URL)
	if e != nil {
		return IndexerStatus{}, e
	}
	c := old.config
	c.ID = id
	c.Name = strings.TrimSpace(in.Name)
	c.Kind = in.Kind
	c.URL = u.String()
	c.Generation = ID()
	c.UserAgent = in.UserAgent
	c.FreeleechOnly = in.FreeleechOnly
	if c.Kind != "torznab" && c.Kind != "newznab" && c.Kind != "iptorrents" && c.Kind != "xspeeds" && c.Kind != "rss" && c.Kind != "nyaa" {
		return IndexerStatus{}, problem("invalid_settings", "Choose a supported indexer type.")
	}
	changed := old.URL != "" && old.URL != c.URL
	if changed && ((c.Cookie != "" && in.Cookie == nil) || (c.APIKey != "" && in.APIKey == nil) || (c.FeedURL != "" && in.FeedURL == nil)) {
		return IndexerStatus{}, problem("invalid_settings", "Re-enter secrets when changing the indexer address.")
	}
	if in.Cookie != nil {
		c.Cookie = *in.Cookie
	}
	if in.APIKey != nil {
		c.APIKey = *in.APIKey
	}
	if in.FeedURL != nil {
		c.FeedURL = *in.FeedURL
	}
	if len(c.Cookie) > 16384 || strings.ContainsAny(c.Cookie, "\r\n\x00") || len(c.APIKey) > 2048 || strings.ContainsAny(c.APIKey, "\r\n\x00") || len(c.UserAgent) > 512 || strings.ContainsAny(c.UserAgent, "\r\n\x00") {
		return IndexerStatus{}, problem("invalid_settings", "One of the credential or browser fields is invalid.")
	}
	if c.Kind == "iptorrents" || c.Kind == "xspeeds" {
		if u.Scheme != "https" || u.Path != "" || c.Cookie == "" || !textOK(c.UserAgent, 512) {
			return IndexerStatus{}, problem("invalid_settings", "Use the tracker's HTTPS homepage, saved session cookie and matching browser user agent.")
		}
		if old.UserAgent != "" && old.UserAgent != c.UserAgent && in.Cookie == nil {
			return IndexerStatus{}, problem("invalid_settings", "Re-enter the session cookie when changing its browser user agent.")
		}
	}
	if c.Kind == "nyaa" {
		if u.Scheme != "https" || u.Path != "" || c.Cookie != "" || c.APIKey != "" || c.FeedURL != "" {
			return IndexerStatus{}, problem("invalid_settings", "NYAA uses its HTTPS homepage without credentials.")
		}
	}
	if c.Kind == "rss" {
		if u.Path != "" {
			return IndexerStatus{}, problem("invalid_settings", "Use the website homepage above and put the complete private URL in the feed field.")
		}
		feed, e := validPrivateURL(c.FeedURL)
		if e != nil {
			return IndexerStatus{}, e
		}
		if !sameOrigin(u, feed) {
			return IndexerStatus{}, problem("invalid_settings", "The private feed must belong to the configured website origin.")
		}
	}
	if ((c.Kind != "torznab" && c.Kind != "newznab") && c.APIKey != "") || (c.Kind != "rss" && c.FeedURL != "") || ((c.Kind == "torznab" || c.Kind == "newznab") && (c.Cookie != "" || c.FreeleechOnly || c.UserAgent != "")) {
		return IndexerStatus{}, problem("invalid_settings", "Only enter credentials supported by the selected indexer type.")
	}
	if a := s.adapter(c); a == nil {
		return IndexerStatus{}, problem("invalid_settings", "This adapter is unavailable.")
	} else if e = a.Test(ctx, c); e != nil {
		return IndexerStatus{}, e
	}
	raw, e := json.Marshal(c)
	if e != nil {
		return IndexerStatus{}, e
	}
	sealed, e := s.secrets.Seal("indexer:"+id, raw)
	if e != nil {
		return IndexerStatus{}, e
	}
	now := time.Now().Unix()
	if old.ID == "" {
		_, e = s.db.ExecContext(ctx, `INSERT INTO indexers(id,name,kind,enabled,generation,ciphertext,updated_at,last_test_at) VALUES(?,?,?,?,?,?,?,?)`, id, c.Name, c.Kind, in.Enabled, c.Generation, sealed, now, now)
	} else {
		var res sql.Result
		res, e = s.db.ExecContext(ctx, `UPDATE indexers SET name=?,enabled=?,revision=revision+1,generation=?,ciphertext=?,updated_at=?,last_test_at=?,last_error='' WHERE id=? AND revision=?`, c.Name, in.Enabled, c.Generation, sealed, now, now, id, in.Revision)
		if e == nil {
			n, _ := res.RowsAffected()
			if n != 1 {
				e = problem("revision_conflict", "This indexer changed. Reload before saving.")
			}
		}
	}
	if e != nil {
		return IndexerStatus{}, e
	}
	saved, e := s.managedIndexer(ctx, id)
	return saved.IndexerStatus, e
}
func (s *Service) DeleteIndexer(ctx context.Context, id string, revision int) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	if id == "legacy" {
		return s.secrets.DeleteNamed(ctx, "torznab")
	}
	res, e := s.db.ExecContext(ctx, `DELETE FROM indexers WHERE id=? AND revision=?`, id, revision)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return problem("revision_conflict", "This indexer changed or was removed. Reload the list.")
	}
	return nil
}
func (s *Service) SetIndexerEnabled(ctx context.Context, id string, revision int, enabled bool) (IndexerStatus, error) {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	res, e := s.db.ExecContext(ctx, `UPDATE indexers SET enabled=?,revision=revision+1,generation=?,updated_at=? WHERE id=? AND revision=?`, enabled, ID(), time.Now().Unix(), id, revision)
	if e != nil {
		return IndexerStatus{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return IndexerStatus{}, problem("revision_conflict", "This indexer changed. Reload before updating it.")
	}
	r, e := s.managedIndexer(ctx, id)
	return r.IndexerStatus, e
}
func (s *Service) TestIndexer(ctx context.Context, id string) error {
	if id == "legacy" {
		return s.Test(ctx, "torznab")
	}
	r, e := s.managedIndexer(ctx, id)
	if e != nil {
		return e
	}
	a := s.adapter(r.config)
	if a == nil {
		return problem("invalid_settings", "This adapter is unavailable.")
	}
	e = a.Test(ctx, r.config)
	message := ""
	if e != nil {
		message = safeIndexerError(e)
	}
	_, save := s.db.ExecContext(ctx, `UPDATE indexers SET last_test_at=?,last_error=? WHERE id=? AND generation=?`, time.Now().Unix(), message, id, r.config.Generation)
	if e != nil {
		return e
	}
	return save
}
func safeIndexerError(e error) string {
	var p *Error
	if errors.As(e, &p) {
		return p.Message
	}
	return "The indexer request could not be completed."
}
func (s *Service) candidateIndexer(ctx context.Context, v Candidate) (IndexerConfig, error) {
	if v.IndexerID == "" {
		return s.indexerConfig(ctx)
	}
	r, e := s.managedIndexer(ctx, v.IndexerID)
	if errors.Is(e, sql.ErrNoRows) || (e == nil && !r.Enabled) {
		return IndexerConfig{}, problem("release_expired", "The source indexer was disabled or removed. Search again.")
	}
	return r.config, e
}
func (s *Service) searchIndexers(ctx context.Context, query, category string) ([]Candidate, error) {
	configs, e := s.enabledIndexerConfigs(ctx)
	if e != nil {
		return nil, e
	}
	for i := range configs {
		configs[i].Category = category
	}
	if len(configs) == 0 {
		return nil, problem("indexer_not_configured", "Add and enable an indexer in Indexers first.")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	type result struct {
		items []Candidate
		err   error
	}
	results := make([]result, len(configs))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 4)
	for i, c := range configs {
		wg.Add(1)
		go func(i int, c IndexerConfig) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				results[i].err = unavailable("The indexer search timed out.")
				return
			}
			a := s.adapter(c)
			if a == nil {
				results[i].err = problem("invalid_settings", "This adapter is unavailable.")
				return
			}
			results[i].items, results[i].err = a.Search(ctx, c, query)
		}(i, c)
	}
	wg.Wait()
	out := []Candidate{}
	successful := 0
	var first error
	updateCtx, finishUpdates := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer finishUpdates()
	for i, r := range results {
		c := configs[i]
		if r.err != nil {
			if first == nil {
				first = r.err
			}
		} else {
			successful++
		}
		if c.ID == "" {
			s.healthMu.Lock()
			s.legacyError = ""
			if r.err != nil {
				s.legacyError = safeIndexerError(r.err)
			}
			s.healthMu.Unlock()
		} else {
			message := ""
			if r.err != nil {
				message = safeIndexerError(r.err)
			}
			if _, e := s.db.ExecContext(updateCtx, `UPDATE indexers SET last_search_at=?,last_error=?,last_result_count=? WHERE id=? AND generation=?`, time.Now().Unix(), message, len(r.items), c.ID, c.Generation); e != nil {
				return nil, e
			}
		}
	}
	if successful == 0 {
		return nil, first
	}
	for position := 0; position < 100 && len(out) < 200; position++ {
		for i, r := range results {
			if r.err != nil || position >= len(r.items) {
				continue
			}
			v := r.items[position]
			v.IndexerID = configs[i].ID
			v.IndexerName = configs[i].Name
			v.Generation = configs[i].Generation
			v.Protocol = "torrent"
			if configs[i].Kind == "newznab" {
				v.Protocol = "usenet"
			}
			out = append(out, v)
			if len(out) == 200 {
				break
			}
		}
	}
	return out, nil
}
func (s *Service) IndexerWarnings(ctx context.Context) []string {
	rows, e := s.db.QueryContext(ctx, `SELECT name,last_error FROM indexers WHERE enabled=1 AND last_error<>'' ORDER BY name LIMIT 12`)
	if e != nil {
		return []string{"Some indexer health information is unavailable."}
	}
	defer rows.Close()
	out := []string{}
	s.healthMu.RLock()
	legacyError := s.legacyError
	s.healthMu.RUnlock()
	if legacyError != "" {
		out = append(out, "Existing Torznab connection: "+legacyError)
	}
	for rows.Next() {
		var name, message string
		if rows.Scan(&name, &message) == nil {
			out = append(out, name+": "+message)
		}
	}
	return out
}
