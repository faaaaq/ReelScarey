package httpapi

import (
	"context"
	"encoding/base64"
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

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func TestAuthenticatedReleaseDownloadAPI(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api":
			if r.URL.Query().Get("apikey") != "test-private-key" {
				w.WriteHeader(403)
				return
			}
			if r.URL.Query().Get("t") == "caps" {
				fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/></searching></caps>`)
			} else {
				fmt.Fprint(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><item><title>Test.Movie.2026</title><guid>one</guid><torznab:attr name="magneturl" value="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"/></item></channel></rss>`)
			}
		case "/api/v2/auth/login":
			http.SetCookie(w, &http.Cookie{Name: "SID", Value: "test-session", Path: "/"})
			fmt.Fprint(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			fmt.Fprint(w, "2.8.3")
		default:
			w.WriteHeader(404)
		}
	}))
	defer upstream.Close()
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	accounts, e := auth.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := securestore.SecretFile(filepath.Join(dir, "setup-token"), 32, false)
	if e != nil {
		t.Fatal(e)
	}
	session, e := accounts.Setup(ctx, "testadmin", "test-only-password", base64.RawURLEncoding.EncodeToString(raw))
	if e != nil {
		t.Fatal(e)
	}
	secrets, e := securestore.New(ctx, db, dir)
	if e != nil {
		t.Fatal(e)
	}
	cfg := config.Config{ConfigDir: dir, MediaDir: "/media", DownloadsDir: "/downloads", PublicURL: "http://example.com"}
	workflow := transfer.New(db, secrets, cfg, transfer.NewQBittorrent(), transfer.NewTorznab())
	handler := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("ReelScarey")}}, "test", Dependencies{Auth: accounts, Transfer: workflow})
	movie, _, e := library.New(db).AddMovie(ctx, metadata.Movie{TMDbID: 123, Title: "Test Movie"}, true)
	if e != nil {
		t.Fatal(e)
	}
	for _, url := range []string{"/api/v1/integrations/torznab", "/api/v1/integrations/qbittorrent", "/api/v1/releases?movie_id=1", "/api/v1/downloads", "/api/v1/downloads/test", "/api/v1/library/movies/1"} {
		if out := request(handler, "GET", url, "", nil, false); out.Code != 401 {
			t.Fatal("unprotected workflow route", url, out.Code)
		}
	}
	body := fmt.Sprintf(`{"url":%q,"api_key":"test-private-key"}`, upstream.URL+"/api")
	if out := request(handler, "PUT", "/api/v1/integrations/torznab", body, &session, false); out.Code != 403 {
		t.Fatal("CSRF not checked")
	}
	if out := request(handler, "PUT", "/api/v1/integrations/torznab", body, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	body = fmt.Sprintf(`{"url":%q,"username":"testadmin","password":"test-private-password","download_path":"/downloads"}`, upstream.URL)
	if out := request(handler, "PUT", "/api/v1/integrations/qbittorrent", body, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	for _, name := range []string{"torznab", "qbittorrent"} {
		out := request(handler, "GET", "/api/v1/integrations/"+name, "", &session, false)
		if out.Code != 200 || strings.Contains(out.Body.String(), "test-private") {
			t.Fatal("credential status exposed a secret", out.Body)
		}
	}
	out := request(handler, "GET", fmt.Sprintf("/api/v1/releases?movie_id=%d", movie.ID), "", &session, false)
	var results struct {
		Items []transfer.Release `json:"items"`
	}
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &results) != nil || len(results.Items) != 1 {
		t.Fatal(out.Code, out.Body)
	}
	if strings.Contains(out.Body.String(), "magnet:") || strings.Contains(out.Body.String(), "test-private") {
		t.Fatal("transport payload exposed")
	}
	if out = request(handler, "POST", "/api/v1/downloads", `{"release_id":"test","url":"http://arbitrary.invalid"}`, &session, true); out.Code != 400 {
		t.Fatal("client supplied URL accepted")
	}
	body = fmt.Sprintf(`{"release_id":%q}`, results.Items[0].ID)
	out = request(handler, "POST", "/api/v1/downloads", body, &session, true)
	if out.Code != 202 {
		t.Fatal(out.Code, out.Body)
	}
	if out = request(handler, "POST", "/api/v1/downloads", body, &session, true); out.Code != 200 {
		t.Fatal("queue retry did not return existing job", out.Code, out.Body)
	}
	if out = request(handler, "DELETE", "/api/v1/integrations/qbittorrent", "", &session, true); out.Code != 409 {
		t.Fatal("active client disconnected", out.Code)
	}
	out = request(handler, "GET", "/api/v1/downloads?page=1", "", &session, false)
	var page transfer.JobPage
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Items) != 1 || page.Total != 1 {
		t.Fatal(out.Code, out.Body)
	}
	if strings.Contains(out.Body.String(), "test-private") || strings.Contains(out.Body.String(), "magnet:") {
		t.Fatal("job leaked transport secret")
	}
	if out = request(handler, "POST", "/api/v1/downloads/"+page.Items[0].ID+"/retry", "{}", &session, false); out.Code != 403 {
		t.Fatal("retry did not enforce CSRF")
	}
	if out = request(handler, "GET", "/api/v1/downloads?page=-1", "", &session, false); out.Code != 400 {
		t.Fatal("invalid pagination accepted")
	}
}
