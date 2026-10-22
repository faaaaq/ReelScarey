package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/settings"
)

type Server struct {
	db       *sql.DB
	config   config.Config
	logger   *slog.Logger
	settings *settings.Store
	library  *library.Store
	version  string
	started  time.Time
}

func New(db *sql.DB, cfg config.Config, logger *slog.Logger, assets fs.FS, version string) http.Handler {
	s := &Server{db: db, config: cfg, logger: logger, settings: settings.New(db), library: library.New(db), version: version, started: time.Now()}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", method(http.MethodGet, s.health))
	mux.HandleFunc("/readyz", method(http.MethodGet, s.ready))
	mux.HandleFunc("/api/v1/system", method(http.MethodGet, s.system))
	mux.HandleFunc("/api/v1/dashboard", method(http.MethodGet, s.dashboard))
	mux.HandleFunc("/api/v1/library", method(http.MethodGet, s.listLibrary))
	mux.HandleFunc("/api/v1/settings", s.handleSettings)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		fail(w, 404, "not_found", "This API endpoint does not exist.")
	})
	mux.HandleFunc("/api", func(w http.ResponseWriter, r *http.Request) {
		fail(w, 404, "not_found", "This API endpoint does not exist.")
	})
	mux.HandleFunc("/", frontend(assets))
	return s.middleware(mux)
}

func method(verb string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != verb {
			w.Header().Set("Allow", verb)
			fail(w, 405, "method_not_allowed", "This method is not supported.")
			return
		}
		next(w, r)
	}
}

func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func fail(w http.ResponseWriter, status int, code, message string) {
	send(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (s *Server) internal(w http.ResponseWriter, operation string, err error) {
	s.logger.Error("request failed", "operation", operation, "error", err)
	fail(w, 500, "internal_error", "The request could not be completed. Check the application logs and try again.")
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	send(w, 200, map[string]string{"status": "ok"})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		fail(w, 503, "database_unavailable", "The database is not ready.")
		return
	}
	send(w, 200, map[string]string{"status": "ready"})
}

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	version, err := database.Version(r.Context(), s.db)
	if err != nil {
		s.internal(w, "system", err)
		return
	}
	send(w, 200, map[string]any{
		"name": "ReelScarey", "version": s.version, "milestone": "foundation",
		"database": "connected", "schema_version": version, "uptime_seconds": int(time.Since(s.started).Seconds()),
		"paths":        map[string]string{"config": s.config.ConfigDir, "media": s.config.MediaDir, "downloads": s.config.DownloadsDir},
		"capabilities": map[string]bool{"library_settings": true, "metadata_search": false, "downloads": false, "import": false, "subtitles": false, "automation": false},
	})
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) {
	counts, err := s.library.Counts(r.Context())
	if err != nil {
		s.internal(w, "library_counts", err)
		return
	}
	prefs, err := s.settings.Get(r.Context())
	if err != nil {
		s.internal(w, "dashboard_settings", err)
		return
	}
	send(w, 200, map[string]any{"library_name": prefs.LibraryName, "counts": counts, "settings_revision": prefs.Revision, "settings_updated_at": prefs.UpdatedAt})
}

func (s *Server) listLibrary(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("kind")
	if kind != "movie" && kind != "series" {
		fail(w, 400, "invalid_kind", "Choose kind=movie or kind=series.")
		return
	}
	items, err := s.library.List(r.Context(), kind)
	if err != nil {
		s.internal(w, "library_list", err)
		return
	}
	send(w, 200, map[string]any{"items": items, "limit": 100})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		prefs, err := s.settings.Get(r.Context())
		if err != nil {
			s.internal(w, "settings_get", err)
			return
		}
		send(w, 200, prefs)
	case http.MethodPut:
		if !sameOrigin(r) {
			fail(w, 403, "cross_origin", "Settings must be saved from the ReelScarey application origin.")
			return
		}
		mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "application/json" {
			fail(w, 415, "unsupported_media_type", "Use application/json.")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var in settings.Update
		if err := decoder.Decode(&in); err != nil {
			invalidJSON(w, err)
			return
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			invalidJSON(w, err)
			return
		}
		out, err := s.settings.Update(r.Context(), in)
		if errors.Is(err, settings.ErrConflict) {
			fail(w, 409, "revision_conflict", err.Error())
			return
		}
		var invalid *settings.ValidationError
		if errors.As(err, &invalid) {
			fail(w, 400, "validation_error", invalid.Error())
			return
		}
		if err != nil {
			s.internal(w, "settings_update", err)
			return
		}
		send(w, 200, out)
	default:
		w.Header().Set("Allow", "GET, PUT")
		fail(w, 405, "method_not_allowed", "Use GET or PUT for settings.")
	}
}

func invalidJSON(w http.ResponseWriter, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		fail(w, 413, "body_too_large", "The request body must be at most 16 KiB.")
		return
	}
	fail(w, 400, "invalid_json", "Send one valid JSON object with only the documented fields.")
}

func sameOrigin(r *http.Request) bool {
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	} // Non-browser clients still require JSON.
	u, err := url.Parse(origin)
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return err == nil && u.Scheme == scheme && strings.EqualFold(u.Host, r.Host) && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.User == nil
}

func frontend(assets fs.FS) http.HandlerFunc {
	files := http.FileServer(http.FS(assets))
	routes := map[string]bool{"/": true, "/movies": true, "/series": true, "/search": true, "/activity": true, "/subtitles": true, "/settings": true}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			fail(w, 405, "method_not_allowed", "Use GET or HEAD.")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if routes[r.URL.Path] {
			body, err := fs.ReadFile(assets, "index.html")
			if err != nil {
				fail(w, 503, "frontend_unavailable", "Build the frontend before starting the application.")
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			if r.Method == http.MethodGet {
				_, _ = w.Write(body)
			}
			return
		}
		info, err := fs.Stat(assets, name)
		if err != nil || info.IsDir() || strings.HasPrefix(filepath.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
		w.ResponseWriter.WriteHeader(code)
	}
}
func (w *responseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(body)
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &responseWriter{ResponseWriter: w}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "method", r.Method, "path", r.URL.Path)
				if wrapped.status == 0 {
					fail(wrapped, 500, "internal_error", "An unexpected error occurred.")
				}
			}
			status := wrapped.status
			if status == 0 {
				status = 200
			}
			s.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", time.Since(start).Milliseconds())
		}()
		next.ServeHTTP(wrapped, r)
	})
}
