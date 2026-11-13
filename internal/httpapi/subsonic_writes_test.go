package httpapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Subsonic write endpoints must never report ok without persisting anything
// (RS-0002): every unsupported write fails explicitly with the OpenSubsonic
// not-authorized error code 50 instead of a silent acknowledgement.
func TestSubsonicWritesFailExplicitlyWithoutPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, _ := musicFixture(t, &fakeMusic{serverTokenOK: true, audioPath: path})
	request := func(rawURL string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", rawURL, nil)
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, r)
		return out
	}
	const auth = "u=reelscarey&p=test-token&v=1.16.1&c=test"
	writes := []struct{ action, query string }{
		{"scrobble", "&id=so-9"},
		{"star", "&id=so-9"},
		{"unstar", "&id=so-9"},
		{"setRating", "&id=so-9&rating=5"},
		{"savePlayQueue", "&id=so-9"},
		{"savePlayQueueByIndex", "&id=so-9"},
		{"createPlaylist", "&name=Mix&id=so-9"},
		{"updatePlaylist", "&playlistId=1"},
		{"deletePlaylist", "&id=1"},
	}
	for _, write := range writes {
		out := request("/rest/" + write.action + ".view?" + auth + write.query)
		body := out.Body.String()
		if out.Code != 200 {
			t.Fatalf("%s: HTTP %d %s", write.action, out.Code, body)
		}
		if strings.Contains(body, `status="ok"`) || strings.Contains(body, `"status":"ok"`) {
			t.Fatalf("%s reported ok without persisting anything (RS-0002)", write.action)
		}
		if !strings.Contains(body, `status="failed"`) || !strings.Contains(body, `code="50"`) {
			t.Fatalf("%s must fail with OpenSubsonic error 50: %s", write.action, body)
		}
		// The JSON encoding path must fail just as explicitly.
		out = request("/rest/" + write.action + ".view?" + auth + write.query + "&f=json")
		if !strings.Contains(out.Body.String(), `"status":"failed"`) || !strings.Contains(out.Body.String(), `"code":50`) {
			t.Fatalf("%s json encoding must fail with code 50: %s", write.action, out.Body.String())
		}
	}

	// Reads keep working: the failure policy is scoped to writes.
	for _, read := range []string{"ping", "getPlaylists", "getPlayQueue", "getStarred"} {
		out := request("/rest/" + read + ".view?" + auth)
		if out.Code != 200 || !strings.Contains(out.Body.String(), `status="ok"`) {
			t.Fatalf("%s must keep working: %d %s", read, out.Code, out.Body.String())
		}
	}
}
