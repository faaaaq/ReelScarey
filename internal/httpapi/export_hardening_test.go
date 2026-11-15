package httpapi

// Finished-file export handler contract for converting profiles, exercised
// against the real music service (auditMusicFixture with a nil service) and,
// when ffmpeg/ffprobe are installed, a real generated audio fixture. The
// original-codec path is covered by audit_f_music_authz_test.go and
// music_transcode_http_test.go and is not repeated here.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/auth"
)

// exportHandlerFixture writes a small real MP3 (one second of sine wave with
// metadata) into the media root and registers it as music_files row id 2.
func exportHandlerFixture(t *testing.T) (http.Handler, *auth.Session, string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed on this machine")
	}
	handler, root, db, service, code := auditMusicFixture(t, nil)
	admin, err := service.Setup(context.Background(), "audit-admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(context.Background(), `INSERT INTO music_artists
		(id, musicbrainz_id, name, created_at, updated_at) VALUES (1,'mbid-export','Export Artist',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(context.Background(), `INSERT INTO music_albums
		(id, artist_id, musicbrainz_id, title, created_at, updated_at) VALUES (1,1,'mbid-export','Export Album',1,1)`); err != nil {
		t.Fatal(err)
	}
	register := func(id int64, relative string) {
		abs := filepath.Join(root, "media", filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
			"-f", "lavfi", "-i", "sine=frequency=440:duration=1",
			"-c:a", "libmp3lame", "-b:a", "64k",
			"-metadata", "title=Export Song",
			abs).CombinedOutput(); err != nil {
			t.Fatalf("generate export fixture %s: %v: %s", abs, err, out)
		}
		if _, err := db.ExecContext(context.Background(), `INSERT INTO music_files
			(id, artist_id, album_id, relative_path, size_bytes, imported_at)
			VALUES (?,1,1,?,1,1)`, id, relative); err != nil {
			t.Fatal(err)
		}
	}
	register(2, "Music/Export Artist/Export Album/01 Song.mp3")
	register(3, "Music/Export Artist/Export Album/02 Söng.mp3")
	return handler, &admin, root
}

func TestMusicExportFinishedFileContract(t *testing.T) {
	handler, admin, root := exportHandlerFixture(t)

	out := request(handler, "GET", "/api/v1/library/music/files/2/export?profile=mp3-compatible", "", admin, false)
	if out.Code != http.StatusOK {
		t.Fatal("converted export", out.Code, out.Body.String())
	}
	if got := out.Header().Get("Content-Type"); got != "audio/mpeg" {
		t.Fatal("content type", got)
	}
	if got := out.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatal("nosniff", got)
	}
	disposition := out.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, `filename="01 Song.mp3"`) || strings.Contains(disposition, "/") {
		t.Fatal("content disposition", disposition)
	}
	if out.Header().Get("Accept-Ranges") != "bytes" {
		t.Fatal("finished exports must support byte ranges")
	}
	body := out.Body.Bytes()
	if len(body) == 0 || !bytes.HasPrefix(body, []byte("ID3")) {
		t.Fatalf("exported audio must be a finished metadata-bearing file, got %d bytes", len(body))
	}
	if strings.Contains(string(body), root) {
		t.Fatal("export disclosed an absolute path")
	}

	// Non-ASCII download names travel as an RFC 5987 filename* value with an
	// ASCII-safe fallback.
	out = request(handler, "GET", "/api/v1/library/music/files/3/export?profile=mp3-compatible", "", admin, false)
	if out.Code != http.StatusOK {
		t.Fatal("non-ascii export", out.Code, out.Body.String())
	}
	disposition = out.Header().Get("Content-Disposition")
	if !strings.Contains(disposition, `filename*=UTF-8''02%20S%C3%B6ng.mp3`) ||
		!strings.Contains(disposition, `filename="02 S_ng.mp3"`) {
		t.Fatal("non-ascii content disposition", disposition)
	}

	// Byte-range support on the finished output.
	rangeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/library/music/files/2/export?profile=mp3-compatible", nil)
	rangeRequest.Header.Set("Range", "bytes=0-2")
	rangeRequest.AddCookie(&http.Cookie{Name: cookieName, Value: admin.Token})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, rangeRequest)
	if recorder.Code != http.StatusPartialContent || recorder.Body.String() != "ID3" {
		t.Fatal("range request", recorder.Code, recorder.Body.String())
	}
}
