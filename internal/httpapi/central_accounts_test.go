package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/auth"
)

func centralRequest(h http.Handler, method, path, body string, session auth.Session) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	req.Header.Set("X-CSRF-Token", session.CSRFToken)
	req.Header.Set("Origin", "http://example.com")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	out := httptest.NewRecorder()
	h.ServeHTTP(out, req)
	return out
}

func TestCentralAccountAPIAndPermissionMatrix(t *testing.T) {
	h, _, service, code := rawFixture(t, nil)
	ctx := context.Background()
	admin, err := service.Setup(ctx, "admin", "admin-test-password", code)
	if err != nil {
		t.Fatal(err)
	}
	out := centralRequest(h, "POST", "/api/v1/accounts", `{"username":"combined","password":"combined-test-password","requests":true,"music_player":true,"subsonic":true}`, admin)
	if out.Code != 201 {
		t.Fatalf("central account create: %d %s", out.Code, out.Body)
	}
	var created auth.Account
	if err = json.Unmarshal(out.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.RequestAccountID < 1 || created.MusicServerUserID < 1 {
		t.Fatalf("central account did not create system profiles: %+v", created)
	}
	if out = centralRequest(h, "GET", "/api/v1/accounts", "", admin); out.Code != 200 || !strings.Contains(out.Body.String(), `"music_player":true`) {
		t.Fatalf("central account list: %d %s", out.Code, out.Body)
	}
	if out = centralRequest(h, "POST", "/api/v1/accounts", `{"username":"COMBINED","password":"another-test-password"}`, admin); out.Code != 409 {
		t.Fatalf("duplicate central username: %d %s", out.Code, out.Body)
	}

	combined, err := service.Login(ctx, "combined", "combined-test-password")
	if err != nil {
		t.Fatal(err)
	}
	for _, route := range []struct {
		method, path, body string
		want               int
	}{
		{"GET", "/api/v1/requester/requests", "", 200},
		// Zero means any non-403 downstream response: authorization passed and
		// the real empty music fixture handled the request.
		{"GET", "/api/v1/library/music/artists", "", 0},
		{"GET", "/api/v1/library/music/albums/1/cover", "", 0},
		{"GET", "/api/v1/library/music/files/1/stream", "", 0},
		{"GET", "/api/v1/library/music/files/1/stream-info", "", 0},
		{"GET", "/api/v1/library/music/playlists", "", 0},
		{"POST", "/api/v1/library/music/playlists", `{"name":"Mine"}`, 0},
		{"GET", "/api/v1/library/music/preferences", "", 0},
		{"GET", "/api/v1/settings", "", 403},
		{"GET", "/api/v1/accounts", "", 403},
		{"GET", "/api/v1/library/music/scans", "", 403},
		{"POST", "/api/v1/library/music/artists", `{"musicbrainz_id":"x"}`, 403},
		{"PUT", "/api/v1/library/music/preferences", `{}`, 403},
		{"GET", "/api/v1/library/music/files/1/export", "", 403},
	} {
		if out = centralRequest(h, route.method, route.path, route.body, combined); (route.want == 0 && out.Code == 403) || (route.want != 0 && out.Code != route.want) {
			t.Fatalf("combined %s %s = %d, want %d: %s", route.method, route.path, out.Code, route.want, out.Body)
		}
	}

	playerAccount, err := service.CreateAccount(ctx, "player", "player-test-password", false, true, false)
	if err != nil {
		t.Fatal(err)
	}
	player, err := service.Login(ctx, "player", "player-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if player.Role != "user" || player.UserAccountID != playerAccount.ID {
		t.Fatalf("player session: %+v", player)
	}
	if out = centralRequest(h, "GET", "/api/v1/library/music/tracks", "", player); out.Code == 403 {
		t.Fatalf("player catalog denied: %d %s", out.Code, out.Body)
	}
	if out = centralRequest(h, "GET", "/api/v1/requester/requests", "", player); out.Code != 403 {
		t.Fatalf("player reached Requests: %d %s", out.Code, out.Body)
	}

	_, err = service.CreateAccount(ctx, "request-only", "request-test-password", true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	requester, err := service.Login(ctx, "request-only", "request-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if out = centralRequest(h, "GET", "/api/v1/requester/requests", "", requester); out.Code != 200 {
		t.Fatalf("Requests grant denied: %d %s", out.Code, out.Body)
	}
	if out = centralRequest(h, "GET", "/api/v1/library/music/tracks", "", requester); out.Code != 403 {
		t.Fatalf("Requests grant reached music player: %d %s", out.Code, out.Body)
	}

	_, err = service.CreateAccount(ctx, "unassigned", "unassigned-test-password", false, false, false)
	if err != nil {
		t.Fatal(err)
	}
	unassigned, err := service.Login(ctx, "unassigned", "unassigned-test-password")
	if err != nil {
		t.Fatal(err)
	}
	if out = centralRequest(h, "GET", "/api/v1/library", "", unassigned); out.Code != 403 {
		t.Fatalf("unassigned account reached admin API: %d", out.Code)
	}
	if out = centralRequest(h, "PUT", "/api/v1/account/password", `{"current_password":"unassigned-test-password","new_password":"unassigned-new-password"}`, unassigned); out.Code != 200 {
		t.Fatalf("central self password change: %d %s", out.Code, out.Body)
	}
	if _, err = service.Login(ctx, "unassigned", "unassigned-new-password"); err != nil {
		t.Fatal(err)
	}

	out = centralRequest(h, "PUT", "/api/v1/accounts/"+jsonNumber(created.ID), `{"disabled":true}`, admin)
	if out.Code != 200 {
		t.Fatalf("disable central account: %d %s", out.Code, out.Body)
	}
	if _, err = service.Resolve(ctx, combined.Token); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("disabled central session survived: %v", err)
	}
	if _, err = service.Login(ctx, "combined", "combined-test-password"); !errors.Is(err, auth.ErrUnauthorized) {
		t.Fatalf("disabled central account logged in: %v", err)
	}
}

func jsonNumber(id int64) string { return strconv.FormatInt(id, 10) }
