package discordbot

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/requests"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type button struct {
	Type     int    `json:"type"`
	Style    int    `json:"style"`
	Label    string `json:"label"`
	CustomID string `json:"custom_id"`
}
type reply struct {
	Content         string           `json:"content"`
	Components      []map[string]any `json:"components,omitempty"`
	AllowedMentions map[string]any   `json:"allowed_mentions"`
}

func message(text string, buttons []button) reply {
	var components []map[string]any
	if len(buttons) > 0 {
		components = []map[string]any{{"type": 1, "components": buttons}}
	}
	return reply{Content: text, Components: components, AllowedMentions: map[string]any{"parse": []string{}}}
}
func label(text string) string {
	text = strings.TrimSpace(text)
	runes := []rune(text)
	if len(runes) > 80 {
		return string(runes[:79]) + "…"
	}
	if text == "" {
		return "Choose"
	}
	return text
}
func randomChoice() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// noticeRequeueCap bounds how many times a rate-limited notice is requeued
// before it is parked in the terminal uncertain state (NEW-C1).
const noticeRequeueCap = 5

// RateLimitError reports that Discord answered 429 with a Retry-After hint.
// Callers distinguish a temporary rejection from a permanent one (NEW-C1).
type RateLimitError struct {
	RetryAfter int
}

func (e *RateLimitError) Error() string { return "Discord is rate limiting requests. Try again later." }

// parseRetryAfter reads a seconds or HTTP-date Retry-After hint, clamped to a
// conservative [1,300] second window.
func parseRetryAfter(value string) int {
	seconds, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		if when, parseErr := http.ParseTime(value); parseErr == nil {
			seconds = int(time.Until(when).Seconds())
		}
	}
	return min(300, max(1, seconds))
}

// waitOut delays for at most five seconds of a provider-provided Retry-After
// hint and returns early when the context is cancelled.
func (s *Service) waitOut(ctx context.Context, seconds int) {
	delay := time.Duration(min(5, seconds)) * time.Second
	if delay <= 0 {
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (s *Service) send(ctx context.Context, method, endpoint, authorization string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://discord.com/api/v10"+endpoint, bytes.NewReader(raw))
	if err != nil {
		return errors.New("Discord request could not be prepared.")
	}
	req.Header.Set("Content-Type", "application/json")
	if authorization != "" {
		req.Header.Set("Authorization", "Bot "+authorization)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return errors.New("Discord request outcome is uncertain.")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			return &RateLimitError{RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
		}
		return errors.New("Discord did not accept the request.")
	}
	return nil
}

// Register creates or updates only this app's named commands in the selected
// server; it never replaces the server's entire command collection.
func (s *Service) Register(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.Config(ctx)
	if err != nil || !c.Configured {
		return ErrConfig
	}
	secret, err := s.secret(ctx)
	if err != nil {
		return err
	}
	commands := []map[string]any{
		{"name": "request", "description": "Find a movie or show to request", "type": 1, "options": []map[string]any{{"type": 3, "name": "kind", "description": "Movie or series", "required": true, "choices": []map[string]string{{"name": "Movie", "value": "movie"}, {"name": "TV show", "value": "series"}}}, {"type": 3, "name": "query", "description": "Title to search", "required": true, "max_length": 200}}},
		{"name": "releases", "description": "Find releases for an approved request", "type": 1, "options": []map[string]any{{"type": 4, "name": "request_id", "description": "Your approved request ID", "required": true, "min_value": 1}, {"type": 4, "name": "season", "description": "TV season number", "required": false, "min_value": 0, "max_value": 999}, {"type": 4, "name": "episode", "description": "TV episode number (zero for a season)", "required": false, "min_value": 0, "max_value": 9999}}},
	}
	for _, command := range commands {
		if err = s.send(ctx, http.MethodPost, "/applications/"+c.ApplicationID+"/guilds/"+c.GuildID+"/commands", secret.Token, command); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ProcessOne(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id, actor, kind, raw string
	var created int64
	var account sql.NullInt64
	var sealed []byte
	err = tx.QueryRowContext(ctx, `SELECT id,account_id,actor_id,kind,payload,ciphertext,created_at FROM discord_bot_jobs WHERE state='queued' ORDER BY created_at,id LIMIT 1`).Scan(&id, &account, &actor, &kind, &raw, &sealed, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE discord_bot_jobs SET state='running',updated_at=unixepoch() WHERE id=? AND state='queued'`, id); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	if created < time.Now().Add(-14*time.Minute).Unix() {
		_ = s.finishJob(ctx, id, "uncertain")
		return true, errors.New("Discord interaction expired before processing.")
	}
	c, err := s.Config(ctx)
	if err != nil || !c.Configured {
		_ = s.finishJob(ctx, id, "uncertain")
		return true, errors.New("Discord bot is not configured.")
	}
	token, err := s.secrets.Open("discord-job:"+id, sealed)
	if err != nil {
		_ = s.finishJob(ctx, id, "uncertain")
		return true, errors.New("Interaction token could not be read.")
	}
	var payload jobPayload
	if json.Unmarshal([]byte(raw), &payload) != nil {
		_ = s.finishJob(ctx, id, "uncertain")
		return true, errors.New("Interaction could not be read.")
	}
	var accountID int64
	if account.Valid {
		accountID = account.Int64
	}
	if actor != c.AdminUserID {
		var n int
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM request_accounts WHERE id=? AND discord_user_id=? AND disabled=0`, accountID, actor).Scan(&n)
		if err != nil || n != 1 {
			_ = s.finishJob(ctx, id, "uncertain")
			return true, errors.New("Discord account link is no longer valid.")
		}
	}
	response, err := s.process(ctx, c, actor, accountID, kind, payload)
	if err != nil {
		response = message("This action could not be completed. Check ReelScarey and try again.", nil)
	}
	endpoint := "/webhooks/" + c.ApplicationID + "/" + url.PathEscape(string(token)) + "/messages/@original"
	if e := s.send(ctx, http.MethodPatch, endpoint, "", response); e != nil {
		// A 429 is temporary and re-editing the interaction reply is safe, so
		// wait out the hint and retry once inside the fourteen-minute
		// interaction window instead of discarding the answer (NEW-C1).
		var limited *RateLimitError
		if errors.As(e, &limited) {
			s.waitOut(ctx, limited.RetryAfter)
			e = s.send(ctx, http.MethodPatch, endpoint, "", response)
		}
		if e != nil {
			_ = s.finishJob(context.WithoutCancel(ctx), id, "uncertain")
			return true, e
		}
	}
	// The terminal sent write must survive context cancellation like the
	// 429 path above: losing it would turn a delivered reply into a
	// duplicate post on the next manual retry.
	return true, s.finishJob(context.WithoutCancel(ctx), id, "sent")
}
func (s *Service) finishJob(ctx context.Context, id, state string) error {
	sealed, err := s.secrets.Seal("discord-job:"+id, []byte(""))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE discord_bot_jobs SET state=?,ciphertext=?,updated_at=unixepoch() WHERE id=? AND state='running'`, state, sealed, id)
	return err
}

func (s *Service) process(ctx context.Context, c Config, actor string, account int64, kind string, p jobPayload) (reply, error) {
	switch kind {
	case "search":
		if s.metadata == nil || account < 1 {
			return reply{}, ErrConfig
		}
		type title struct {
			ID   int64
			Name string
		}
		var titles []title
		if p.Kind == "movie" {
			out, err := s.metadata.Search(ctx, p.Query, 1)
			if err != nil {
				return reply{}, err
			}
			for _, v := range out.Items {
				titles = append(titles, title{v.TMDbID, v.Title})
			}
		} else {
			out, err := s.metadata.SearchSeries(ctx, p.Query, 1)
			if err != nil {
				return reply{}, err
			}
			for _, v := range out.Items {
				titles = append(titles, title{v.TMDbID, v.Title})
			}
		}
		if len(titles) > 5 {
			titles = titles[:5]
		}
		buttons := []button{}
		for _, v := range titles {
			if v.ID < 1 || strings.TrimSpace(v.Name) == "" {
				continue
			}
			id, err := randomChoice()
			if err != nil {
				return reply{}, err
			}
			data, _ := json.Marshal(jobPayload{Kind: p.Kind, TMDbID: v.ID})
			if _, err = s.db.ExecContext(ctx, `INSERT INTO discord_bot_choices(id,actor_id,account_id,kind,payload,expires_at) VALUES(?,?,?,'request',?,unixepoch()+900)`, id, actor, account, string(data)); err != nil {
				return reply{}, err
			}
			buttons = append(buttons, button{Type: 2, Style: 1, Label: label(v.Name), CustomID: "request:" + id})
		}
		if len(buttons) == 0 {
			return message("No matching titles were found.", nil), nil
		}
		return message("Choose a title to request:", buttons), nil
	case "request":
		if s.metadata == nil || account < 1 {
			return reply{}, ErrConfig
		}
		var title string
		if p.Kind == "movie" {
			v, err := s.metadata.Movie(ctx, p.TMDbID)
			if err != nil || v.TMDbID != p.TMDbID {
				return reply{}, ErrConfig
			}
			title = v.Title
		} else if p.Kind == "series" {
			v, err := s.metadata.Series(ctx, p.TMDbID)
			if err != nil || v.TMDbID != p.TMDbID {
				return reply{}, ErrConfig
			}
			title = v.Title
		} else {
			return reply{}, ErrConfig
		}
		item, err := s.requests.Create(ctx, account, p.Kind, p.TMDbID, title)
		if err != nil {
			return reply{}, err
		}
		return message(fmt.Sprintf("Request %d: %s (%s). An administrator can review it.", item.ID, item.Title, item.State), nil), nil
	case "approve":
		if actor != c.AdminUserID || s.metadata == nil {
			return reply{}, ErrConfig
		}
		item, err := s.requests.Claim(ctx, p.RequestID)
		if err != nil {
			return reply{}, err
		}
		if item.State != "approved" {
			if item.Kind == "movie" {
				v, e := s.metadata.Movie(ctx, item.TMDbID)
				if e != nil || v.TMDbID != item.TMDbID {
					return reply{}, ErrConfig
				}
				if _, _, e = s.library.AddMovie(ctx, v, true); e != nil {
					return reply{}, e
				}
			} else {
				v, e := s.metadata.Series(ctx, item.TMDbID)
				if e != nil || v.TMDbID != item.TMDbID {
					return reply{}, ErrConfig
				}
				if _, _, e = s.library.AddSeries(ctx, v, true); e != nil {
					return reply{}, e
				}
			}
			if err = s.requests.Finish(ctx, item.ID); err != nil {
				return reply{}, err
			}
		}
		if item.Kind == "series" {
			return message("Request approved. Use /releases with its request ID and a loaded season to choose a release.", nil), nil
		}
		out, e := s.releaseReply(ctx, c, actor, account, item, 0, 0)
		if e != nil {
			return message("Request approved, but release search failed. Use /releases to try again.", nil), nil
		}
		return out, nil
	case "releases":
		item, err := s.requests.Get(ctx, p.RequestID)
		if err != nil || item.State != "approved" || (actor != c.AdminUserID && item.AccountID != account) {
			return reply{}, ErrConfig
		}
		return s.releaseReply(ctx, c, actor, account, item, p.Season, p.Episode)
	case "grab":
		if s.transfer == nil {
			return reply{}, ErrConfig
		}
		_, created, err := s.transfer.Queue(ctx, p.ReleaseID)
		if err != nil {
			return reply{}, err
		}
		if created {
			return message("Release queued in ReelScarey.", nil), nil
		}
		return message("This release was already queued.", nil), nil
	}
	return reply{}, ErrConfig
}

func (s *Service) releaseReply(ctx context.Context, c Config, actor string, account int64, item requests.Request, season, episode int) (reply, error) {
	if s.transfer == nil {
		return reply{}, ErrConfig
	}
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM media_items WHERE kind=? AND tmdb_id=?`, item.Kind, item.TMDbID).Scan(&id); err != nil {
		return reply{}, err
	}
	var releases []transfer.Release
	var err error
	if item.Kind == "movie" {
		releases, err = s.transfer.Search(ctx, id, "")
	} else {
		releases, err = s.transfer.SearchTV(ctx, id, season, episode, "")
	}
	if err != nil {
		return reply{}, err
	}
	if len(releases) > 5 {
		releases = releases[:5]
	}
	secret, err := s.secret(ctx)
	if err != nil {
		return reply{}, err
	}
	key, err := base64.RawURLEncoding.DecodeString(secret.SelectionKey)
	if err != nil {
		return reply{}, err
	}
	buttons := []button{}
	for _, v := range releases {
		signed, e := SignSelection(key, v.ID, actor, time.Now().Add(10*time.Minute), time.Now())
		if e != nil {
			continue
		}
		buttons = append(buttons, button{Type: 2, Style: 1, Label: label(v.Title), CustomID: "grab:" + signed})
	}
	if len(buttons) == 0 {
		return message("No releases were found for this request.", nil), nil
	}
	return message("Choose a release to queue. Selections expire after ten minutes.", buttons), nil
}

func (s *Service) ProcessNotice(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, err := s.Config(ctx)
	if err != nil || !c.Configured {
		return false, err
	}
	secret, err := s.secret(ctx)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO discord_bot_notices(request_id,state,updated_at) SELECT id,'queued',unixepoch() FROM media_requests WHERE state='pending' AND created_at>=(SELECT updated_at FROM discord_bot_config WHERE id=1) ON CONFLICT(request_id) DO NOTHING`); err != nil {
		return false, err
	}
	var id int64
	var title, user string
	err = tx.QueryRowContext(ctx, `SELECT n.request_id,r.title,a.username FROM discord_bot_notices n JOIN media_requests r ON r.id=n.request_id JOIN request_accounts a ON a.id=r.account_id WHERE n.state='queued' AND r.state='pending' ORDER BY n.request_id LIMIT 1`).Scan(&id, &title, &user)
	if errors.Is(err, sql.ErrNoRows) {
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE discord_bot_notices SET state='sending',updated_at=unixepoch() WHERE request_id=? AND state='queued'`, id); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	payload := message(fmt.Sprintf("New request %d: %s (from %s)", id, title, user), []button{{Type: 2, Style: 3, Label: "Approve and search", CustomID: fmt.Sprintf("approve:%d", id)}})
	state := "sent"
	if err = s.send(ctx, http.MethodPost, "/channels/"+c.AdminChannelID+"/messages", secret.Token, payload); err != nil {
		var limited *RateLimitError
		if errors.As(err, &limited) {
			// The notice was not delivered, so it stays retryable: requeue it
			// for the next worker tick rather than parking it in the terminal
			// uncertain state (NEW-C1). The requeues are capped in memory: a
			// persistently rate-limited notice is parked uncertain after the
			// cap instead of re-POSTing on every ~2 s tick indefinitely. The
			// cap is not durable — a restart resets it, which costs at most
			// one more bounded batch of retries per notice.
			if s.noticeAttempts == nil {
				s.noticeAttempts = map[int64]int{}
			}
			s.noticeAttempts[id]++
			if s.noticeAttempts[id] > noticeRequeueCap {
				delete(s.noticeAttempts, id)
				state = "uncertain"
			} else {
				state = "queued"
			}
		} else {
			delete(s.noticeAttempts, id)
			state = "uncertain"
		}
	}
	if state == "sent" {
		delete(s.noticeAttempts, id)
	}
	_, saveErr := s.db.ExecContext(context.WithoutCancel(ctx), `UPDATE discord_bot_notices SET state=?,updated_at=unixepoch() WHERE request_id=? AND state='sending'`, state, id)
	if saveErr != nil {
		return true, saveErr
	}
	return true, err
}

func (s *Service) Run(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE discord_bot_jobs SET state='uncertain',updated_at=unixepoch() WHERE state='running'`)
	_, _ = s.db.ExecContext(ctx, `UPDATE discord_bot_notices SET state='uncertain',updated_at=unixepoch() WHERE state='sending'`)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM discord_bot_jobs WHERE state IN ('sent','uncertain') AND created_at<unixepoch()-604800`)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM discord_bot_choices WHERE expires_at<unixepoch()-86400`)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	nextCleanup := time.Now().Add(time.Hour)
	for {
		if time.Now().After(nextCleanup) {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM discord_bot_jobs WHERE state IN ('sent','uncertain') AND created_at<unixepoch()-604800`)
			_, _ = s.db.ExecContext(ctx, `DELETE FROM discord_bot_choices WHERE expires_at<unixepoch()-86400`)
			nextCleanup = time.Now().Add(time.Hour)
		}
		for i := 0; i < 5 && ctx.Err() == nil; i++ {
			did, err := s.ProcessOne(ctx)
			if err != nil || !did {
				break
			}
		}
		for i := 0; i < 5 && ctx.Err() == nil; i++ {
			did, err := s.ProcessNotice(ctx)
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
