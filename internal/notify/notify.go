// Package notify delivers administrator notifications through durable outbox
// rows. A send that may have reached the provider is marked uncertain and is
// never retried automatically, so a restart cannot silently duplicate it.
package notify

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

var (
	ErrConfig   = errors.New("Choose a notification name, provider and event, then try again.")
	ErrNotFound = errors.New("That notification channel does not exist.")
	ErrConflict = errors.New("The notification channel changed. Reload before saving.")
)

var SupportedKinds = []string{"email", "ntfy", "gotify", "pushover", "telegram", "discord", "webhook"}

var SupportedEvents = []string{
	"download.completed", "download.failed", "import.completed",
	"request.approved", "request.declined", "health.failed",
	"subtitle.failed", "backup.failed", "organization.failed", "youtube.completed",
}

type Channel struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Kind      string            `json:"kind"`
	Enabled   bool              `json:"enabled"`
	Config    map[string]string `json:"config"`
	Events    []string          `json:"events"`
	HasSecret bool              `json:"has_secret"`
	Revision  int               `json:"revision"`
	CreatedAt int64             `json:"created_at"`
	UpdatedAt int64             `json:"updated_at"`
}

type ChannelInput struct {
	Name     string            `json:"name"`
	Kind     string            `json:"kind"`
	Enabled  bool              `json:"enabled"`
	Config   map[string]string `json:"config"`
	Events   []string          `json:"events"`
	Secret   string            `json:"secret,omitempty"`
	Revision int               `json:"revision"`
}

type OutboxItem struct {
	ID          int64  `json:"id"`
	ChannelID   int64  `json:"channel_id"`
	Event       string `json:"event"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	State       string `json:"state"`
	Attempts    int    `json:"attempts"`
	NextAttempt int64  `json:"next_attempt"`
	LastError   string `json:"last_error"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
}

type Store struct {
	db      *sql.DB
	secrets *securestore.Store
	client  *http.Client
}

func New(db *sql.DB, secrets *securestore.Store) *Store {
	store := &Store{db: db, secrets: secrets, client: &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{Proxy: nil},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("notification redirects are disabled")
		},
	}}
	// Discord webhook URLs are bearer-capable secrets. Older rows kept them in
	// config_json; move them into the encrypted column before the API can list
	// them again.
	_ = store.migrateLegacy(context.Background())
	return store
}

// migrateLegacy seals a plaintext Discord URL left by an earlier schema and
// removes it from config_json. It is idempotent and safe to call on every start.
func (s *Store) migrateLegacy(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id,config_json FROM notification_channels WHERE kind='discord' AND secret_ciphertext IS NULL`)
	if err != nil {
		return err
	}
	type legacy struct {
		id  int64
		url string
	}
	items := []legacy{}
	for rows.Next() {
		var id int64
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var config map[string]string
		if json.Unmarshal([]byte(raw), &config) == nil {
			if url := strings.TrimSpace(config["url"]); url != "" {
				items = append(items, legacy{id: id, url: url})
			}
		}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := safeURL(item.url, false); err != nil || !isDiscordHost(item.url) {
			continue
		}
		sealed, err := s.secrets.Seal(fmt.Sprintf("notification:%d", item.id), []byte(item.url))
		if err != nil {
			return err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE notification_channels SET secret_ciphertext=?,config_json='{}',updated_at=? WHERE id=?`, sealed, time.Now().Unix(), item.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) List(ctx context.Context) ([]Channel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,kind,enabled,config_json,events_json,secret_ciphertext IS NOT NULL,revision,created_at,updated_at FROM notification_channels ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Channel{}
	for rows.Next() {
		var ch Channel
		var config, events string
		if err := rows.Scan(&ch.ID, &ch.Name, &ch.Kind, &ch.Enabled, &config, &events, &ch.HasSecret, &ch.Revision, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(config), &ch.Config); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(events), &ch.Events); err != nil {
			return nil, err
		}
		if ch.Config == nil {
			ch.Config = map[string]string{}
		}
		if ch.Kind == "discord" {
			delete(ch.Config, "url")
		}
		if ch.Events == nil {
			ch.Events = []string{}
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func (s *Store) Save(ctx context.Context, id int64, in ChannelInput) (Channel, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 80 || !utf8.ValidString(in.Name) || strings.IndexFunc(in.Name, unicode.IsControl) >= 0 {
		return Channel{}, ErrConfig
	}
	if !contains(SupportedKinds, in.Kind) {
		return Channel{}, ErrConfig
	}
	events, err := normalizeEvents(in.Events)
	if err != nil {
		return Channel{}, err
	}
	if in.Kind == "discord" {
		if raw, ok := in.Config["url"]; ok {
			raw = strings.TrimSpace(raw)
			if raw != "" {
				if _, err := safeURL(raw, false); err != nil || !isDiscordHost(raw) {
					return Channel{}, ErrConfig
				}
				in.Secret = raw
			}
			delete(in.Config, "url")
		}
		if id == 0 && in.Secret == "" {
			return Channel{}, ErrConfig
		}
	}
	config, err := validateConfig(in.Kind, in.Config)
	if err != nil {
		return Channel{}, err
	}
	if len(in.Secret) > 4096 || strings.ContainsAny(in.Secret, "\x00") {
		return Channel{}, ErrConfig
	}
	configJSON, _ := json.Marshal(config)
	eventsJSON, _ := json.Marshal(events)
	now := time.Now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Channel{}, err
	}
	defer tx.Rollback()
	if id == 0 {
		result, err := tx.ExecContext(ctx, `INSERT INTO notification_channels(name,kind,enabled,config_json,events_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			in.Name, in.Kind, in.Enabled, string(configJSON), string(eventsJSON), now, now)
		if err != nil {
			return Channel{}, err
		}
		id, err = result.LastInsertId()
		if err != nil {
			return Channel{}, err
		}
	} else {
		var revision int
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM notification_channels WHERE id=?`, id).Scan(&revision); errors.Is(err, sql.ErrNoRows) {
			return Channel{}, ErrNotFound
		} else if err != nil {
			return Channel{}, err
		}
		if in.Revision != revision {
			return Channel{}, ErrConflict
		}
		result, err := tx.ExecContext(ctx, `UPDATE notification_channels SET name=?,kind=?,enabled=?,config_json=?,events_json=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`,
			in.Name, in.Kind, in.Enabled, string(configJSON), string(eventsJSON), now, id, revision)
		if err != nil {
			return Channel{}, err
		}
		if changed, _ := result.RowsAffected(); changed != 1 {
			return Channel{}, ErrConflict
		}
	}
	if in.Secret != "" {
		sealed, err := s.secrets.Seal(fmt.Sprintf("notification:%d", id), []byte(in.Secret))
		if err != nil {
			return Channel{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE notification_channels SET secret_ciphertext=? WHERE id=?`, sealed, id); err != nil {
			return Channel{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Channel{}, err
	}
	return s.get(ctx, id)
}

func (s *Store) get(ctx context.Context, id int64) (Channel, error) {
	var ch Channel
	var config, events string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,kind,enabled,config_json,events_json,secret_ciphertext IS NOT NULL,revision,created_at,updated_at FROM notification_channels WHERE id=?`, id).
		Scan(&ch.ID, &ch.Name, &ch.Kind, &ch.Enabled, &config, &events, &ch.HasSecret, &ch.Revision, &ch.CreatedAt, &ch.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Channel{}, ErrNotFound
	}
	if err != nil {
		return Channel{}, err
	}
	if err := json.Unmarshal([]byte(config), &ch.Config); err != nil {
		return Channel{}, err
	}
	if err := json.Unmarshal([]byte(events), &ch.Events); err != nil {
		return Channel{}, err
	}
	if ch.Config == nil {
		ch.Config = map[string]string{}
	}
	if ch.Events == nil {
		ch.Events = []string{}
	}
	return ch, nil
}

func (s *Store) Delete(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM notification_channels WHERE id=?`, id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrNotFound
	}
	return nil
}

// Test sends a message synchronously. It bypasses the outbox so the owner gets
// an immediate answer while configuring a channel.
func (s *Store) Test(ctx context.Context, id int64) error {
	ch, err := s.get(ctx, id)
	if err != nil {
		return err
	}
	secret, err := s.secret(ctx, id)
	if err != nil {
		return err
	}
	return s.send(ctx, ch, secret, "test", "ReelScarey test notification", "This is a test notification from ReelScarey.")
}

func (s *Store) secret(ctx context.Context, id int64) (string, error) {
	var sealed []byte
	err := s.db.QueryRowContext(ctx, `SELECT secret_ciphertext FROM notification_channels WHERE id=?`, id).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil || len(sealed) == 0 {
		return "", err
	}
	plain, err := s.secrets.Open(fmt.Sprintf("notification:%d", id), sealed)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *Store) Outbox(ctx context.Context, limit int) ([]OutboxItem, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,channel_id,event,title,body,state,attempts,next_attempt,last_error,created_at,updated_at FROM notification_outbox ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OutboxItem{}
	for rows.Next() {
		var item OutboxItem
		if err := rows.Scan(&item.ID, &item.ChannelID, &item.Event, &item.Title, &item.Body, &item.State, &item.Attempts, &item.NextAttempt, &item.LastError, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Retry moves an uncertain or failed delivery back to pending on explicit owner
// request. It can duplicate a message that the provider already accepted.
func (s *Store) Retry(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE notification_outbox SET state='pending',next_attempt=0,attempts=0,last_error='',updated_at=? WHERE id=? AND state IN ('uncertain','failed')`, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return ErrNotFound
	}
	return nil
}

// PublishNewEvents maps recently recorded activity to notification events. The
// cursor and outbox inserts share one transaction so a crash cannot replay an
// event after the cursor moved.
func (s *Store) PublishNewEvents(ctx context.Context) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var cursor int64
	if err := tx.QueryRowContext(ctx, `SELECT last_event_id FROM notification_state WHERE id=1`).Scan(&cursor); err != nil {
		return 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,kind,title,state FROM activity_events WHERE id>? ORDER BY id LIMIT 200`, cursor)
	if err != nil {
		return 0, err
	}
	type eventRow struct {
		id              int64
		kind, title, st string
	}
	events := []eventRow{}
	for rows.Next() {
		var row eventRow
		if err := rows.Scan(&row.id, &row.kind, &row.title, &row.st); err != nil {
			rows.Close()
			return 0, err
		}
		events = append(events, row)
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(events) == 0 {
		return 0, tx.Commit()
	}
	channels, err := notificationChannels(ctx, tx)
	if err != nil {
		return 0, err
	}
	inserted := 0
	last := cursor
	for _, event := range events {
		last = event.id
		name, ok := eventName(event.kind, event.st)
		if !ok {
			continue
		}
		title := truncate(event.title, 200)
		body := truncate(event.kind+": "+event.st, 2000)
		for _, ch := range channels {
			if !contains(ch.events, name) {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO notification_outbox(channel_id,event,title,body,state,created_at,updated_at) VALUES(?,?,?,?,'pending',?,?)`,
				ch.id, name, title, body, time.Now().Unix(), time.Now().Unix()); err != nil {
				return 0, err
			}
			inserted++
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE notification_state SET last_event_id=? WHERE id=1`, last); err != nil {
		return 0, err
	}
	return inserted, tx.Commit()
}

type channelEvents struct {
	id     int64
	events []string
}

func notificationChannels(ctx context.Context, tx *sql.Tx) ([]channelEvents, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,events_json FROM notification_channels WHERE enabled=1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []channelEvents{}
	for rows.Next() {
		var ch channelEvents
		var raw string
		if err := rows.Scan(&ch.id, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &ch.events); err != nil {
			return nil, err
		}
		out = append(out, ch)
	}
	return out, rows.Err()
}

func eventName(kind, state string) (string, bool) {
	state = strings.ToLower(state)
	switch kind {
	case "download":
		if strings.Contains(state, "completed") {
			return "download.completed", true
		}
		if strings.Contains(state, "failed") || strings.Contains(state, "blocked") {
			return "download.failed", true
		}
	case "title":
		if strings.Contains(state, "file registered") {
			return "import.completed", true
		}
	case "request":
		if strings.Contains(state, "approved") {
			return "request.approved", true
		}
		if strings.Contains(state, "declined") {
			return "request.declined", true
		}
	case "subtitle":
		if strings.Contains(state, "failed") {
			return "subtitle.failed", true
		}
	case "backup":
		if strings.Contains(state, "failed") {
			return "backup.failed", true
		}
	case "organization":
		if strings.Contains(state, "failed") {
			return "organization.failed", true
		}
	case "youtube":
		if strings.Contains(state, "completed") {
			return "youtube.completed", true
		}
	case "automation":
		if strings.Contains(state, "failed") {
			return "health.failed", true
		}
	}
	return "", false
}

func normalizeEvents(in []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, event := range in {
		event = strings.TrimSpace(event)
		if !contains(SupportedEvents, event) {
			return nil, ErrConfig
		}
		if !seen[event] {
			seen[event] = true
			out = append(out, event)
		}
	}
	if len(out) == 0 {
		return nil, ErrConfig
	}
	return out, nil
}

func validateConfig(kind string, in map[string]string) (map[string]string, error) {
	if len(in) > 16 {
		return nil, ErrConfig
	}
	out := map[string]string{}
	for key, value := range in {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 64 || len(value) > 2048 || strings.ContainsAny(key+value, "\x00\r\n") {
			return nil, ErrConfig
		}
		out[key] = strings.TrimSpace(value)
	}
	switch kind {
	case "email":
		host := strings.TrimSpace(out["host"])
		if host == "" || strings.ContainsAny(host, " \t\r\n/") || out["from"] == "" || out["to"] == "" {
			return nil, ErrConfig
		}
		if port := strings.TrimSpace(out["port"]); port != "" {
			number, err := strconv.Atoi(port)
			if err != nil || number < 1 || number > 65535 {
				return nil, ErrConfig
			}
		}
		if _, err := mail.ParseAddress(out["from"]); err != nil {
			return nil, ErrConfig
		}
		if recipients, err := mail.ParseAddressList(out["to"]); err != nil || len(recipients) == 0 {
			return nil, ErrConfig
		}
	case "ntfy":
		if _, err := safeURL(out["url"], false); err != nil {
			return nil, ErrConfig
		}
	case "gotify", "webhook":
		if _, err := safeURL(out["url"], false); err != nil {
			return nil, ErrConfig
		}
	case "pushover":
		if out["user"] == "" {
			return nil, ErrConfig
		}
	case "telegram":
		if out["chat_id"] == "" {
			return nil, ErrConfig
		}
	case "discord":
		if out["url"] != "" {
			if _, err := safeURL(out["url"], false); err != nil || !isDiscordHost(out["url"]) {
				return nil, ErrConfig
			}
		}
	}
	return out, nil
}

func safeURL(raw string, allowEmpty bool) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" && allowEmpty {
		return &url.URL{}, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 1024 {
		return nil, ErrConfig
	}
	return u, nil
}

func isDiscordHost(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "discord.com" || host == "discordapp.com" || host == "canary.discord.com" || host == "ptb.discord.com"
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func truncate(value string, max int) string { return truncateUTF8(value, max) }

func truncateUTF8(value string, max int) string {
	if len(value) <= max {
		return value
	}
	for max > 0 && !utf8.ValidString(value[:max]) {
		max--
	}
	return value[:max]
}

func (s *Store) Run(ctx context.Context, logger *slog.Logger) {
	if err := s.migrateLegacy(ctx); err != nil && ctx.Err() == nil {
		logger.Error("notification secret migration failed", "error", err)
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE notification_outbox SET state='uncertain',updated_at=? WHERE state='sending'`, time.Now().Unix())
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		for ctx.Err() == nil {
			published, err := s.PublishNewEvents(ctx)
			if err != nil {
				logger.Error("notification event scan failed", "error", err)
				break
			}
			worked, err := s.ProcessOne(ctx)
			if err != nil {
				logger.Error("notification delivery failed", "error", err)
			}
			if !worked && published == 0 {
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

func (s *Store) ProcessOne(ctx context.Context) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var item OutboxItem
	err = tx.QueryRowContext(ctx, `UPDATE notification_outbox SET state='sending',attempts=attempts+1,updated_at=?
		WHERE id=(SELECT id FROM notification_outbox WHERE state='pending' AND next_attempt<=? ORDER BY next_attempt,id LIMIT 1)
		RETURNING id,channel_id,event,title,body,attempts`, time.Now().Unix(), time.Now().Unix()).
		Scan(&item.ID, &item.ChannelID, &item.Event, &item.Title, &item.Body, &item.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	ch, err := s.get(ctx, item.ChannelID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_ = s.finish(ctx, item.ID, "cancelled", 0, "")
			return true, nil
		}
		// A transient database/read failure must not permanently cancel the
		// delivery. Put it back with a short retry instead.
		next := time.Now().Add(time.Minute).Unix()
		_ = s.finish(ctx, item.ID, "pending", next, "The notification channel could not be read; retrying.")
		return true, err
	}
	if !ch.Enabled {
		_ = s.finish(ctx, item.ID, "cancelled", 0, "")
		return true, nil
	}
	secret, err := s.secret(ctx, item.ChannelID)
	if err != nil {
		_ = s.finish(ctx, item.ID, "failed", 0, "The channel secret could not be read.")
		return true, nil
	}
	if err := s.send(ctx, ch, secret, item.Event, item.Title, item.Body); err != nil {
		var problem *sendError
		if errors.As(err, &problem) {
			if problem.uncertain {
				return true, s.finish(ctx, item.ID, "uncertain", 0, problem.message)
			}
			if problem.retry && item.Attempts < 3 {
				next := time.Now().Add(time.Duration(item.Attempts) * 2 * time.Minute).Unix()
				return true, s.finish(ctx, item.ID, "pending", next, problem.message)
			}
			return true, s.finish(ctx, item.ID, "failed", 0, problem.message)
		}
		return true, s.finish(ctx, item.ID, "failed", 0, "The notification could not be sent.")
	}
	return true, s.finish(ctx, item.ID, "sent", 0, "")
}

func (s *Store) finish(ctx context.Context, id int64, state string, next int64, message string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE notification_outbox SET state=?,next_attempt=?,last_error=?,updated_at=? WHERE id=?`, state, next, truncate(message, 500), time.Now().Unix(), id)
	return err
}
