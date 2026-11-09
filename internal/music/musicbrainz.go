package music

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	// DefaultMusicBrainzBaseURL is the public MusicBrainz web service. The
	// application never bundles or hosts a MusicBrainz mirror.
	DefaultMusicBrainzBaseURL = "https://musicbrainz.org/ws/2"
	// musicbrainzIntegrationName is the encrypted integration row that stores
	// an optional user-supplied base URL. The URL is not a credential, but it
	// can disclose a private LAN address, so it uses the existing encrypted
	// integration store rather than a plain settings row.
	musicbrainzIntegrationName = "musicbrainz"
	// PublicMusicBrainzInterval follows the public server's one request per
	// second policy. Local mirrors may be configured with a much shorter
	// client-side spacing while still honoring their own 429/503 cooldown.
	PublicMusicBrainzInterval = time.Second
	LocalMusicBrainzInterval  = 50 * time.Millisecond
	musicbrainzMessageLimit   = 500
)

// MusicBrainzStatus is the administrator-facing provider status. It never
// contains a credential; the optional base URL is the only stored value.
type MusicBrainzStatus struct {
	BaseURL        string `json:"base_url"`
	DefaultBaseURL string `json:"default_base_url"`
	UsingDefault   bool   `json:"using_default"`
	CooldownUntil  int64  `json:"cooldown_until"`
	Message        string `json:"message"`
	CacheEntries   int    `json:"cache_entries"`
}

// NormalizeMusicBrainzBaseURL validates and normalizes an optional provider
// base URL.
//
// Contract: the value is a server prefix. A path ending in /ws/2 is kept as
// is; any other path (including /) gets /ws/2 appended exactly once. HTTPS is
// allowed for any host. Plain HTTP is allowed only for localhost, loopback,
// RFC1918 and IPv6 ULA addresses. Credentials, query strings, fragments and
// path traversal are refused. Redirects are refused by the HTTP client.
func NormalizeMusicBrainzBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\t") {
		return "", errors.New("Enter a MusicBrainz base URL.")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.Scheme == "" || parsed.Opaque != "" {
		return "", errors.New("Enter a complete MusicBrainz URL such as https://musicbrainz.org or a local mirror address.")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", errors.New("MusicBrainz base URLs must use HTTPS. Plain HTTP is allowed only for localhost or a private LAN address.")
	}
	if parsed.User != nil {
		return "", errors.New("MusicBrainz base URLs cannot include a username or password.")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" {
		return "", errors.New("MusicBrainz base URLs cannot include a query string or fragment.")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", errors.New("Enter a MusicBrainz URL with a hostname.")
	}
	if scheme == "http" && !privateMusicBrainzHost(host) {
		return "", errors.New("Plain HTTP is only allowed for localhost or a private LAN address. Use HTTPS for a public MusicBrainz base URL.")
	}
	rawPath := parsed.Path
	if strings.Contains(rawPath, `\`) {
		return "", errors.New("MusicBrainz base URLs cannot contain a backslash in the path.")
	}
	for _, segment := range strings.Split(strings.Trim(rawPath, "/"), "/") {
		decoded, err := url.PathUnescape(segment)
		if err != nil || decoded == "." || decoded == ".." || strings.ContainsAny(decoded, `/\`) || strings.ContainsFunc(decoded, func(r rune) bool { return r < 0x20 }) {
			return "", errors.New("MusicBrainz base URLs cannot contain path traversal or control characters.")
		}
	}
	cleanPath := path.Clean("/" + strings.Trim(rawPath, "/"))
	if cleanPath == "/" {
		cleanPath = ""
	}
	if cleanPath == "" {
		cleanPath = "/ws/2"
	} else if !strings.HasSuffix(cleanPath, "/ws/2") {
		cleanPath += "/ws/2"
	}
	parsed.Scheme = scheme
	parsed.Path = cleanPath
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

// MusicBrainzBaseIsLocal reports whether the base URL points at a local or
// private host, in which case the client uses the shorter mirror pacing.
func MusicBrainzBaseIsLocal(base string) bool {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil || parsed.Host == "" {
		return false
	}
	return privateMusicBrainzHost(strings.ToLower(parsed.Hostname()))
}

func privateMusicBrainzHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

func musicbrainzPacing(base string) time.Duration {
	if MusicBrainzBaseIsLocal(base) {
		return LocalMusicBrainzInterval
	}
	return PublicMusicBrainzInterval
}

// LoadMusicBrainzBase applies the saved optional base URL at startup. A saved
// value that no longer validates falls back to the public server instead of
// failing the application; the status API reports the reset.
func (s *Service) LoadMusicBrainzBase(ctx context.Context) error {
	if s.client == nil {
		return nil
	}
	saved, err := s.savedMusicBrainzBase(ctx)
	if err != nil {
		return err
	}
	if saved == "" {
		s.client.SetBase(DefaultMusicBrainzBaseURL)
		return nil
	}
	normalized, err := NormalizeMusicBrainzBaseURL(saved)
	if err != nil {
		s.client.SetBase(DefaultMusicBrainzBaseURL)
		return nil
	}
	s.client.SetBase(normalized)
	return nil
}

func (s *Service) savedMusicBrainzBase(ctx context.Context) (string, error) {
	if s.secrets == nil {
		return "", nil
	}
	raw, err := s.secrets.GetNamed(ctx, musicbrainzIntegrationName)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

// MusicBrainzStatus reads the effective provider, cooldown and active cache
// size. Reads never contact MusicBrainz and never return a secret.
func (s *Service) MusicBrainzStatus(ctx context.Context) (MusicBrainzStatus, error) {
	status := MusicBrainzStatus{
		BaseURL:        DefaultMusicBrainzBaseURL,
		DefaultBaseURL: DefaultMusicBrainzBaseURL,
		UsingDefault:   true,
	}
	saved, err := s.savedMusicBrainzBase(ctx)
	if err != nil {
		return status, err
	}
	effective := DefaultMusicBrainzBaseURL
	if saved != "" {
		normalized, normalizeErr := NormalizeMusicBrainzBaseURL(saved)
		if normalizeErr != nil {
			status.Message = "The saved MusicBrainz base URL is invalid. Save a new URL or reset it to the public server."
		} else {
			effective = normalized
			status.UsingDefault = false
		}
	}
	status.BaseURL = effective
	if s.brainz != nil {
		until, message, cooldownErr := s.brainz.Cooldown(ctx)
		if cooldownErr == nil && until.After(time.Now()) {
			status.CooldownUntil = until.Unix()
			status.Message = message
			if strings.TrimSpace(status.Message) == "" {
				status.Message = cooldownMessage(until)
			}
		}
		count, countErr := s.brainz.Count(ctx)
		if countErr == nil {
			status.CacheEntries = count
		} else if status.Message == "" {
			status.Message = "Cached MusicBrainz evidence is temporarily unreadable; requests continue without it."
		}
	}
	if status.Message == "" {
		if status.UsingDefault {
			status.Message = "Using public MusicBrainz at one request per second. Responses are cached locally as evidence, not permanent truth."
		} else {
			status.Message = "Using a custom MusicBrainz base URL. Local mirrors are paced at 50 ms. No MusicBrainz mirror is bundled with ReelScarey."
		}
	}
	return status, nil
}

// SaveMusicBrainzBaseURL validates, encrypts and applies an optional base URL.
// The next request uses the new value; requests already in flight keep the
// base they started with.
func (s *Service) SaveMusicBrainzBaseURL(ctx context.Context, raw string) (MusicBrainzStatus, error) {
	normalized, err := NormalizeMusicBrainzBaseURL(raw)
	if err != nil {
		return MusicBrainzStatus{}, &Error{Code: "music_invalid_musicbrainz_url", Message: err.Error()}
	}
	if s.secrets == nil {
		return MusicBrainzStatus{}, &Error{Code: "music_musicbrainz_unavailable", Message: "MusicBrainz settings storage is unavailable. Configure the application secret store and try again."}
	}
	if err := s.secrets.SetNamed(ctx, musicbrainzIntegrationName, []byte(normalized)); err != nil {
		return MusicBrainzStatus{}, err
	}
	if s.client != nil {
		s.client.SetBase(normalized)
	}
	return s.MusicBrainzStatus(ctx)
}

// DeleteMusicBrainzBaseURL removes the optional base URL and returns to the
// public MusicBrainz service.
func (s *Service) DeleteMusicBrainzBaseURL(ctx context.Context) (MusicBrainzStatus, error) {
	if s.secrets == nil {
		return MusicBrainzStatus{}, &Error{Code: "music_musicbrainz_unavailable", Message: "MusicBrainz settings storage is unavailable. Configure the application secret store and try again."}
	}
	if err := s.secrets.DeleteNamed(ctx, musicbrainzIntegrationName); err != nil {
		return MusicBrainzStatus{}, err
	}
	if s.client != nil {
		s.client.SetBase(DefaultMusicBrainzBaseURL)
	}
	return s.MusicBrainzStatus(ctx)
}

func cooldownMessage(until time.Time) string {
	return "MusicBrainz is limiting requests. Matching is paused until " + until.UTC().Format(time.RFC3339) + ". Wait for that window, then run Match all again."
}

// musicbrainzStore persists provider evidence and the cooldown in SQLite. Every
// method is fail-safe at the call site: a database problem only disables the
// cache or the persisted cooldown, never matching itself.
type musicbrainzStore struct {
	db *sql.DB
}

const musicbrainzCleanupLimit = 500

func (m *musicbrainzStore) Get(ctx context.Context, key string) ([]byte, bool, bool, error) {
	var payload string
	var negative int
	err := m.db.QueryRowContext(ctx, `SELECT payload,negative FROM musicbrainz_cache WHERE key=? AND expires_at>unixepoch()`, key).Scan(&payload, &negative)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, false, nil
	}
	if err != nil {
		return nil, false, false, err
	}
	return []byte(payload), negative != 0, true, nil
}

func (m *musicbrainzStore) Put(ctx context.Context, key string, payload []byte, ttl time.Duration, negative bool) error {
	if ttl <= 0 {
		return nil
	}
	expires := time.Now().Add(ttl).Unix()
	flag := 0
	if negative {
		flag = 1
	}
	_, err := m.db.ExecContext(ctx, `INSERT INTO musicbrainz_cache(key,payload,expires_at,negative) VALUES(?,?,?,?)
		ON CONFLICT(key) DO UPDATE SET payload=excluded.payload,expires_at=excluded.expires_at,negative=excluded.negative`,
		key, string(payload), expires, flag)
	return err
}

// Cleanup removes at most musicbrainzCleanupLimit expired rows. The client
// calls it occasionally, so the table cannot grow without bound.
func (m *musicbrainzStore) Cleanup(ctx context.Context) error {
	_, err := m.db.ExecContext(ctx, `DELETE FROM musicbrainz_cache WHERE key IN (
		SELECT key FROM musicbrainz_cache WHERE expires_at<=unixepoch() LIMIT ?)`, musicbrainzCleanupLimit)
	return err
}

func (m *musicbrainzStore) Count(ctx context.Context) (int, error) {
	var count int
	err := m.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM musicbrainz_cache WHERE expires_at>unixepoch()`).Scan(&count)
	return count, err
}

func (m *musicbrainzStore) Cooldown(ctx context.Context) (time.Time, string, error) {
	var retryUntil int64
	var message string
	err := m.db.QueryRowContext(ctx, `SELECT retry_until,message FROM musicbrainz_provider_state WHERE id=1`).Scan(&retryUntil, &message)
	if errors.Is(err, sql.ErrNoRows) {
		return time.Time{}, "", nil
	}
	if err != nil {
		return time.Time{}, "", err
	}
	return time.Unix(retryUntil, 0), message, nil
}

func (m *musicbrainzStore) SetCooldown(ctx context.Context, until time.Time, message string) error {
	message = strings.TrimSpace(message)
	if runes := []rune(message); len(runes) > musicbrainzMessageLimit {
		message = string(runes[:musicbrainzMessageLimit])
	}
	_, err := m.db.ExecContext(ctx, `INSERT INTO musicbrainz_provider_state(id,retry_until,message,updated_at) VALUES(1,?,?,unixepoch())
		ON CONFLICT(id) DO UPDATE SET retry_until=excluded.retry_until,message=excluded.message,updated_at=excluded.updated_at`,
		until.Unix(), message)
	return err
}
