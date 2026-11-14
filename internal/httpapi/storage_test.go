package httpapi

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/config"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// testDir returns the temp directory in its canonical (symlink-resolved) form
// so configured media roots pass the storage browser's strict link check on
// every platform: Windows t.TempDir can return an 8.3 short path, which
// filepath.EvalSymlinks expands, making the browser reject the raw form.
func testDir(t *testing.T) string {
	t.Helper()
	dir, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return dir
}

func TestStorageRoutesAreAdminOnlyBoundedAndReadOnly(t *testing.T) {
	_, db, accounts, code := rawFixture(t, nil)
	ctx := context.Background()
	admin, e := accounts.Setup(ctx, "storageadmin", "fixture-storage-password", code)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = accounts.CreateRequestAccount(ctx, "viewer", "fixture-viewer-password"); e != nil {
		t.Fatal(e)
	}
	viewer, e := accounts.Login(ctx, "viewer", "fixture-viewer-password")
	if e != nil {
		t.Fatal(e)
	}
	base := testDir(t)
	cfg := config.Config{ConfigDir: filepath.Join(base, "config"), MediaDir: filepath.Join(base, "media"), DownloadsDir: filepath.Join(base, "downloads"), BackupsDir: filepath.Join(base, "backups"), PublicURL: "http://example.com"}
	for _, dir := range []string{cfg.ConfigDir, cfg.MediaDir, cfg.DownloadsDir, cfg.BackupsDir} {
		os.Mkdir(dir, 0700)
	}
	os.WriteFile(filepath.Join(cfg.MediaDir, "owned.mkv"), []byte("generated owned bytes"), 0600)
	h := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("storage app")}}, "test", Dependencies{Auth: accounts})
	for _, p := range []string{"/api/v1/system/storage", "/api/v1/system/storage/migration"} {
		if r := request(h, "GET", p, "", nil, false); r.Code != 401 {
			t.Fatal(r.Code)
		}
		if r := request(h, "GET", p, "", &viewer, false); r.Code != 403 {
			t.Fatal(r.Code)
		}
		if r := request(h, "POST", p, "{}", &admin, true); r.Code != 405 {
			t.Fatal(r.Code)
		}
	}
	r := request(h, "GET", "/api/v1/system/storage", "", &admin, false)
	if r.Code != 200 || !strings.Contains(r.Body.String(), "owned.mkv") || strings.Contains(r.Body.String(), "generated owned bytes") {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, suffix := range []string{"?path=../config", "?page=0", "?root=media&root=downloads", "?write=yes"} {
		if r := request(h, "GET", "/api/v1/system/storage"+suffix, "", &admin, false); r.Code != 400 {
			t.Fatal(r.Code, r.Body.String())
		}
	}
	r = request(h, "GET", "/api/v1/system/storage/migration", "", &admin, false)
	if r.Code != 200 || !strings.Contains(r.Body.String(), `"state":"none"`) {
		t.Fatal(r.Code, r.Body.String())
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "http://example.com/storage", nil))
	if page.Code != 200 {
		t.Fatal(page.Code)
	}
}
