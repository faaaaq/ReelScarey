package httpapi

// Independent audit tests (FIX-OWNERSHIP: new audit_f_ file) for the music
// transcoding HTTP surface: every route sits behind the shared security
// middleware (session auth, requester denial, CSRF+same-origin for
// mutations), the optimize endpoints are bounded, and the export/stream
// paths fail closed for nonexistent or foreign file ids without leaking
// absolute filesystem paths.

import (
	"context"
	"database/sql"
	"encoding/base64"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

// auditMusicFixture mirrors musicFixture but returns the temporary root (the
// media root is root/media), the auth service and the setup code so the
// tests can mint a request-account session or seed catalog rows. Passing a
// nil music service builds the real service against that media root.
func auditMusicFixture(t *testing.T, service MusicService) (http.Handler, string, *sql.DB, *auth.Service, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := database.Open(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	assets := fstest.MapFS{"index.html": {Data: []byte("app shell")}}
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
	handler := New(db, config.Config{
		ConfigDir:    dir,
		MediaDir:     filepath.Join(dir, "media"),
		DownloadsDir: filepath.Join(dir, "downloads"),
		PublicURL:    "http://example.com",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)), assets, "test", Dependencies{
		Auth:    authService,
		Secrets: store,
		Music:   service,
	})
	return handler, dir, db, authService, base64.RawURLEncoding.EncodeToString(code)
}

// auditTranscodeRoutes is the music transcoding surface registered in
// server.go for this feature. All of them are administrator APIs.
var auditTranscodeRoutes = []struct {
	method string
	path   string
}{
	{"GET", "/api/v1/library/music/profiles"},
	{"PUT", "/api/v1/library/music/profiles/9"},
	{"DELETE", "/api/v1/library/music/profiles/9"},
	{"GET", "/api/v1/library/music/conversions"},
	{"POST", "/api/v1/library/music/conversions/1/cancel"},
	{"POST", "/api/v1/library/music/conversions/1/retry"},
	{"POST", "/api/v1/library/music/optimize/preview"},
	{"POST", "/api/v1/library/music/optimize/apply"},
	{"GET", "/api/v1/library/music/cache"},
	{"DELETE", "/api/v1/library/music/cache"},
	{"GET", "/api/v1/library/music/files/1/export?profile=original"},
	{"GET", "/api/v1/library/music/files/1/stream"},
	{"PUT", "/api/v1/library/music/preferences"},
	{"PUT", "/api/v1/library/music/albums/1/preferences"},
	{"DELETE", "/api/v1/library/music/albums/1/preferences"},
}

func TestAuditFMusicTranscodeRoutesRequireSessionAndAdminRole(t *testing.T) {
	handler, _, _, service, code := auditMusicFixture(t, &fakeMusic{})
	admin, err := service.Setup(context.Background(), "audit-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.CreateRequestAccount(context.Background(), "audit-viewer", "viewer-test-password"); err != nil {
		t.Fatal(err)
	}
	requester, err := service.Login(context.Background(), "audit-viewer", "viewer-test-password")
	if err != nil {
		t.Fatal(err)
	}

	for _, route := range auditTranscodeRoutes {
		// Unauthenticated requests never reach the handler.
		if out := request(handler, route.method, route.path, "{}", nil, false); out.Code != 401 {
			t.Fatalf("%s %s accepted an anonymous request: %d %s", route.method, route.path, out.Code, out.Body.String())
		}
		// Request accounts are denied every administrator API by default.
		out := request(handler, route.method, route.path, "{}", &requester, true)
		if out.Code != 403 || !strings.Contains(out.Body.String(), "administrator_required") {
			t.Fatalf("%s %s did not deny the requester session: %d %s", route.method, route.path, out.Code, out.Body.String())
		}
		// The administrator session passes the security middleware: the
		// recorded status comes from the handler or the method gate, never
		// from an authentication or role rejection.
		adminOut := request(handler, route.method, route.path, "{}", &admin, true)
		if adminOut.Code == http.StatusUnauthorized || adminOut.Code == http.StatusForbidden {
			t.Fatalf("%s %s rejected a valid administrator session: %d %s", route.method, route.path, adminOut.Code, adminOut.Body.String())
		}
	}
}

func TestAuditFMusicOptimizeApplyRequiresCSRF(t *testing.T) {
	fake := &fakeMusic{}
	handler, _, _, service, code := auditMusicFixture(t, fake)
	admin, err := service.Setup(context.Background(), "audit-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}

	// No CSRF token: the security middleware must refuse the mutation
	// before the handler runs.
	out := request(handler, "POST", "/api/v1/library/music/optimize/apply", `{"profile":"opus-high","file_ids":[3]}`, &admin, false)
	if out.Code != 403 || !strings.Contains(out.Body.String(), "csrf_required") {
		t.Fatal("optimize apply without CSRF", out.Code, out.Body.String())
	}
	if fake.appliedProfile != "" {
		t.Fatal("the handler ran without a CSRF token")
	}

	// A cross-site mutation is refused even when a token is attached:
	// same-origin validation is part of the same middleware chain.
	cross := httptest.NewRequest("POST", "/api/v1/library/music/optimize/apply", strings.NewReader(`{"profile":"opus-high","file_ids":[3]}`))
	cross.Header.Set("Content-Type", "application/json")
	cross.Header.Set("Origin", "http://evil.example")
	cross.Header.Set("Sec-Fetch-Site", "cross-site")
	cross.AddCookie(&http.Cookie{Name: cookieName, Value: admin.Token})
	cross.Header.Set("X-CSRF-Token", admin.CSRFToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, cross)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), "cross_origin") {
		t.Fatal("cross-site optimize apply", rec.Code, rec.Body.String())
	}
}

func TestAuditFMusicOptimizeRequestBounds(t *testing.T) {
	handler, _, _, service, code := auditMusicFixture(t, &fakeMusic{})
	admin, err := service.Setup(context.Background(), "audit-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	tooMany := `{"profile":"opus-high","file_ids":[` +
		strings.TrimSuffix(strings.Repeat("1,", 501), ",") + `]}`
	if out := request(handler, "POST", "/api/v1/library/music/optimize/preview", tooMany, &admin, true); out.Code != 400 {
		t.Fatal("preview accepted more than 500 file ids", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/optimize/apply", tooMany, &admin, true); out.Code != 400 {
		t.Fatal("apply accepted more than 500 file ids", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/optimize/apply", `{"profile":"opus-high","file_ids":[]}`, &admin, true); out.Code != 400 {
		t.Fatal("apply accepted an empty id list", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/optimize/apply", `{"profile":"opus-high","file_ids":[0]}`, &admin, true); out.Code != 400 {
		t.Fatal("apply accepted a zero file id", out.Code, out.Body.String())
	}
	if out := request(handler, "POST", "/api/v1/library/music/optimize/preview", `{"profile":"opus-high","file_ids":[0]}`, &admin, true); out.Code != 400 {
		t.Fatal("preview accepted a zero file id", out.Code, out.Body.String())
	}
	if out := request(handler, "GET", "/api/v1/library/music/conversions?page=100001", "", &admin, false); out.Code != 400 {
		t.Fatal("conversions accepted an out-of-range page", out.Code, out.Body.String())
	}
}

func TestAuditFMusicExportFailsClosedOnForeignFileIDs(t *testing.T) {
	handler, root, db, service, code := auditMusicFixture(t, nil)
	admin, err := service.Setup(context.Background(), "audit-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}

	// The secret lives in the fixture root, one level above the media root
	// (root/media), so a stale or tampered relative_path must never reach it.
	secret := "outside secret content"
	if err := os.WriteFile(filepath.Join(root, "outside-secret.txt"), []byte(secret), 0o600); err != nil {
		t.Fatal(err)
	}

	// Seed an artist, an album and registered files: one honest file plus
	// several with tampered relative paths aiming outside the media root.
	_, err = db.ExecContext(context.Background(), `INSERT INTO music_artists
		(id, musicbrainz_id, name, created_at, updated_at) VALUES (1,'mbid-audit','Audit Artist',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO music_albums
		(id, artist_id, musicbrainz_id, title, created_at, updated_at) VALUES (1,1,'mbid-audit','Audit Album',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	for id, relative := range map[int64]string{
		2: "Music/ok.mp3",                   // real file, created below
		3: "../outside-secret.txt",          // parent escape
		4: `Music\..\outside-secret.txt`,    // backslash escape
		5: "/outside-secret.txt",            // absolute path
		6: "..%2Foutside-secret.txt",        // encoded-looking junk
		7: "....//....//outside-secret.txt", // nested dot escape
	} {
		if _, err := db.ExecContext(context.Background(), `INSERT INTO music_files
			(id, artist_id, album_id, relative_path, size_bytes, imported_at) VALUES (?,?,?,?,1,1)`,
			id, 1, 1, relative); err != nil {
			t.Fatal(err)
		}
	}

	// The honest file exists inside the media root so a positive control
	// can prove the route itself works.
	if err := os.MkdirAll(filepath.Join(root, "media", "Music"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "media", "Music", "ok.mp3"), []byte("catalog bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, id := range []int64{99, 3, 4, 5, 6, 7} {
		path := "/api/v1/library/music/files/" + strconv.FormatInt(id, 10)
		out := request(handler, "GET", path+"/export?profile=original", "", &admin, false)
		if out.Code == http.StatusOK {
			t.Fatalf("export served foreign or missing file id %d", id)
		}
		if strings.Contains(out.Body.String(), secret) {
			t.Fatalf("export leaked the outside-secret content for id %d: %s", id, out.Body.String())
		}
		stream := request(handler, "GET", path+"/stream", "", &admin, false)
		if stream.Code == http.StatusOK && id != 99 {
			// The stream route shares OpenAudioFile; only the honest id may
			// ever return audio.
			t.Fatalf("stream served foreign file id %d", id)
		}
		if strings.Contains(stream.Body.String(), secret) {
			t.Fatalf("stream leaked the outside-secret content for id %d", id)
		}
	}

	// Positive control: the registered file inside the root streams and
	// exports normally.
	out := request(handler, "GET", "/api/v1/library/music/files/2/export?profile=original", "", &admin, false)
	if out.Code != 200 || out.Body.String() != "catalog bytes" {
		t.Fatal("honest export", out.Code, out.Body.String())
	}
}

func TestAuditFMusicTranscodeResponsesStayRelative(t *testing.T) {
	handler, root, db, service, code := auditMusicFixture(t, nil)
	admin, err := service.Setup(context.Background(), "audit-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO music_artists
		(id, musicbrainz_id, name, created_at, updated_at) VALUES (1,'mbid-audit','Audit Artist',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO music_albums
		(id, artist_id, musicbrainz_id, title, created_at, updated_at) VALUES (1,1,'mbid-audit','Audit Album',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO music_files
		(id, artist_id, album_id, relative_path, size_bytes, imported_at)
		VALUES (2,1,1,'Music/Audit Artist/Audit Album/01 Song.flac',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO music_conversion_jobs
		(id, kind, file_id, profile, state, source_path, output_path, created_at, updated_at)
		VALUES (5,'library',2,'opus-high','queued','Music/Audit Artist/Audit Album/01 Song.flac','',1,1)`)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/api/v1/library/music/conversions",
		"/api/v1/library/music/files",
	} {
		out := request(handler, "GET", path, "", &admin, false)
		if out.Code != 200 {
			t.Fatalf("%s read failed: %d %s", path, out.Code, out.Body.String())
		}
		// Relative catalog paths may appear, but the absolute root of the
		// fixture must never appear in a response.
		if strings.Contains(out.Body.String(), root) {
			t.Fatalf("%s disclosed an absolute path: %s", path, out.Body.String())
		}
	}
}

func TestAuditFExportHeadersCarryNoPath(t *testing.T) {
	handler, root, db, service, code := auditMusicFixture(t, nil)
	admin, err := service.Setup(context.Background(), "audit-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO music_artists
		(id, musicbrainz_id, name, created_at, updated_at) VALUES (1,'mbid-audit','Audit Artist',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO music_albums
		(id, artist_id, musicbrainz_id, title, created_at, updated_at) VALUES (1,1,'mbid-audit','Audit Album',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(context.Background(), `INSERT INTO music_files
		(id, artist_id, album_id, relative_path, size_bytes, imported_at)
		VALUES (2,1,1,'Music/Audit Artist/Audit Album/01 Song.flac',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	songDir := filepath.Join(root, "media", "Music", "Audit Artist", "Audit Album")
	if err := os.MkdirAll(songDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(songDir, "01 Song.flac"), []byte("flac"), 0o600); err != nil {
		t.Fatal(err)
	}

	out := request(handler, "GET", "/api/v1/library/music/files/2/export?profile=original", "", &admin, false)
	if out.Code != 200 || out.Body.String() != "flac" {
		t.Fatal("export of a registered file", out.Code, out.Body.String())
	}
	if disposition := out.Header().Get("Content-Disposition"); disposition != "attachment" || strings.Contains(disposition, "Song") {
		t.Fatal("export Content-Disposition must not name the file path:", disposition)
	}
	if strings.Contains(out.Body.String(), root) {
		t.Fatal("export disclosed an absolute path")
	}
}
