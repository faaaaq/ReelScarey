package httpapi

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func TestTorrentManagementRoutesAreAdminOnlyAndValidated(t *testing.T) {
	_, db, accounts, code := rawFixture(t, nil)
	ctx := context.Background()
	admin, e := accounts.Setup(ctx, "torrentadmin", "fixture-torrent-password", code)
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
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("torrents app")}}, "test", Dependencies{Auth: accounts, Transfer: service})
	hash := strings.Repeat("ab", 20)
	valid := `{"hashes":["` + hash + `"],"action":"pause"}`
	routes := []struct{ method, path, body string }{
		{"POST", "/api/v1/torrents/actions", valid},
		{"POST", "/api/v1/torrents/speed-limits", `{"alt_enabled":true}`},
		{"POST", "/api/v1/torrents/" + hash + "/files", `{"indexes":[0],"priority":0}`},
		{"GET", "/api/v1/torrents", ""},
		{"GET", "/api/v1/torrents/" + hash, ""},
	}
	for _, route := range routes {
		if r := request(handler, route.method, route.path, route.body, nil, false); r.Code != 401 {
			t.Fatal(route.path, r.Code)
		}
		if r := request(handler, route.method, route.path, route.body, &viewer, true); r.Code != 403 || !strings.Contains(r.Body.String(), "administrator_required") {
			t.Fatal("requester reached", route.path, r.Code)
		}
		if route.method == "POST" {
			if r := request(handler, route.method, route.path, route.body, &admin, false); r.Code != 403 || !strings.Contains(r.Body.String(), "csrf_required") {
				t.Fatal("missing CSRF", route.path, r.Code)
			}
		}
	}
	// The built-in client is not configured, so valid requests stop at the
	// availability check while invalid ones are rejected first.
	for _, v := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"POST", "/api/v1/torrents/actions", valid, 409, "builtin_unavailable"},
		{"POST", "/api/v1/torrents/actions", `{"hashes":[],"action":"pause"}`, 400, "invalid_selection"},
		{"POST", "/api/v1/torrents/actions", `{"hashes":["` + hash + `"],"action":"set_location"}`, 400, "invalid_selection"},
		{"POST", "/api/v1/torrents/actions", `{"hashes":["` + hash + `"],"action":"remove","delete_files":true}`, 400, "review_required"},
		{"POST", "/api/v1/torrents/actions", `{"hashes":["` + hash + `"],"action":"pause","location":"/"}`, 400, "invalid_json"},
		{"POST", "/api/v1/torrents/actions", `{"hashes":["` + strings.Repeat(hash+`","`, 499) + hash + `"],"action":"pause"}`, 409, "builtin_unavailable"},
		{"POST", "/api/v1/torrents/speed-limits", `{}`, 400, "invalid_selection"},
		{"POST", "/api/v1/torrents/speed-limits", `{"download_limit":1024}`, 409, "builtin_unavailable"},
		{"POST", "/api/v1/torrents/" + hash + "/files", `{"indexes":[0]}`, 400, "invalid_selection"},
		{"POST", "/api/v1/torrents/" + hash + "/files", `{"indexes":[0],"priority":3}`, 400, "invalid_selection"},
		{"POST", "/api/v1/torrents/" + hash + "/files", `{"indexes":[0],"priority":6}`, 409, "builtin_unavailable"},
		{"GET", "/api/v1/torrents/" + hash + "/files", "", 405, "method_not_allowed"},
		{"GET", "/api/v1/torrents/actions", "", 405, "method_not_allowed"},
		{"POST", "/api/v1/torrents/" + hash + "/pause", "", 409, "builtin_unavailable"},
	} {
		r := request(handler, v.method, v.path, v.body, &admin, true)
		if r.Code != v.status || !strings.Contains(r.Body.String(), `"code":"`+v.code+`"`) {
			t.Fatal(v.path, v.body, r.Code, r.Body.String())
		}
	}
	if r := request(handler, "GET", "/api/v1/torrents", "", &admin, false); r.Code != 200 || !strings.Contains(r.Body.String(), `"server":null`) {
		t.Fatal(r.Code, r.Body.String())
	}
}
