package subtitle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type Service struct {
	db       *sql.DB
	secrets  *securestore.Store
	cfg      config.Config
	provider Provider
	subdl    *SubDL
	configMu sync.Mutex
	Prober   transfer.Prober
	now      func() time.Time
}

func New(db *sql.DB, secrets *securestore.Store, cfg config.Config, p Provider) *Service {
	return &Service{db: db, secrets: secrets, cfg: cfg, provider: p, subdl: NewSubDL(), Prober: transfer.FFprobe{}, now: time.Now}
}

type Preferences struct {
	DownloadFormat  string  `json:"download_format"`
	Enabled         bool    `json:"enabled"`
	Tracks          []Track `json:"tracks"`
	HearingImpaired string  `json:"hearing_impaired"`
	MatchPolicy     string  `json:"match_policy"`
	Interval        int     `json:"interval_minutes"`
	OnlyMonitored   bool    `json:"only_monitored"`
	Revision        int     `json:"revision"`
}

func (s *Service) Preferences(ctx context.Context) (Preferences, error) {
	var p Preferences
	var tracks string
	e := s.db.QueryRowContext(ctx, `SELECT enabled,tracks,hearing_impaired,match_policy,interval_minutes,only_monitored,revision,download_format FROM subtitle_preferences WHERE id=1`).Scan(&p.Enabled, &tracks, &p.HearingImpaired, &p.MatchPolicy, &p.Interval, &p.OnlyMonitored, &p.Revision, &p.DownloadFormat)
	if e == nil {
		e = json.Unmarshal([]byte(tracks), &p.Tracks)
	}
	return p, e
}
func validatePreferences(p Preferences) error {
	if p.DownloadFormat != "" && p.DownloadFormat != "srt" && p.DownloadFormat != "original" {
		return problem("invalid_preferences", "Choose SRT or keep the supported original subtitle format.")
	}
	if len(p.Tracks) == 0 || len(p.Tracks) > 9 || p.Interval < 60 || p.Interval > 10080 || !slices.Contains([]string{"any", "exclude", "prefer"}, p.HearingImpaired) || !slices.Contains([]string{"hash", "hash_or_release"}, p.MatchPolicy) {
		return problem("invalid_preferences", "Choose 1–9 language/track combinations, an interval of 60–10080 minutes, and supported matching/hearing preferences.")
	}
	seen := map[Track]bool{}
	langs := map[string]bool{}
	for _, t := range p.Tracks {
		if !validTrack(t) || seen[t] {
			return problem("invalid_preferences", "Choose supported languages and unique full, signs/songs or forced tracks.")
		}
		seen[t] = true
		langs[t.Language] = true
	}
	if len(langs) > 3 {
		return problem("invalid_preferences", "Choose at most three subtitle languages.")
	}
	return nil
}
func (s *Service) SavePreferences(ctx context.Context, p Preferences) (Preferences, error) {
	if p.DownloadFormat == "" {
		p.DownloadFormat = "srt"
	}
	if e := validatePreferences(p); e != nil {
		return p, e
	}
	b, _ := json.Marshal(p.Tracks)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return p, e
	}
	defer tx.Rollback()
	r, e := tx.ExecContext(ctx, `UPDATE subtitle_preferences SET enabled=?,tracks=?,hearing_impaired=?,match_policy=?,interval_minutes=?,only_monitored=?,download_format=?,revision=revision+1 WHERE id=1 AND revision=?`, p.Enabled, string(b), p.HearingImpaired, p.MatchPolicy, p.Interval, p.OnlyMonitored, p.DownloadFormat, p.Revision)
	if e != nil {
		return p, e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return p, problem("revision_conflict", "Subtitle preferences changed. Reload before saving.")
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_jobs SET requested=0,next_run=0,attempts=0,state=CASE WHEN ? THEN 'queued' ELSE 'paused' END,message='Subtitle preferences changed.',updated_at=?`, p.Enabled, s.now().Unix()); e != nil {
		return p, e
	}
	if e = tx.Commit(); e != nil {
		return p, e
	}
	return s.Preferences(ctx)
}
func (s *Service) credentials(ctx context.Context) (Credentials, error) {
	var c Credentials
	b, e := s.secrets.GetNamed(ctx, "opensubtitles")
	if e != nil {
		return c, e
	}
	if len(b) == 0 {
		return c, problem("provider_not_configured", "Connect a subtitle provider to search and download subtitles. Torznab is not required.")
	}
	e = json.Unmarshal(b, &c)
	if e == nil && providerName(c.Provider) != "opensubtitles" && c.Provider != "subdl" {
		return c, problem("provider_not_configured", "The saved subtitle provider is unsupported.")
	}
	return c, e
}

type ProviderStatus struct {
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"`
	Username   string `json:"username"`
	UserAgent  string `json:"user_agent"`
	State      string `json:"state"`
	Message    string `json:"message"`
	Cooldown   int64  `json:"cooldown_until"`
	Remaining  *int   `json:"remaining"`
}

func (s *Service) Status(ctx context.Context) (ProviderStatus, error) {
	c, e := s.credentials(ctx)
	var p *Error
	if errors.As(e, &p) && p.Code == "provider_not_configured" {
		return ProviderStatus{State: "not_connected", Message: p.Message, UserAgent: "ReelScarey v0.7.0"}, nil
	}
	if e != nil {
		return ProviderStatus{}, e
	}
	out := ProviderStatus{Provider: providerName(c.Provider), Configured: true, Username: c.Username, UserAgent: c.UserAgent}
	e = s.db.QueryRowContext(ctx, `SELECT state,message,cooldown_until,remaining FROM subtitle_provider_state WHERE id=1`).Scan(&out.State, &out.Message, &out.Cooldown, &out.Remaining)
	if e == nil && out.Cooldown <= s.now().Unix() && (out.State == "provider_quota" || out.State == "provider_rate_limit") {
		out.State, out.Message, out.Cooldown = "ready", "Provider cooldown ended; missing tracks can be checked again.", 0
	}
	return out, e
}
func (s *Service) Configure(ctx context.Context, in CredentialUpdate) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	old, e := s.credentials(ctx)
	var p *Error
	if e != nil && !(errors.As(e, &p) && p.Code == "provider_not_configured") {
		return e
	}
	in.Provider = providerName(in.Provider)
	if in.Provider != "opensubtitles" && in.Provider != "subdl" {
		return problem("invalid_credentials", "Choose OpenSubtitles or SubDL.")
	}
	if providerName(old.Provider) != in.Provider {
		old = Credentials{}
		if in.APIKey == nil {
			return problem("invalid_credentials", "Enter a new API key when changing subtitle providers.")
		}
	}
	c := Credentials{Provider: in.Provider, Username: strings.TrimSpace(in.Username), UserAgent: strings.TrimSpace(in.UserAgent), APIKey: old.APIKey, Password: old.Password, Generation: transfer.ID()}
	if in.APIKey != nil {
		c.APIKey = *in.APIKey
	}
	if in.Password != nil {
		c.Password = *in.Password
	}
	if c.Provider == "opensubtitles" && old.Username != "" && c.Username != old.Username && in.Password == nil {
		return problem("invalid_credentials", "Re-enter the password when changing the OpenSubtitles account.")
	}
	if c.Provider == "subdl" {
		c.Username = ""
		c.Password = ""
		c.UserAgent = ""
	}
	if !textOK(c.APIKey, 2048) || (c.Provider == "opensubtitles" && (!textOK(c.Username, 200) || !textOK(c.Password, 2048) || !textOK(c.UserAgent, 120))) {
		if c.Provider == "subdl" {
			return problem("invalid_credentials", "Enter a valid SubDL API key.")
		}
		return problem("invalid_credentials", "Enter your OpenSubtitles API key, account username/password and registered application name/version.")
	}
	if e = s.selectedProvider(c).Test(ctx, c); e != nil {
		return e
	}
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	sealed, e := s.secrets.Seal("config:opensubtitles", b)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `INSERT INTO integration_configs(name,ciphertext) VALUES('opensubtitles',?) ON CONFLICT(name) DO UPDATE SET ciphertext=excluded.ciphertext,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')`, sealed); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_provider_state SET state='ready',message='Connection verified.',cooldown_until=0,remaining=NULL,generation=? WHERE id=1`, c.Generation); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_jobs SET next_run=0,attempts=0,state='queued',updated_at=? WHERE state IN ('waiting_provider','retrying','no_match','needs_review')`, s.now().Unix()); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) Test(ctx context.Context) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	c, e := s.credentials(ctx)
	if e != nil {
		return e
	}
	if e = s.selectedProvider(c).Test(ctx, c); e != nil {
		return s.persistProviderError(ctx, c.Generation, e)
	}
	_, e = s.db.ExecContext(ctx, `UPDATE subtitle_provider_state SET state='ready',message='Connection verified.',cooldown_until=0,remaining=NULL WHERE id=1 AND generation=?`, c.Generation)
	return e
}
func (s *Service) Disconnect(ctx context.Context) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `DELETE FROM integration_configs WHERE name='opensubtitles'`); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_provider_state SET state='not_connected',message='Provider disconnected.',generation='',remaining=NULL,cooldown_until=0 WHERE id=1`); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) providerError(ctx context.Context, generation string, err error) error {
	var p *Error
	if !errors.As(err, &p) {
		return nil
	}
	if !slices.Contains([]string{"provider_auth", "provider_access", "provider_rate_limit", "provider_quota"}, p.Code) {
		return nil
	}
	until := p.RetryAt
	if until == 0 {
		until = s.now().Unix() + 86400
	}
	_, e := s.db.ExecContext(ctx, `UPDATE subtitle_provider_state SET state=?,message=?,cooldown_until=? WHERE id=1 AND generation=?`, p.Code, p.Message, until, generation)
	return e
}

// persistProviderError records a provider cooldown after a provider call. The
// original provider error stays authoritative; a failed state write is folded
// into it so an unrecorded cooldown cannot go unnoticed. Without this, a
// failed write would let the worker keep contacting a provider that just
// asked us to back off (RS-0014). The combined error uses a code the job
// state machine does not classify as a recorded cooldown, so the run fails
// visibly into review instead of presenting an unsaved cooldown as usable.
func (s *Service) persistProviderError(ctx context.Context, generation string, providerErr error) error {
	if err := s.providerError(ctx, generation, providerErr); err != nil {
		return &Error{Code: "provider_state_write_failed", Message: fmt.Sprintf("%s (the provider cooldown could not be saved: %v)", providerErr, err)}
	}
	return providerErr
}
func (s *Service) providerReady(ctx context.Context, generation string) error {
	var state, message, actual string
	var until int64
	if e := s.db.QueryRowContext(ctx, `SELECT state,message,cooldown_until,generation FROM subtitle_provider_state WHERE id=1`).Scan(&state, &message, &until, &actual); e != nil {
		return e
	}
	if actual != generation {
		return problem("provider_changed", "Subtitle provider settings changed. Request a fresh search.")
	}
	if state == "provider_auth" || state == "provider_access" {
		return &Error{Code: state, Message: message, RetryAt: s.now().Unix() + 86400}
	}
	if until > s.now().Unix() {
		return &Error{Code: state, Message: message, Temporary: true, RetryAt: until}
	}
	return nil
}
func (s *Service) quota(ctx context.Context, c Credentials, d Download) error {
	if d.Remaining == nil {
		return nil
	}
	state, message := "ready", ""
	until := int64(0)
	if *d.Remaining <= 0 {
		state = "provider_quota"
		message = "OpenSubtitles download allowance is exhausted. Downloads will wait for the reset."
		until = d.ResetAt
		if until <= s.now().Unix() {
			until = s.now().Unix() + 86400
		}
	}
	_, e := s.db.ExecContext(ctx, `UPDATE subtitle_provider_state SET remaining=?,state=?,message=?,cooldown_until=? WHERE id=1 AND generation=?`, d.Remaining, state, message, until, c.Generation)
	return e
}
