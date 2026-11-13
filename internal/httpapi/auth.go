package httpapi

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/settings"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
)

const cookieName = "reelscarey_session"

type sessionKey struct{}

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.warnInsecureBind()
		if !s.allowedHost(r.Host, r.RemoteAddr) {
			fail(w, 403, "invalid_host", "Open ReelScarey using localhost or its configured public URL.")
			return
		}
		// A reviewed restore restarts the application; refuse Subsonic writes
		// alongside the first-party API and compatibility gates (NEW-E3).
		// Subsonic writes already fail with code 50, so this only keeps clients
		// from hitting the database mid-restore.
		if s.backups != nil && subsonicRestoreBlocked(s.backups.Restoring(), r.URL.Path, r.Method) {
			w.Header().Set("Retry-After", "5")
			fail(w, 503, "restore_restarting", "A reviewed restore is restarting ReelScarey. Wait a moment, then retry.")
			return
		}
		if compatPath(r.URL.Path) {
			s.compatSecurity(next, w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api" {
			next.ServeHTTP(w, r)
			return
		}
		if s.backups != nil && s.backups.Restoring() && r.Method != "GET" && r.Method != "HEAD" {
			w.Header().Set("Retry-After", "5")
			fail(w, 503, "restore_restarting", "The reviewed restore is restarting ReelScarey. Wait a moment, then sign in again.")
			return
		}
		if r.URL.Path == "/api/v1/discord/interactions" {
			next.ServeHTTP(w, r)
			return
		}
		mutation := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
		if mutation && !s.sameOrigin(r) {
			fail(w, 403, "cross_origin", "Use the ReelScarey application origin for this action.")
			return
		}
		public := r.URL.Path == "/api/v1/auth/session" || r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/setup"
		if public {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(cookieName)
		if err != nil {
			fail(w, 401, "authentication_required", "Sign in to continue.")
			return
		}
		session, err := s.auth.Resolve(r.Context(), cookie.Value)
		if errors.Is(err, auth.ErrUnauthorized) {
			fail(w, 401, "authentication_required", "Your session has expired. Sign in again.")
			return
		}
		if err != nil {
			s.internal(w, "session_lookup", err)
			return
		}
		if session.Role != "admin" && session.Role != "requester" {
			fail(w, 403, "invalid_role", "This session is not authorized.")
			return
		}
		if mutation && subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
			fail(w, 403, "csrf_required", "Reload the application before trying again.")
			return
		}
		if session.Role == "requester" && r.URL.Path != "/api/v1/auth/logout" && !(path.Clean(r.URL.Path) == r.URL.Path && strings.HasPrefix(r.URL.Path, "/api/v1/requester/")) {
			fail(w, 403, "administrator_required", "This action is available only to the administrator.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, session)))
	})
}

// compatPath reports the Sonarr/Radarr/Prowlarr compatibility prefixes. These
// routes use scoped API keys instead of browser sessions.
func compatPath(path string) bool {
	for _, prefix := range []string{"/api/v3", "/sonarr", "/radarr", "/prowlarr"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// compatSecurity accepts a scoped API key, or an administrator session for
// manual inspection. It never accepts a request-account session.
type compatAttempt struct {
	count int
	until time.Time
}

// compatLimitedIP is a per-IP 60/minute sliding bucket shared by the
// compatibility API-key path and failed Subsonic sign-in attempts.
func (s *Server) compatLimitedIP(attempts *map[string]compatAttempt, ip string) bool {
	s.compatMu.Lock()
	defer s.compatMu.Unlock()
	now := time.Now()
	if *attempts == nil {
		*attempts = map[string]compatAttempt{}
	}
	attempt := (*attempts)[ip]
	if now.After(attempt.until) {
		attempt = compatAttempt{until: now.Add(time.Minute)}
	}
	attempt.count++
	(*attempts)[ip] = attempt
	if len(*attempts) > 2048 {
		for key, value := range *attempts {
			if now.After(value.until) {
				delete(*attempts, key)
			}
		}
	}
	return attempt.count <= 60
}

func (s *Server) compatLimited(ip string) bool {
	return s.compatLimitedIP(&s.compatAttempts, ip)
}

func (s *Server) compatSecurity(next http.Handler, w http.ResponseWriter, r *http.Request) {
	mutation := r.Method != "GET" && r.Method != "HEAD" && r.Method != "OPTIONS"
	if s.backups != nil && s.backups.Restoring() && mutation {
		w.Header().Set("Retry-After", "5")
		fail(w, 503, "restore_restarting", "A reviewed restore is restarting ReelScarey. Wait a moment, then retry.")
		return
	}
	token := strings.TrimSpace(r.Header.Get("X-Api-Key"))
	if token == "" && (r.Method == "GET" || r.Method == "HEAD") {
		// A query-string key lands in proxy and access logs, so it is accepted
		// only for reads; mutations must send the X-Api-Key header.
		token = strings.TrimSpace(r.URL.Query().Get("apikey"))
	}
	if token == "" {
		if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		}
	}
	if token != "" {
		key, err := s.auth.ResolveAPIKey(r.Context(), token)
		if errors.Is(err, auth.ErrUnauthorized) {
			ip, _, splitErr := net.SplitHostPort(r.RemoteAddr)
			if splitErr != nil {
				ip = r.RemoteAddr
			}
			if !s.compatLimited(ip) {
				w.Header().Set("Retry-After", "60")
				fail(w, 429, "api_key_rate_limited", "Too many invalid API-key attempts. Wait a minute and try again.")
				return
			}
			fail(w, 401, "invalid_api_key", "Provide a valid ReelScarey API key in the X-Api-Key header.")
			return
		}
		if err != nil {
			s.internal(w, "api_key_lookup", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(auth.WithAPIKey(r.Context(), key)))
		return
	}
	// A local administrator session may inspect compatibility output without
	// minting a key. Mutations still require CSRF, exactly like the main API.
	cookie, err := r.Cookie(cookieName)
	if err == nil {
		session, resolveErr := s.auth.Resolve(r.Context(), cookie.Value)
		if resolveErr == nil && session.Role == "admin" {
			if mutation && subtle.ConstantTimeCompare([]byte(session.CSRFToken), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
				fail(w, 403, "csrf_required", "Reload the application before trying again.")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, session)))
			return
		}
	}
	fail(w, 401, "authentication_required", "Provide a scoped ReelScarey API key in the X-Api-Key header.")
}

// isLoopbackHostname reports whether a claimed Host header names the loopback
// interface ("localhost" or a loopback IP literal).
func isLoopbackHostname(host string) bool {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	hostname = strings.Trim(hostname, "[]")
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	if ip := net.ParseIP(hostname); ip != nil && ip.IsLoopback() {
		return true
	}
	return false
}

func (s *Server) allowedHost(host, remoteAddr string) bool {
	// NEW-E2: a client-controlled Host header may only claim a loopback name
	// from a loopback connection. A remote client spoofing "Host: localhost"
	// is rejected; local clients keep working and proxies forwarding the real
	// configured origin are unaffected. An unparseable remote address is
	// treated as non-loopback, so the guard fails closed.
	remote := remoteAddr
	if h, _, err := net.SplitHostPort(remoteAddr); err == nil {
		remote = h
	}
	remoteIP := net.ParseIP(strings.Trim(remote, "[]"))
	if isLoopbackHostname(host) && (remoteIP == nil || !remoteIP.IsLoopback()) {
		return false
	}
	if isLoopbackHostname(host) {
		return true
	}
	if s.config.PublicURL != "" {
		u, err := url.Parse(s.config.PublicURL)
		return err == nil && strings.EqualFold(host, u.Host)
	}
	return false
}

// subsonicRestoreBlocked reports whether a Subsonic /rest/ request must be
// refused while a reviewed restore is restarting the application. It mirrors
// the compatibility surface: plain reads are still served, everything else is
// refused with 503 (NEW-E3). Expressed as a pure function so the gate is
// testable without driving a real restore; the caller supplies Restoring().
func subsonicRestoreBlocked(restoring bool, path, method string) bool {
	if !restoring || !strings.HasPrefix(path, "/rest/") {
		return false
	}
	return method != "GET" && method != "HEAD" && method != "OPTIONS"
}

// listenLoopback reports whether a bind address only accepts loopback
// connections. An empty host, a wildcard address or any resolvable
// non-loopback host listens beyond this machine.
func listenLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// insecureBindWarning reports the RS-0029 misconfiguration: a bind that accepts
// remote connections while REELSCAREY_PUBLIC_URL is not https://, so session
// cookies are issued without the Secure flag even behind a TLS-terminating
// proxy. Plain-HTTP LAN deployments remain fully supported; this only makes
// the trade-off visible instead of silently downgrading the cookie.
func insecureBindWarning(cfg config.Config) bool {
	return !listenLoopback(cfg.ListenAddr) && !strings.HasPrefix(cfg.PublicURL, "https://")
}

// warnInsecureBind logs the RS-0029 warning once per process, on the first
// served request.
var warnInsecureBindOnce sync.Once

func (s *Server) warnInsecureBind() {
	if !insecureBindWarning(s.config) {
		return
	}
	warnInsecureBindOnce.Do(func() {
		if s.logger != nil {
			s.logger.Warn("insecure bind: session cookies will not carry the Secure flag because the listen address is not loopback-only and REELSCAREY_PUBLIC_URL is not https://. Set REELSCAREY_PUBLIC_URL to the exact https:// origin behind a TLS reverse proxy, or keep the published port loopback-only.")
		}
	})
}

func (s *Server) cookie(w http.ResponseWriter, r *http.Request, token string, expires int64) {
	secure := r.TLS != nil || strings.HasPrefix(s.config.PublicURL, "https://")
	c := &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, Expires: time.Unix(expires, 0), MaxAge: int(time.Until(time.Unix(expires, 0)).Seconds())}
	if token == "" {
		c.MaxAge = -1
		c.Expires = time.Unix(1, 0)
	}
	http.SetCookie(w, c)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	return decodeJSONLimit(w, r, out, 16<<10)
}

// decodeOptionalJSON accepts a body-less DELETE while keeping the normal
// content-type and unknown-field checks when a body is present.
func decodeOptionalJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	if r.Body == nil || r.ContentLength == 0 {
		return true
	}
	return decodeJSON(w, r, out)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, 415, "unsupported_media_type", "Use application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, 413, "body_too_large", fmt.Sprintf("The request body must be at most %d KiB.", limit/1024))
			return false
		}
		invalidJSON(w, err)
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			fail(w, 413, "body_too_large", fmt.Sprintf("The request body must be at most %d KiB.", limit/1024))
			return false
		}
		invalidJSON(w, err)
		return false
	}
	return true
}

func (s *Server) authSession(w http.ResponseWriter, r *http.Request) {
	configured, err := s.auth.Configured(r.Context())
	if err != nil {
		s.internal(w, "auth_status", err)
		return
	}
	totpEnabled := false
	if s.totp != nil {
		if status, e := s.totp.Status(r.Context()); e == nil {
			totpEnabled = status.Enabled
		}
	}
	if c, err := r.Cookie(cookieName); err == nil {
		session, err := s.auth.Resolve(r.Context(), c.Value)
		if err == nil {
			send(w, 200, map[string]any{"setup_required": false, "authenticated": true, "session": session, "totp_enabled": totpEnabled})
			return
		}
		if !errors.Is(err, auth.ErrUnauthorized) {
			s.internal(w, "auth_status", err)
			return
		}
	}
	// The unauthenticated response must not reveal whether two-factor
	// authentication is enabled. A failed sign-in with the correct username
	// and password already asks for the code through the invalid_totp error.
	send(w, 200, map[string]any{"setup_required": !configured, "authenticated": false})
}
func (s *Server) authAttempt(w http.ResponseWriter, r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		ip = r.RemoteAddr
	}
	if !s.auth.Limit(ip) {
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "auth_rate_limited", auth.ErrBusy.Error())
		return false
	}
	return true
}
func (s *Server) authSetup(w http.ResponseWriter, r *http.Request) {
	if !s.authAttempt(w, r) {
		return
	}
	var body struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		SetupToken string `json:"setup_token"`
		ImportMode string `json:"import_mode"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	mode := strings.TrimSpace(body.ImportMode)
	if mode == "" {
		mode = "hardlink"
	}
	if err := settings.ValidateImportMode(mode); err != nil {
		fail(w, 400, "validation_error", err.Error())
		return
	}
	session, err := s.auth.SetupWith(r.Context(), body.Username, body.Password, body.SetupToken, func(ctx context.Context, tx *sql.Tx) error {
		return settings.SetImportModeTx(ctx, tx, mode)
	})
	if err != nil {
		s.authError(w, err)
		return
	}
	s.cookie(w, r, session.Token, session.ExpiresAt)
	send(w, 201, session)
}
func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if !s.authAttempt(w, r) {
		return
	}
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTPCode string `json:"totp_code"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}
	session, err := s.auth.LoginWithTOTP(r.Context(), body.Username, body.Password, body.TOTPCode)
	if err != nil {
		s.authError(w, err)
		return
	}
	s.cookie(w, r, session.Token, session.ExpiresAt)
	send(w, 200, session)
}
func (s *Server) authError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		fail(w, 401, "invalid_credentials", err.Error())
	case errors.Is(err, auth.ErrTOTPCode):
		fail(w, 401, "invalid_totp", err.Error())
	case errors.Is(err, auth.ErrSetupToken):
		fail(w, 403, "invalid_setup_token", err.Error())
	case errors.Is(err, auth.ErrSetupTokenFile):
		fail(w, 403, "setup_token_missing", err.Error())
	case errors.Is(err, auth.ErrSetupClosed):
		fail(w, 409, "setup_closed", err.Error())
	case errors.Is(err, auth.ErrPassword), errors.Is(err, auth.ErrUsername):
		fail(w, 400, "validation_error", err.Error())
	case errors.Is(err, auth.ErrBusy):
		w.Header().Set("Retry-After", "60")
		fail(w, 429, "auth_rate_limited", err.Error())
	default:
		s.internal(w, "authentication", err)
	}
}
func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	session := r.Context().Value(sessionKey{}).(auth.Session)
	if err := s.auth.Logout(r.Context(), session.Token); err != nil {
		s.internal(w, "logout", err)
		return
	}
	s.cookie(w, r, "", 0)
	send(w, 200, map[string]bool{"signed_out": true})
}
