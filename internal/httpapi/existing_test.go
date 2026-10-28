package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func TestExistingLibraryAPIAuthenticationReviewAndPathBoundaries(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(root, "config"), MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads"), PublicURL: "http://example.com"}
	dir := filepath.Join(cfg.MediaDir, "Movies")
	if e := os.MkdirAll(dir, 0755); e != nil {
		t.Fatal(e)
	}
	source := filepath.Join(dir, "owned.mp4")
	body := []byte("owned unit fixture")
	if e := os.WriteFile(source, body, 0644); e != nil {
		t.Fatal(e)
	}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	accounts, e := auth.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	token, e := securestore.SecretFile(filepath.Join(cfg.ConfigDir, "setup-token"), 32, false)
	if e != nil {
		t.Fatal(e)
	}
	session, e := accounts.Setup(ctx, "testadmin", "disposable-test-password", base64.RawURLEncoding.EncodeToString(token))
	if e != nil {
		t.Fatal(e)
	}
	store, e := securestore.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	workflow := transfer.New(db, store, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("ReelScarey")}}, "test", Dependencies{Auth: accounts, Transfer: workflow, Secrets: store})
	movie, _, e := library.New(db).AddMovie(ctx, metadata.Movie{TMDbID: 11, Title: "Owned movie"}, false)
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/v1/library/imports", "/api/v1/library/imports/catalog?kind=movie", "/api/v1/library/imports/" + strings.Repeat("a", 32)} {
		if out := request(handler, "GET", path, "", nil, false); out.Code != 401 {
			t.Fatal("unprotected discovery", path, out.Code)
		}
	}
	for _, path := range []string{"/api/v1/emby", "/api/v1/emby/activity", "/api/v1/emby/monitor", "/api/v1/emby/history", "/api/v1/events", "/api/v1/library/items/1/related", "/api/v1/library/items/1/franchise"} {
		if out := request(handler, "GET", path, "", nil, false); out.Code != 401 {
			t.Fatal("unprotected Emby API", path, out.Code)
		}
	}
	if out := request(handler, "PUT", "/api/v1/emby", `{"url":"http://emby:8096","api_key":"private"}`, &session, false); out.Code != 403 {
		t.Fatal("Emby settings missing CSRF", out.Code)
	}
	if out := request(handler, "PUT", "/api/v1/emby", `{"url":"http://emby:8096","api_key":"private"}`, &session, true); out.Code != 200 {
		t.Fatal("Emby save", out.Code, out.Body)
	}
	if out := request(handler, "GET", "/api/v1/emby", "", &session, false); out.Code != 200 || strings.Contains(out.Body.String(), "private") {
		t.Fatal("Emby key returned", out.Code, out.Body)
	}
	for _, op := range []struct{ method, path string }{{"POST", "/api/v1/library/imports"}, {"POST", "/api/v1/library/import-files/" + strings.Repeat("a", 32) + "/register"}, {"POST", "/api/v1/library/import-files/" + strings.Repeat("a", 32) + "/skip"}, {"POST", "/api/v1/library/imports/" + strings.Repeat("a", 32) + "/cancel"}, {"DELETE", "/api/v1/library/imports/" + strings.Repeat("a", 32)}} {
		if out := request(handler, op.method, op.path, "{}", &session, false); out.Code != 403 {
			t.Fatal("missing CSRF", op, out.Code)
		}
	}
	if out := request(handler, "POST", "/api/v1/library/imports", `{"kind":"movie","root":"/arbitrary"}`, &session, true); out.Code != 400 {
		t.Fatal("arbitrary root accepted")
	}
	r := httptest.NewRequest("POST", "/api/v1/library/imports", strings.NewReader(`{"kind":"movie"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://elsewhere.invalid")
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin scan accepted")
	}
	out := request(handler, "POST", "/api/v1/library/imports", `{"kind":"movie"}`, &session, true)
	var scan transfer.LibraryScan
	if out.Code != 202 || json.Unmarshal(out.Body.Bytes(), &scan) != nil {
		t.Fatal(out.Code, out.Body)
	}
	for range 4 {
		if _, e = workflow.ProcessLibraryNext(ctx); e != nil {
			t.Fatal(e)
		}
	}
	path := "/api/v1/library/imports/" + scan.ID
	if out = request(handler, "GET", path+"?page=0", "", &session, false); out.Code != 400 {
		t.Fatal("invalid page accepted")
	}
	out = request(handler, "GET", path+"?page=1", "", &session, false)
	var page transfer.DiscoveredPage
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
		t.Fatal(out.Code, out.Body)
	}
	filePath := "/api/v1/library/import-files/" + page.Items[0].ID + "/register"
	if out = request(handler, "POST", filePath, `{"media_id":1,"reviewed":false}`, &session, true); out.Code != 400 {
		t.Fatal("explicit review bypassed")
	}
	if out = request(handler, "POST", filePath, `{"media_id":1,"reviewed":true,"path":"/elsewhere"}`, &session, true); out.Code != 400 {
		t.Fatal("client supplied file path accepted")
	}
	payload, _ := json.Marshal(transfer.ExistingRegistration{MediaID: movie.ID, Reviewed: true})
	if out = request(handler, "POST", filePath, string(payload), &session, true); out.Code != 202 {
		t.Fatal(out.Code, out.Body)
	}
	if out = request(handler, "DELETE", path, "{}", &session, true); out.Code != 409 {
		t.Fatal("queued registration discarded")
	}
	saved, _ := os.ReadFile(source)
	if string(saved) != string(body) {
		t.Fatal("discovery or review modified source")
	}
}
