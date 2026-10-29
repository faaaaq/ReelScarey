package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestDownloadClientAPIIsPrivateRevisionCheckedAndSecretFree(t *testing.T) {
	_, db, accounts, code := rawFixture(t, nil)
	ctx := context.Background()
	admin, e := accounts.Setup(ctx, "clientadmin", "fixture-client-password", code)
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
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("clients app")}}, "test", Dependencies{Auth: accounts, Transfer: service})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "fixture", Path: "/"})
			fmt.Fprint(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			fmt.Fprint(w, "2.8.3")
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	routes := []struct{ method, path string }{{"GET", "/download-clients"}, {"POST", "/download-clients"}, {"PUT", "/download-clients/defaults"}, {"PUT", "/download-clients/fixture"}, {"DELETE", "/download-clients/fixture"}, {"POST", "/download-clients/fixture/test"}}
	for _, route := range routes {
		p := "/api/v1" + route.path
		if r := request(handler, route.method, p, "{}", nil, false); r.Code != 401 {
			t.Fatal(p, r.Code)
		}
		if r := request(handler, route.method, p, "{}", &viewer, true); r.Code != 403 {
			t.Fatal(p, r.Code)
		}
		if route.method != "GET" {
			if r := request(handler, route.method, p, "{}", &admin, false); r.Code != 403 {
				t.Fatal("missing CSRF", p, r.Code)
			}
		}
	}
	body := fmt.Sprintf(`{"name":"Owned torrent","kind":"qbittorrent","url":%q,"username":"fixture","password":"private-client-password","download_path":"/completed","local_folder":"server-a"}`, upstream.URL)
	r := request(handler, "POST", "/api/v1/download-clients", body, &admin, true)
	var saved transfer.DownloadClient
	if r.Code != 201 || json.Unmarshal(r.Body.Bytes(), &saved) != nil || saved.Enabled || saved.Revision != 1 || strings.Contains(r.Body.String(), "private-client-password") {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request(handler, "PUT", "/api/v1/download-clients/defaults", fmt.Sprintf(`{"torrent":%q,"usenet":"","revision":1}`, saved.ID), &admin, true)
	if r.Code == 200 {
		t.Fatal("paused default accepted")
	}
	edit := fmt.Sprintf(`{"name":"Owned torrent","kind":"qbittorrent","url":%q,"username":"fixture","download_path":"/completed","local_folder":"server-a","enabled":true,"revision":1}`, upstream.URL)
	r = request(handler, "PUT", "/api/v1/download-clients/"+saved.ID, edit, &admin, true)
	if r.Code != 200 {
		t.Fatal("blank credential retention", r.Code, r.Body.String())
	}
	if r = request(handler, "PUT", "/api/v1/download-clients/"+saved.ID, edit, &admin, true); r.Code == 200 {
		t.Fatal("stale edit accepted")
	}
	r = request(handler, "PUT", "/api/v1/download-clients/defaults", fmt.Sprintf(`{"torrent":%q,"usenet":"","revision":1}`, saved.ID), &admin, true)
	if r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(handler, "POST", "/api/v1/download-clients/"+saved.ID+"/test", "{}", &admin, true); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = request(handler, "GET", "/api/v1/download-clients", "", &admin, false)
	if r.Code != 200 || strings.Contains(r.Body.String(), "private-client-password") || strings.Contains(r.Body.String(), `"password"`) || !strings.Contains(r.Body.String(), "server-a") {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(handler, "DELETE", "/api/v1/download-clients/"+saved.ID, `{"revision":2,"reviewed":true}`, &admin, true); r.Code == 200 {
		t.Fatal("default removed")
	}
	if r = request(handler, "PUT", "/api/v1/download-clients/defaults", `{"torrent":"","usenet":"","revision":2}`, &admin, true); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = request(handler, "DELETE", "/api/v1/download-clients/"+saved.ID, `{"revision":2}`, &admin, true); r.Code != 400 {
		t.Fatal("unreviewed removal accepted", r.Code)
	}
	if r = request(handler, "DELETE", "/api/v1/download-clients/"+saved.ID, `{"revision":2,"reviewed":true}`, &admin, true); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest("GET", "http://example.com/download-clients", nil))
	if page.Code != 200 {
		t.Fatal(page.Code)
	}
}
