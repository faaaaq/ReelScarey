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
	"time"
)

const cookieName = "reelscarey_session"

type sessionKey struct{}

func (s *Server) security(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowedHost(r.Host) {
			fail(w, 403, "invalid_host", "Open ReelScarey using localhost or its configured public URL.")
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

func (s *Server) allowedHost(host string) bool {
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
	if s.config.PublicURL != "" {
		u, err := url.Parse(s.config.PublicURL)
		return err == nil && strings.EqualFold(host, u.Host)
	}
	return false
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
	send(w, 200, map[string]any{"setup_required": !configured, "authenticated": false, "totp_enabled": totpEnabled})
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
