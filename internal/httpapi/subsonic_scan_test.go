package httpapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/music"
)

// RS-0018: startScan is a settings-level operation, so it requires
// CanSettings() exactly like the administrator scan controls behind the
// session API. getScanStatus stays available to every authenticated account:
// it is a harmless read that Subsonic clients poll during sync. startScan
// remains the honest no-op it has always been; nothing here wires it to a real
// scan, the gate only stops unprivileged accounts from seeing an ok response.
func TestSubsonicStartScanRequiresSettingsRole(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	restricted := music.MusicServerUser{
		ID: 4, Username: "kid",
		DownloadRole: true, PlaylistRole: true, CoverArtRole: true,
	}
	handler, _ := musicFixture(t, &fakeMusic{serverTokenOK: true, serverUser: restricted, serverSongCount: 7, audioPath: path})
	request := func(rawURL string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", rawURL, nil)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	const auth = "u=kid&p=test-token&v=1.16.1&c=test"

	// (a) A named account without the settings role is refused with Subsonic
	// code 50 in the default XML form.
	out := request("/rest/startScan.view?" + auth)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `status="failed"`) || !strings.Contains(out.Body.String(), `code="50"`) {
		t.Fatalf("unprivileged startScan must be denied: %d %s", out.Code, out.Body.String())
	}

	// (e) The JSON response variant reports the same denial.
	out = request("/rest/startScan.view?" + auth + "&f=json")
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"status":"failed"`) || !strings.Contains(out.Body.String(), `"code":50`) {
		t.Fatalf("unprivileged startScan must be denied over JSON: %d %s", out.Code, out.Body.String())
	}

	// getScanStatus stays open to the same account and reports the unchanged
	// registered song count with scanning=false.
	out = request("/rest/getScanStatus.view?" + auth)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `scanning="false"`) || !strings.Contains(out.Body.String(), `count="7"`) {
		t.Fatalf("getScanStatus must stay available: %d %s", out.Code, out.Body.String())
	}

	// (d) The denied request must not change any scan state, and a subsequent
	// authorized request still reports the same honest status.
	out = request("/rest/getScanStatus.view?" + auth)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `count="7"`) {
		t.Fatalf("denied startScan changed scan state: %d %s", out.Code, out.Body.String())
	}
}

func TestSubsonicStartScanAllowsAdminRoleAndLegacyToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}

	// (b) An administrator-role account gets a non-error response.
	handler, _ := musicFixture(t, &fakeMusic{
		serverTokenOK: true, serverUser: music.MusicServerUser{ID: 9, Username: "boss", AdminRole: true},
		serverSongCount: 7, audioPath: path,
	})
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest("GET", "/rest/startScan.view?u=boss&p=test-token&v=1.16.1&c=test", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `status="ok"`) {
		t.Fatalf("admin startScan must succeed: %d %s", out.Code, out.Body.String())
	}

	// (c) The legacy shared token keeps working (back-compat): it resolves to
	// the full-rights sentinel account, so startScan is still allowed.
	handler, _ = musicFixture(t, &fakeMusic{serverTokenOK: true, serverSongCount: 7, audioPath: path})
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, httptest.NewRequest("GET", "/rest/startScan.view?u=reelscarey&p=test-token&v=1.16.1&c=test", nil))
	if out.Code != 200 || !strings.Contains(out.Body.String(), `status="ok"`) {
		t.Fatalf("legacy-token startScan must keep working: %d %s", out.Code, out.Body.String())
	}
}

// (f) Missing or malformed credentials fail exactly as any other Subsonic
// action does: code 40 for a wrong or absent password, independent of roles.
func TestSubsonicStartScanMalformedCredentialsUnchanged(t *testing.T) {
	handler, _ := musicFixture(t, &fakeMusic{serverTokenOK: true})
	for _, query := range []string{
		"v=1.16.1&c=test",                 // no credentials at all
		"u=kid&v=1.16.1&c=test",           // username without a password
		"u=kid&p=wrong&v=1.16.1&c=test",   // wrong password
		"u=kid&t=&s=cafe&v=1.16.1&c=test", // md5 form missing the token
	} {
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, httptest.NewRequest("GET", "/rest/startScan.view?"+query, nil))
		if out.Code != 200 || !strings.Contains(out.Body.String(), `code="40"`) {
			t.Fatalf("malformed credentials must fail as today: %q %d %s", query, out.Code, out.Body.String())
		}
	}
}
