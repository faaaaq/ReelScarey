package httpapi

// NEW-A1: an over-limit transcode request must be reported honestly as a busy
// failure (Subsonic code 50 + Retry-After), never as the not-found code 70.

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubsonicStreamReportsBusyTranscode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, _ := musicFixture(t, &fakeMusic{serverTokenOK: true, audioPath: path, transcodeBusy: true})
	r := httptest.NewRequest("GET", "/rest/stream.view?u=reelscarey&p=test-token&v=1.16.1&c=test&id=9", nil)
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	if !strings.Contains(out.Body.String(), `code="50"`) || !strings.Contains(out.Body.String(), "concurrent conversion limit") {
		t.Fatalf("busy transcode must be honest: %d %s", out.Code, out.Body.String())
	}
	if got := out.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2", got)
	}
	if strings.Contains(out.Body.String(), `code="70"`) {
		t.Fatal("busy must not be misreported as song-not-found")
	}
}
