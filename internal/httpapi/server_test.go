package httpapi

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
)

func rawFixture(t *testing.T, provider metadata.MovieProvider) (http.Handler, *sql.DB, *auth.Service, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	assets := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>ReelScarey</title>")}, "assets/app.js": {Data: []byte("console.log('asset')")}}
	authService, err := auth.New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	store, err := securestore.New(context.Background(), db, dir)
	if err != nil {
		t.Fatal(err)
	}
	code, err := securestore.SecretFile(filepath.Join(dir, "setup-token"), 32, false)
	if err != nil {
		t.Fatal(err)
	}
	if provider == nil {
		provider = metadata.NewTMDb()
	}
	return New(db, config.Config{ConfigDir: dir, MediaDir: "/media", DownloadsDir: "/downloads", PublicURL: "http://example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), assets, "test", Dependencies{Auth: authService, Metadata: metadata.New(store, provider), Secrets: store}), db, authService, base64.RawURLEncoding.EncodeToString(code)
}

func fixture(t *testing.T) (http.Handler, *sql.DB) {
	handler, db, service, code := rawFixture(t, nil)
	session, err := service.Setup(context.Background(), "testadmin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		handler.ServeHTTP(w, r)
	}), db
}

const validSettings = `{"library_name":"Cinema","movie_folder":"Movies","series_folder":"TV","revision":1}`

func TestRequesterCannotAccessAdministratorAPIs(t *testing.T) {
	h, _, service, code := rawFixture(t, nil)
	ctx := context.Background()
	if _, err := service.Setup(ctx, "admin", "admin-test-password", code); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRequestAccount(ctx, "viewer", "viewer-test-password"); err != nil {
		t.Fatal(err)
	}
	session, err := service.Login(ctx, "viewer", "viewer-test-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct{ method, path, body string }{
		{"GET", "/api/v1/settings", ""}, {"GET", "/api/v1/library", ""}, {"GET", "/api/v1/indexers", ""},
		{"POST", "/api/v1/automation/run", "{}"}, {"PUT", "/api/v1/settings", validSettings},
		{"GET", "/api/v1/requester/../settings", ""},
	} {
		req := httptest.NewRequest(route.method, route.path, strings.NewReader(route.body))
		req.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
		req.Header.Set("X-CSRF-Token", session.CSRFToken)
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 403 {
			t.Fatalf("requester accessed %s: %d %s", route.path, rec.Code, rec.Body)
		}
	}
}

func TestAPIInputBoundaries(t *testing.T) {
	for _, test := range []struct {
		name, method, path, body, contentType, origin string
		status                                        int
	}{
		{"health", "GET", "/healthz", "", "", "", 200},
		{"ready", "GET", "/readyz", "", "", "", 200},
		{"system", "GET", "/api/v1/system", "", "", "", 200},
		{"settings", "GET", "/api/v1/settings", "", "", "", 200},
		{"valid", "PUT", "/api/v1/settings", validSettings, "application/json", "http://example.com", 200},
		{"wrong origin", "PUT", "/api/v1/settings", validSettings, "application/json", "https://evil.test", 403},
		{"null origin", "PUT", "/api/v1/settings", validSettings, "application/json", "null", 403},
		{"text", "PUT", "/api/v1/settings", validSettings, "text/plain", "", 415},
		{"unknown field", "PUT", "/api/v1/settings", `{"secret":"value"}`, "application/json", "", 400},
		{"malformed", "PUT", "/api/v1/settings", "{", "application/json", "", 400},
		{"multiple objects", "PUT", "/api/v1/settings", validSettings + "{}", "application/json", "", 400},
		{"null", "PUT", "/api/v1/settings", "null", "application/json", "", 400},
		{"oversized", "PUT", "/api/v1/settings", `{"library_name":"` + strings.Repeat("a", 17000) + `"}`, "application/json", "", 413},
		{"wrong method", "POST", "/api/v1/settings", "", "", "", 405},
		{"library kind", "GET", "/api/v1/library?kind=music", "", "", "", 400},
		{"movie library", "GET", "/api/v1/library?kind=movie", "", "", "", 200},
		{"unknown api", "GET", "/api/v1/nope", "", "", "", 404},
		{"bare api", "GET", "/api", "", "", "", 404},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler, _ := fixture(t)
			req := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			req.Header.Set("Content-Type", test.contentType)
			req.Header.Set("Origin", test.origin)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != test.status {
				t.Fatalf("got %d, want %d: %s", rec.Code, test.status, rec.Body.String())
			}
			if !json.Valid(rec.Body.Bytes()) {
				t.Fatalf("non-JSON API response: %s", rec.Body)
			}
			if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("security headers missing")
			}
		})
	}
}

func TestSettingsConflictAndLiveCounts(t *testing.T) {
	handler, db := fixture(t)
	for i, expected := range []int{200, 409} {
		req := httptest.NewRequest("PUT", "/api/v1/settings", strings.NewReader(validSettings))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != expected {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}
	if _, err := db.Exec("INSERT INTO media_items (kind, tmdb_id, title, year) VALUES ('movie', 1, 'Test fixture', 2020)"); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/dashboard", nil))
	var body struct {
		LibraryName string `json:"library_name"`
		Counts      struct {
			Movies int `json:"movies"`
		} `json:"counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.LibraryName != "Cinema" || body.Counts.Movies != 1 {
		t.Fatalf("dashboard not reading SQLite: %+v", body)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/library?kind=movie", nil))
	if !strings.Contains(rec.Body.String(), "Test fixture") {
		t.Fatalf("library does not read real rows: %s", rec.Body)
	}
}

func TestFrontendRoutingAndReadinessFailure(t *testing.T) {
	handler, db := fixture(t)
	for _, test := range []struct {
		method, path string
		status       int
		contains     string
	}{
		{"GET", "/", 200, "ReelScarey"}, {"GET", "/settings", 200, "ReelScarey"},
		{"GET", "/automation", 200, "ReelScarey"},
		{"HEAD", "/movies", 200, ""}, {"GET", "/assets/app.js", 200, "asset"},
		{"GET", "/assets/missing.js", 404, "404"}, {"GET", "/unknown", 404, "404"},
		{"GET", "/assets/", 404, "404"}, {"POST", "/settings", 405, "method_not_allowed"},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(test.method, test.path, nil))
		if rec.Code != test.status || !strings.Contains(rec.Body.String(), test.contains) {
			t.Fatalf("%s: %d %s", test.path, rec.Code, rec.Body)
		}
		if test.method == "HEAD" && rec.Body.Len() != 0 {
			t.Fatal("HEAD returned a body")
		}
	}
	db.Close()
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != 503 {
		t.Fatalf("closed database is ready: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Fatal("liveness should remain independent of database readiness")
	}
}
