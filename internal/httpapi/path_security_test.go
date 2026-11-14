package httpapi

// NEW-E6 negative regression tests: the storage browser and the backup
// download endpoint must reject every path-escape encoding and must never
// leak absolute filesystem paths in an error body. These are HTTP-level tests
// because Go's net/http decodes the query string exactly once; the point is
// to pin that no double-decoding, drive prefix, UNC share or backslash form
// can escape the mounted roots.

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/backups"
	"github.com/faaaaq/ReelScarey/internal/config"
)

func TestStorageBrowserRejectsEncodedTraversalWithoutPathDisclosure(t *testing.T) {
	_, db, accounts, code := rawFixture(t, nil)
	admin, e := accounts.Setup(t.Context(), "pathadmin", "fixture-path-password", code)
	if e != nil {
		t.Fatal(e)
	}
	base := testDir(t)
	cfg := config.Config{ConfigDir: filepath.Join(base, "config"), MediaDir: filepath.Join(base, "media"), DownloadsDir: filepath.Join(base, "downloads"), BackupsDir: filepath.Join(base, "backups"), PublicURL: "http://example.com"}
	for _, dir := range []string{cfg.ConfigDir, cfg.MediaDir, cfg.DownloadsDir, cfg.BackupsDir} {
		os.Mkdir(dir, 0700)
	}
	os.WriteFile(filepath.Join(cfg.ConfigDir, "secret.txt"), []byte("config secret"), 0600)
	h := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("app")}}, "test", Dependencies{Auth: accounts})

	// Each attempt is a distinct encoding of an escape into the private
	// config directory (which is not a browsable root).
	attempts := []string{
		"?root=media&path=..%2Fconfig",                  // single-decoded ../config
		"?root=media&path=%2e%2e%2fconfig",              // percent-encoded dots
		"?root=media&path=%252e%252e%252fconfig",        // double-encoded dots
		"?root=media&path=..%5Cconfig",                  // backslash escape
		"?root=media&path=C%3A%5CWindows",               // Windows drive prefix
		"?root=media&path=%2F%2Fserver%2Fshare",         // UNC-style share
		"?root=media&path=..%00config",                  // NUL truncation
		"?root=media&path=Series%2F..%2F..%2Fconfig",    // nested escape
		"?root=config&path=",                            // private root id
		"?root=media&path=" + strings.Repeat("a", 2100), // overlong path
	}
	for _, q := range attempts {
		r := request(h, "GET", "/api/v1/system/storage"+q, "", &admin, false)
		if r.Code != 400 && r.Code != 409 {
			t.Fatalf("storage browser accepted %q: code %d body %s", q, r.Code, r.Body.String())
		}
		if strings.Contains(r.Body.String(), base) {
			t.Fatalf("storage browser disclosed the absolute root for %q: %s", q, r.Body.String())
		}
		if strings.Contains(r.Body.String(), "secret") {
			t.Fatalf("storage browser leaked private content for %q", q)
		}
	}
	// The private directory contents were never returned.
	if r := request(h, "GET", "/api/v1/system/storage?root=media&path=", "", &admin, false); r.Code != 200 || strings.Contains(r.Body.String(), "secret") {
		t.Fatal("media root browse must not expose private directories")
	}
}

func TestBackupDownloadRejectsPathLikeIdentifiers(t *testing.T) {
	_, db, accounts, code := rawFixture(t, nil)
	admin, e := accounts.Setup(t.Context(), "backupadmin", "fixture-backup-password", code)
	if e != nil {
		t.Fatal(e)
	}
	var dbPath string
	var seq int
	var dbName string
	if e = db.QueryRow(`PRAGMA database_list`).Scan(&seq, &dbName, &dbPath); e != nil {
		t.Fatal(e)
	}
	base := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Dir(dbPath), BackupsDir: filepath.Join(base, "backups"), MediaDir: filepath.Join(base, "media"), DownloadsDir: filepath.Join(base, "downloads"), PublicURL: "http://example.com"}
	os.Mkdir(cfg.BackupsDir, 0700)
	b, e := backups.New(t.Context(), db, cfg, "test", func() {})
	if e != nil {
		t.Fatal(e)
	}
	h := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("app")}}, "test", Dependencies{Auth: accounts, Backups: b})

	for _, id := range []string{
		"..", "%2e%2e", "%252e%252e", "other",
		"0000000000000000000000000000000",  // 31 hex: wrong length
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", // 32 chars: wrong alphabet
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa%2F..",
	} {
		r := request(h, "GET", "/api/v1/system/backups/"+id+"/download", "", &admin, false)
		// A literal ".." never reaches the handler: ServeMux cleans the path
		// and answers 307 before any filesystem access, which is itself a
		// rejection. Every form that does reach the handler must be 404.
		if r.Code != 404 && r.Code != 307 {
			t.Fatalf("backup download accepted %q: code %d", id, r.Code)
		}
		if strings.Contains(r.Body.String(), base) || strings.Contains(r.Body.String(), "config") {
			t.Fatalf("backup download disclosed paths for %q: %s", id, r.Body.String())
		}
	}
}
