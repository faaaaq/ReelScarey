package httpapi

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func TestUsenetRoutesAreAdminOnlyAndValidated(t *testing.T) {
	_, db, accounts, code := rawFixture(t, nil)
	ctx := context.Background()
	admin, e := accounts.Setup(ctx, "usenetadmin", "fixture-usenet-password", code)
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
	var sequence int
	var dbName, filename string
	if e = db.QueryRow(`PRAGMA database_list`).Scan(&sequence, &dbName, &filename); e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{ConfigDir: filepath.Dir(filename), MediaDir: "/media", DownloadsDir: "/downloads", PublicURL: "http://example.com"}
	secrets, e := securestore.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	service := transfer.New(db, secrets, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("usenet app")}}, "test", Dependencies{Auth: accounts, Transfer: service})
	id := "SABnzbd_nzo_aaa111"
	routes := []struct{ method, path, body string }{
		{"GET", "/api/v1/usenet", ""},
		{"GET", "/api/v1/usenet/news", ""},
		{"GET", "/api/v1/usenet/queue", ""},
		{"PUT", "/api/v1/usenet", `{"revision":0,"enabled":true}`},
		{"PUT", "/api/v1/usenet/news", `{"host":"news.example.com","port":563,"username":"fixture","password":"fixture-password","connections":8}`},
		{"POST", "/api/v1/usenet/queue/actions", `{"ids":["` + id + `"],"action":"pause"}`},
		{"POST", "/api/v1/usenet/speed-limits", `{"download_limit":1048576}`},
	}
	for _, route := range routes {
		if r := request(handler, route.method, route.path, route.body, nil, false); r.Code != 401 {
			t.Fatal(route.path, r.Code)
		}
		if r := request(handler, route.method, route.path, route.body, &viewer, true); r.Code != 403 || !strings.Contains(r.Body.String(), "administrator_required") {
			t.Fatal("requester reached", route.path, r.Code)
		}
		if route.method != "GET" {
			if r := request(handler, route.method, route.path, route.body, &admin, false); r.Code != 403 || !strings.Contains(r.Body.String(), "csrf_required") {
				t.Fatal("missing CSRF", route.path, r.Code)
			}
		}
	}

	if r := request(handler, "GET", "/api/v1/usenet", "", &admin, false); r.Code != 200 || !strings.Contains(r.Body.String(), `"present":false`) || !strings.Contains(r.Body.String(), `"available":false`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r := request(handler, "GET", "/api/v1/usenet/queue", "", &admin, false); r.Code != 200 || !strings.Contains(r.Body.String(), `"available":false`) {
		t.Fatal(r.Code, r.Body.String())
	}
	for _, v := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"PUT", "/api/v1/usenet", `{"revision":0,"enabled":true}`, 409, "client_not_configured"},
		{"PUT", "/api/v1/usenet/news", `{"host":"https://news.example.com","port":563,"connections":8}`, 400, "invalid_settings"},
		{"PUT", "/api/v1/usenet/news", `{"host":"news.example.com","port":0,"connections":8}`, 400, "invalid_settings"},
		{"PUT", "/api/v1/usenet/news", `{"host":"news.example.com","port":563,"username":"fixture","connections":8}`, 400, "invalid_settings"},
		{"PUT", "/api/v1/usenet/news", `{"host":"news.example.com","port":563,"connections":65}`, 400, "invalid_settings"},
		{"PUT", "/api/v1/usenet/news", `{"host":"news.example.com","port":563,"connections":8,"unexpected":true}`, 400, "invalid_json"},
		{"POST", "/api/v1/usenet/news/test", `{}`, 400, "client_not_configured"},
		{"POST", "/api/v1/usenet/queue/actions", `{"ids":[],"action":"pause"}`, 400, "invalid_selection"},
		{"POST", "/api/v1/usenet/queue/actions", `{"ids":["` + id + `"],"action":"delete","reviewed":true}`, 409, "builtin_unavailable"},
		{"POST", "/api/v1/usenet/queue/actions", `{"ids":["bad id"],"action":"pause"}`, 400, "invalid_selection"},
		{"POST", "/api/v1/usenet/queue/actions", `{"ids":["` + id + `"],"action":"delete","delete_files":true}`, 400, "review_required"},
		{"POST", "/api/v1/usenet/queue/actions", `{"ids":["` + id + `"],"action":"rename"}`, 400, "invalid_selection"},
		{"POST", "/api/v1/usenet/speed-limits", `{}`, 400, "invalid_selection"},
		{"POST", "/api/v1/usenet/speed-limits", `{"download_limit":50}`, 400, "invalid_selection"},
		{"POST", "/api/v1/usenet/speed-limits", `{"download_limit":1048576}`, 409, "builtin_unavailable"},
		{"GET", "/api/v1/usenet/queue/actions", "", 405, "method_not_allowed"},
	} {
		r := request(handler, v.method, v.path, v.body, &admin, true)
		if r.Code != v.status || !strings.Contains(r.Body.String(), `"code":"`+v.code+`"`) {
			t.Fatal(v.path, v.body, r.Code, r.Body.String())
		}
	}

	// The NZB route requires an explicit media type and validates the XML
	// before it considers the daemon. It never fetches a URL.
	req := httptest.NewRequest("POST", "/api/v1/usenet/queue", strings.NewReader(ownedNZBBody))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: admin.Token})
	req.Header.Set("X-CSRF-Token", admin.CSRFToken)
	if r := serve(handler, req); r.Code != 415 || !strings.Contains(r.Body.String(), "unsupported_media_type") {
		t.Fatal(r.Code, r.Body.String())
	}
	req = httptest.NewRequest("POST", "/api/v1/usenet/queue", strings.NewReader("<html/>"))
	req.Header.Set("Content-Type", "application/x-nzb")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: admin.Token})
	req.Header.Set("X-CSRF-Token", admin.CSRFToken)
	if r := serve(handler, req); r.Code != 400 || !strings.Contains(r.Body.String(), "unsupported_nzb") {
		t.Fatal(r.Code, r.Body.String())
	}

	// A valid provider save creates the paused row with a safe response that
	// never contains the NNTP password.
	password := "fixture-nntp-secret"
	body := `{"host":"news.example.com","port":563,"tls":true,"username":"fixture","password":"` + password + `","connections":8}`
	r := request(handler, "PUT", "/api/v1/usenet/news", body, &admin, true)
	if r.Code != 200 || strings.Contains(r.Body.String(), password) || !strings.Contains(r.Body.String(), `"configured":true`) || !strings.Contains(r.Body.String(), `"enabled":false`) {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(handler, "GET", "/api/v1/usenet", "", &admin, false); r.Code != 200 || !strings.Contains(r.Body.String(), `"present":true`) || strings.Contains(r.Body.String(), password) {
		t.Fatal(r.Code, r.Body.String())
	}
	// Re-saving the identity without the password is refused.
	if r = request(handler, "PUT", "/api/v1/usenet/news", `{"host":"other.example.com","port":563,"tls":true,"username":"fixture","connections":8}`, &admin, true); r.Code != 400 || !strings.Contains(r.Body.String(), "invalid_settings") {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(handler, "POST", "/api/v1/usenet/news/test", `{"host":"127.0.0.1","port":1,"connections":1}`, &admin, true); r.Code != 502 || !strings.Contains(r.Body.String(), "upstream_unavailable") {
		t.Fatal(r.Code, r.Body.String())
	}
}

const ownedNZBBody = `<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/2003/nzb"><file poster="fixture" date="1" subject="owned.mp4"><groups><group>alt.test</group></groups><segments><segment bytes="100" number="1">owned-fixture@invalid</segment></segments></file></nzb>`

func serve(handler http.Handler, r *http.Request) *httptest.ResponseRecorder {
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	return out
}
