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

// The browser-picked upload route copies one file into the title's library
// folder and queues its reviewed registration; the version-label route
// records an owner-corrected movie edition. Both stay behind the
// administrator session, the same-origin check and CSRF.
func TestDirectUploadAndFileVersionRoutes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	cfgRoot := filepath.Join(dir, "config")
	mediaDir := filepath.Join(dir, "media")
	if e := os.MkdirAll(filepath.Join(mediaDir, "Movies"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.MkdirAll(filepath.Join(dir, "downloads"), 0755); e != nil {
		t.Fatal(e)
	}
	db, e := database.Open(ctx, cfgRoot)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	accounts, e := auth.New(ctx, db, cfgRoot)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := securestore.SecretFile(filepath.Join(cfgRoot, "setup-token"), 32, false)
	if e != nil {
		t.Fatal(e)
	}
	session, e := accounts.Setup(ctx, "testadmin", "test-only-password", base64.RawURLEncoding.EncodeToString(raw))
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := securestore.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{ConfigDir: cfgRoot, MediaDir: mediaDir, DownloadsDir: filepath.Join(dir, "downloads"), PublicURL: "http://example.com"}
	workflow := transfer.New(db, secrets, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	movieYear := 2026
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("ReelScarey")}}, "test", Dependencies{Auth: accounts, Transfer: workflow})
	if _, _, e = library.New(db).AddMovie(ctx, metadata.Movie{TMDbID: 123, Title: "Test Movie", Year: &movieYear}, true); e != nil {
		t.Fatal(e)
	}

	// Every mutation requires an administrator session and CSRF.
	if out := request(handler, "PUT", "/api/v1/library/files/1/version", `{"version_label":"2019"}`, nil, false); out.Code != 401 {
		t.Fatal("the version route allowed an anonymous write", out.Code)
	}
	if out := request(handler, "PUT", "/api/v1/library/files/1/version", `{"version_label":"2019"}`, &session, false); out.Code != 403 || !strings.Contains(out.Body.String(), "csrf_required") {
		t.Fatal("the version route allowed a write without CSRF", out.Code, out.Body.String())
	}
	if out := request(handler, "PUT", "/api/v1/library/files/1/version", `{"version_label":"2019"}`, &session, true); out.Code != 400 || !strings.Contains(out.Body.String(), "invalid_file") {
		t.Fatal("the version route accepted an unknown file", out.Code, out.Body.String())
	}

	// Prepare resolves the destination folder before any bytes move.
	if _, e := workflow.DirectUploadPrepare(ctx, "movie", "", 1); e != nil {
		t.Fatalf("prepare failed directly: %v", e)
	}
	prepare := request(handler, "POST", "/api/v1/library/imports/upload/prepare", `{"kind":"movie","media_id":1}`, &session, true)
	if prepare.Code != 200 {
		t.Fatal("the destination could not be prepared", prepare.Code, prepare.Body.String())
	}
	if !strings.Contains(prepare.Body.String(), `"destination":"Movies/`) {
		t.Fatal("the destination is not inside a configured root", prepare.Body.String())
	}
	var prepared struct {
		Destination string `json:"destination"`
	}
	if e := json.Unmarshal([]byte(prepare.Body.String()), &prepared); e != nil {
		t.Fatal(e)
	}

	// The upload itself requires the same session, origin and CSRF.
	if out := uploadRequest(handler, "/api/v1/library/imports/upload?kind=movie&media_id=1&name=Test.Movie.2026.mp4", "owned upload bytes", nil, false); out.Code != 401 {
		t.Fatal("the upload route allowed an anonymous copy", out.Code)
	}
	out := uploadRequest(handler, "/api/v1/library/imports/upload?kind=movie&media_id=1&name=Test.Movie.2026.mp4", "owned upload bytes", &session, true)
	if out.Code != 202 {
		t.Fatal("the upload route refused a valid copy", out.Code, out.Body.String())
	}
	if !strings.Contains(out.Body.String(), `"state":"completed"`) {
		t.Fatal("the upload was not queued for verification", out.Body.String())
	}
	copied, e := os.ReadFile(filepath.Join(mediaDir, filepath.FromSlash(prepared.Destination), "Test.Movie.2026.mp4"))
	if e != nil {
		t.Fatal(e)
	}
	if string(copied) != "owned upload bytes" {
		t.Fatal("the copied bytes do not match the upload")
	}
	// A second upload with the same name is refused, not overwritten.
	if out = uploadRequest(handler, "/api/v1/library/imports/upload?kind=movie&media_id=1&name=Test.Movie.2026.mp4", "second", &session, true); out.Code != 409 {
		t.Fatalf("a colliding upload was not refused: %d %s", out.Code, out.Body.String())
	}

	// The re-attach route stays behind the session, origin and CSRF.
	if out := request(handler, "POST", "/api/v1/library/imports/reattach", `{"kind":"movie","media_id":1,"relative":"Movies/x.mkv","reviewed":true}`, nil, false); out.Code != 401 {
		t.Fatal("the re-attach route allowed an anonymous move", out.Code)
	}
	if out := request(handler, "POST", "/api/v1/library/imports/reattach", `{"kind":"movie","media_id":1,"relative":"Movies/x.mkv","reviewed":true}`, &session, false); out.Code != 403 || !strings.Contains(out.Body.String(), "csrf_required") {
		t.Fatal("the re-attach route allowed a move without CSRF", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/imports/reattach", `{"kind":"movie","media_id":1,"relative":"Movies/missing.mkv","reviewed":true}`, &session, true); out.Code != 400 || !strings.Contains(out.Body.String(), "invalid_selection") {
		t.Fatal("the re-attach route accepted an unregistered file", out.Code, out.Body.String())
	}
}

// uploadRequest posts a raw body with the session cookie and CSRF header the
// browser's XHR upload sends.
func uploadRequest(handler http.Handler, path, body string, session *auth.Session, csrf bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/octet-stream")
	if session != nil {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	return out
}
