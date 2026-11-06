package announce

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// SourceSettings is the encrypted half of an announce source. Passwords are
// never returned by the API or written to logs.
type SourceSettings struct {
	Nick     string `json:"nick"`
	Account  string `json:"account,omitempty"`
	Password string `json:"password,omitempty"`
	Invite   string `json:"invite,omitempty"`
}

type Source struct {
	ID          string `json:"id"`
	Tracker     string `json:"tracker"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Revision    int    `json:"revision"`
	LastError   string `json:"last_error,omitempty"`
	LastEventAt int64  `json:"last_event_at"`
}

type SourceStatus struct {
	Source
	Server         string `json:"server"`
	Port           int    `json:"port"`
	TLS            bool   `json:"tls"`
	Channel        string `json:"channel"`
	Nick           string `json:"nick"`
	Account        string `json:"account"`
	HasPassword    bool   `json:"has_password"`
	RequireAccount bool   `json:"require_account"`
}

type SourceUpdate struct {
	Tracker  string `json:"tracker"`
	Name     string `json:"name"`
	Enabled  bool   `json:"enabled"`
	Revision int    `json:"revision"`
	Nick     string `json:"nick"`
	Account  string `json:"account"`
	Password string `json:"password"`
	Invite   string `json:"invite"`
}

type Event struct {
	ID          int64  `json:"id"`
	SourceID    string `json:"source_id"`
	Tracker     string `json:"tracker"`
	ReleaseName string `json:"release_name"`
	Category    string `json:"category"`
	SizeBytes   int64  `json:"size_bytes"`
	Freeleech   string `json:"freeleech,omitempty"`
	TorrentID   string `json:"torrent_id,omitempty"`
	InfoURL     string `json:"info_url,omitempty"`
	ReceivedAt  int64  `json:"received_at"`
}

type EventPage struct {
	Items []Event `json:"items"`
	Page  int     `json:"page"`
	Total int     `json:"total"`
	Limit int     `json:"limit"`
}

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string           { return e.Message }
func problem(code, message string) error { return &Error{code, message} }

var nickPattern = regexp.MustCompile(`^[A-Za-z0-9_\[\]\\^{}|\-]{1,32}$`)

type sourceRun struct {
	cancel   context.CancelFunc
	revision int
}

type Service struct {
	db      *sql.DB
	secrets *securestore.Store
	logger  *slog.Logger
	mu      sync.Mutex
	active  map[string]*sourceRun
}

func New(db *sql.DB, secrets *securestore.Store, logger *slog.Logger) *Service {
	return &Service{db: db, secrets: secrets, logger: logger, active: map[string]*sourceRun{}}
}

func definitions() map[string]Definition {
	defs, _ := Definitions()
	return defs
}

// DefinitionList is the safe subset shown to the UI.
type DefinitionList struct {
	Items []DefinitionSummary `json:"items"`
}
type DefinitionSummary struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Server         string `json:"server"`
	Port           int    `json:"port"`
	TLS            bool   `json:"tls"`
	Channel        string `json:"channel"`
	RequireAccount bool   `json:"require_account"`
}

func (s *Service) Definitions() DefinitionList {
	defs := definitions()
	out := DefinitionList{Items: []DefinitionSummary{}}
	for _, id := range []string{"iptorrents", "torrentleech", "filelist", "beyondhd", "animebytes", "myanonamouse"} {
		d, ok := defs[id]
		if !ok {
			continue
		}
		out.Items = append(out.Items, DefinitionSummary{ID: d.ID, Name: d.Name, Server: d.Server, Port: d.Port, TLS: d.TLS, Channel: d.Channel, RequireAccount: d.RequireAccount})
	}
	return out
}

func (s *Service) loadSettings(ctx context.Context, id string, sealed []byte) (SourceSettings, error) {
	var out SourceSettings
	plain, e := s.secrets.Open("announce:"+id, sealed)
	if e != nil {
		return out, e
	}
	if e = json.Unmarshal(plain, &out); e != nil {
		return out, e
	}
	return out, nil
}

func (s *Service) Sources(ctx context.Context) ([]SourceStatus, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,tracker,name,enabled,revision,settings,last_error,last_event_at FROM announce_sources ORDER BY name COLLATE NOCASE,id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SourceStatus{}
	defs := definitions()
	for rows.Next() {
		var st SourceStatus
		var sealed []byte
		if e = rows.Scan(&st.ID, &st.Tracker, &st.Name, &st.Enabled, &st.Revision, &sealed, &st.LastError, &st.LastEventAt); e != nil {
			return nil, e
		}
		if d, ok := defs[st.Tracker]; ok {
			st.Server, st.Port, st.TLS, st.Channel, st.RequireAccount = d.Server, d.Port, d.TLS, d.Channel, d.RequireAccount
		}
		settings, e := s.loadSettings(ctx, st.ID, sealed)
		if e != nil {
			st.LastError = "The saved IRC settings could not be read. Re-enter them."
		} else {
			st.HasPassword = settings.Password != ""
			st.Nick = settings.Nick
			st.Account = settings.Account
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

func (s *Service) SaveSource(ctx context.Context, id string, in SourceUpdate) (SourceStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defs := definitions()
	def, ok := defs[in.Tracker]
	if !ok {
		return SourceStatus{}, problem("invalid_settings", "Choose a supported announce tracker.")
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Nick = strings.TrimSpace(in.Nick)
	in.Account = strings.TrimSpace(in.Account)
	in.Invite = strings.TrimSpace(in.Invite)
	if len(in.Name) < 1 || len(in.Name) > 80 || strings.ContainsAny(in.Name, "\r\n\x00") {
		return SourceStatus{}, problem("invalid_settings", "Give the announce source a name with 1–80 characters.")
	}
	if !nickPattern.MatchString(in.Nick) {
		return SourceStatus{}, problem("invalid_settings", "Use a 1–32 character IRC nick with letters, digits, _-[]\\^{}|.")
	}
	if len(in.Invite) > 512 || strings.ContainsAny(in.Invite, "\r\n\x00") {
		return SourceStatus{}, problem("invalid_settings", "The invite command is too long or contains control characters.")
	}
	var sealed []byte
	var prior SourceSettings
	var revision int
	isNew := id == ""
	if isNew {
		id = newID()
		revision = 1
	} else {
		if in.Revision < 1 {
			return SourceStatus{}, problem("invalid_settings", "A valid revision is required.")
		}
		if e := s.db.QueryRowContext(ctx, `SELECT revision,settings FROM announce_sources WHERE id=?`, id).Scan(&revision, &sealed); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return SourceStatus{}, problem("invalid_settings", "This announce source no longer exists.")
			}
			return SourceStatus{}, e
		}
		if revision != in.Revision {
			return SourceStatus{}, problem("revision_conflict", "The announce source changed. Reload it before saving.")
		}
		prior, _ = s.loadSettings(ctx, id, sealed)
	}
	settings := prior
	settings.Nick = in.Nick
	if in.Account != "" {
		settings.Account = in.Account
	}
	if in.Password != "" {
		settings.Password = in.Password
	}
	if in.Invite != "" {
		settings.Invite = in.Invite
	}
	if in.Enabled && def.RequireAccount && (settings.Account == "" || settings.Password == "") {
		return SourceStatus{}, problem("invalid_settings", "This tracker requires a NickServ account and password before it can be enabled.")
	}
	plain, e := json.Marshal(settings)
	if e != nil {
		return SourceStatus{}, e
	}
	cipher, e := s.secrets.Seal("announce:"+id, plain)
	if e != nil {
		return SourceStatus{}, e
	}
	now := time.Now().Unix()
	if isNew {
		_, e = s.db.ExecContext(ctx, `INSERT INTO announce_sources(id,tracker,name,enabled,revision,settings,created_at,updated_at) VALUES(?,?,?,?,1,?,?,?)`, id, in.Tracker, in.Name, in.Enabled, cipher, now, now)
	} else {
		res, err := s.db.ExecContext(ctx, `UPDATE announce_sources SET name=?,enabled=?,revision=revision+1,settings=?,last_error='',updated_at=? WHERE id=? AND revision=?`, in.Name, in.Enabled, cipher, now, id, in.Revision)
		e = err
		if e == nil {
			if n, _ := res.RowsAffected(); n != 1 {
				e = problem("revision_conflict", "The announce source changed. Reload it before saving.")
			}
		}
	}
	if e != nil {
		return SourceStatus{}, e
	}
	return s.sourceStatus(ctx, id)
}

func (s *Service) sourceStatus(ctx context.Context, id string) (SourceStatus, error) {
	list, e := s.Sources(ctx)
	if e != nil {
		return SourceStatus{}, e
	}
	for _, item := range list {
		if item.ID == id {
			return item, nil
		}
	}
	return SourceStatus{}, problem("invalid_settings", "This announce source no longer exists.")
}

func (s *Service) DeleteSource(ctx context.Context, id string, revision int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	res, e := s.db.ExecContext(ctx, `DELETE FROM announce_sources WHERE id=? AND revision=?`, id, revision)
	if e != nil {
		return e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return problem("revision_conflict", "The announce source changed or was removed. Reload the list.")
	}
	s.cancel(id)
	return nil
}

func (s *Service) Events(ctx context.Context, page int) (EventPage, error) {
	const limit = 50
	if page < 1 || page > 100000 {
		return EventPage{}, problem("invalid_page", "Choose a valid page.")
	}
	var total int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM announce_events`).Scan(&total); e != nil {
		return EventPage{}, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT id,source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,info_url,received_at FROM announce_events ORDER BY received_at DESC,id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if e != nil {
		return EventPage{}, e
	}
	defer rows.Close()
	out := EventPage{Items: []Event{}, Page: page, Total: total, Limit: limit}
	for rows.Next() {
		var ev Event
		if e = rows.Scan(&ev.ID, &ev.SourceID, &ev.Tracker, &ev.ReleaseName, &ev.Category, &ev.SizeBytes, &ev.Freeleech, &ev.TorrentID, &ev.InfoURL, &ev.ReceivedAt); e != nil {
			return EventPage{}, e
		}
		out.Items = append(out.Items, ev)
	}
	return out, rows.Err()
}

// Record stores one parsed announce. The dedupe hash covers the raw line, but
// the line itself is never stored, so tracker links or credentials cannot leak
// from the event list.
func (s *Service) Record(ctx context.Context, sourceID, tracker string, p Parsed, raw string) error {
	sum := sha256.Sum256([]byte(raw))
	dedupe := hex.EncodeToString(sum[:])
	res, e := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO announce_events(source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,info_url,dedupe,received_at) VALUES(?,?,?,?,?,?,?,?,?,unixepoch())`, sourceID, tracker, p.ReleaseName, p.Category, p.SizeBytes, p.Freeleech, p.TorrentID, p.InfoURL, dedupe)
	if e != nil {
		return e
	}
	if n, _ := res.RowsAffected(); n == 1 {
		_, e = s.db.ExecContext(ctx, `UPDATE announce_sources SET last_event_at=unixepoch() WHERE id=?`, sourceID)
	}
	return e
}

func (s *Service) setError(ctx context.Context, sourceID, message string) {
	if len(message) > 500 {
		message = message[:500]
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE announce_sources SET last_error=? WHERE id=?`, message, sourceID)
}

func (s *Service) cancel(id string) {
	if run, ok := s.active[id]; ok {
		run.cancel()
		delete(s.active, id)
	}
}

// Run reconciles one goroutine per enabled source and reconnects with backoff.
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	defer func() {
		s.mu.Lock()
		for id := range s.active {
			s.cancel(id)
		}
		s.mu.Unlock()
	}()
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	for {
		s.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-prune.C:
			s.Prune(ctx)
		}
	}
}

func (s *Service) reconcile(ctx context.Context) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,tracker,name,enabled,revision,settings FROM announce_sources`)
	if e != nil {
		s.logger.Warn("announce sources unavailable", "error", e)
		return
	}
	type row struct {
		st       Source
		settings SourceSettings
	}
	items := map[string]row{}
	for rows.Next() {
		var r row
		var sealed []byte
		if e = rows.Scan(&r.st.ID, &r.st.Tracker, &r.st.Name, &r.st.Enabled, &r.st.Revision, &sealed); e != nil {
			rows.Close()
			s.logger.Warn("announce source scan failed", "error", e)
			return
		}
		if r.settings, e = s.loadSettings(ctx, r.st.ID, sealed); e != nil {
			continue
		}
		items[r.st.ID] = r
	}
	rows.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, run := range s.active {
		r, ok := items[id]
		if !ok || !r.st.Enabled || r.st.Revision != run.revision {
			s.cancel(id)
		}
	}
	for id, r := range items {
		if !r.st.Enabled {
			continue
		}
		if _, ok := s.active[id]; ok {
			continue
		}
		def, ok := definitions()[r.st.Tracker]
		if !ok {
			s.setError(ctx, id, "This tracker definition is unavailable.")
			continue
		}
		runCtx, cancel := context.WithCancel(ctx)
		s.active[id] = &sourceRun{cancel: cancel, revision: r.st.Revision}
		go s.runSource(runCtx, id, def, r.settings)
	}
}

func (s *Service) runSource(ctx context.Context, id string, def Definition, settings SourceSettings) {
	s.setError(ctx, id, "")
	backoff := 2 * time.Second
	for ctx.Err() == nil {
		client := &ircClient{def: def, settings: settings}
		e := client.Run(ctx, func(line string) {
			if parsed, ok := def.Parse(line); ok {
				if err := s.Record(ctx, id, def.ID, parsed, line); err != nil {
					s.logger.Warn("announce event could not be stored", "tracker", def.ID, "error", err)
				}
			}
		})
		if ctx.Err() != nil {
			return
		}
		if e != nil {
			s.setError(ctx, id, e.Error())
			s.logger.Warn("announce connection failed", "tracker", def.ID, "error", e)
		}
		if sleep(ctx, backoff) != nil {
			return
		}
		if backoff < 5*time.Minute {
			backoff *= 2
		}
	}
}

// Prune keeps a bounded history: 30 days and at most 5000 events.
func (s *Service) Prune(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM announce_events WHERE received_at < unixepoch()-30*86400`)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM announce_events WHERE id NOT IN (SELECT id FROM announce_events ORDER BY received_at DESC,id DESC LIMIT 5000)`)
}

func NewID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}

func newID() string { return NewID() }
