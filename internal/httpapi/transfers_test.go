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
		case "/sab/api":
			if r.URL.Query().Get("apikey") != "test-private-key" {
				w.WriteHeader(403)
				return
			}
			if r.URL.Query().Get("mode") == "version" {
				fmt.Fprint(w, `{"version":"5.1.3"}`)
			} else {
				fmt.Fprint(w, `{"categories":["reelscarey"]}`)
			}
		case "/newznab":
			if r.URL.Query().Get("t") == "caps" {
				fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/></searching></caps>`)
			} else {
				fmt.Fprintf(w, `<rss><channel><item><title>Owned NZB</title><guid>nzb-one</guid><enclosure url="http://%s/nzb" length="100" type="application/x-nzb"/></item></channel></rss>`, r.Host)
			}
		case "/nzb":
			fmt.Fprint(w, `<nzb><file subject="owned"><groups><group>alt.test</group></groups><segments><segment bytes="100" number="1">owned@invalid</segment></segments></file></nzb>`)
		case "/api":
			if r.URL.Query().Get("apikey") != "test-private-key" {
				w.WriteHeader(403)
				return
			}
			if r.URL.Query().Get("t") == "caps" {
				fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/></searching></caps>`)
			} else {
				if r.URL.Query().Get("cat") == "5000" {
					if r.URL.Query().Get("q") != "TV fixture S01E01" {
						t.Error("wrong TV search target")
					}
					fmt.Fprint(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><item><title>TV.S01E01</title><guid>tv-one</guid><torznab:attr name="magneturl" value="magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"/></item></channel></rss>`)
					return
				}
				fmt.Fprintf(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><item><title>Test.Movie.2026</title><guid>one</guid><comments>http://%s/details/one</comments><pubDate>Thu, 22 Oct 2026 10:00:00 +0000</pubDate><torznab:attr name="magneturl" value="magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"/></item></channel></rss>`, r.Host)
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
	for _, url := range []string{"/api/v1/indexers", "/api/v1/indexers/status", "/api/v1/library/items/1", "/api/v1/integrations/torznab", "/api/v1/integrations/qbittorrent", "/api/v1/integrations/sabnzbd", "/api/v1/releases?movie_id=1", "/api/v1/downloads", "/api/v1/downloads/test", "/api/v1/library/movies/1"} {
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
	if results.Items[0].Published != "2026-10-22T10:00:00Z" || results.Items[0].InfoURL == "" || !strings.HasPrefix(results.Items[0].InfoURL, upstream.URL+"/details/") {
		t.Fatal("release date/details missing from API response", results.Items[0])
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
	movieJobID := page.Items[0].ID
	if _, e = db.Exec(`UPDATE downloads SET state='completed' WHERE id=?`, movieJobID); e != nil {
		t.Fatal(e)
	}
	out = request(handler, "GET", "/api/v1/downloads?page=1", "", &session, false)
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &page) != nil || len(page.Items) != 0 || page.Total != 0 {
		t.Fatal("completed download remained in the queue", out.Code, out.Body)
	}
	out = request(handler, "GET", "/api/v1/downloads/"+movieJobID, "", &session, false)
	var completedJob transfer.Job
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &completedJob) != nil || completedJob.State != "completed" {
		t.Fatal("direct completed job read failed", out.Code, out.Body)
	}
	show := metadata.Series{Movie: metadata.Movie{TMDbID: 456, Title: "TV fixture"}, Seasons: []metadata.SeasonSummary{{TMDbID: 457, Number: 1, Title: "Season 1", EpisodeCount: 1}}}
	item, _, e := library.New(db).AddSeries(ctx, show, true)
	if e != nil {
		t.Fatal(e)
	}
	if e = library.New(db).SaveSeason(ctx, item.ID, metadata.Season{SeasonSummary: show.Seasons[0], Episodes: []metadata.Episode{{TMDbID: 458, Number: 1, Title: "Pilot"}}}, 1); e != nil {
		t.Fatal(e)
	}
	tvURL := fmt.Sprintf("/api/v1/library/series/%d/seasons/1/releases?episode=1", item.ID)
	if out = request(handler, "GET", tvURL, "", nil, false); out.Code != 401 {
		t.Fatal("TV search unprotected")
	}
	out = request(handler, "GET", tvURL, "", &session, false)
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &results) != nil || len(results.Items) != 1 {
		t.Fatal(out.Code, out.Body)
	}
	out = request(handler, "POST", "/api/v1/downloads", fmt.Sprintf(`{"release_id":%q}`, results.Items[0].ID), &session, true)
	var queued struct {
		Item transfer.Job `json:"item"`
	}
	if out.Code != 202 || json.Unmarshal(out.Body.Bytes(), &queued) != nil || queued.Item.Kind != "series" {
		t.Fatal(out.Code, out.Body)
	}
	j := queued.Item
	mappingURL := "/api/v1/downloads/" + j.ID + "/mapping"
	if _, e = db.Exec(`INSERT INTO download_tv_files(download_id,file_index,name,source_path,size_bytes) VALUES(?,0,'TV.S01E01.mp4',?,20)`, j.ID, "ReelScarey/"+j.ID+"/TV.S01E01.mp4"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE downloads SET state='blocked',error_code='map_episodes' WHERE id=?`, j.ID); e != nil {
		t.Fatal(e)
	}
	body = fmt.Sprintf(`{"files":[{"file_index":0,"episode_ids":[%d]}],"skip_episode_ids":[]}`, j.Targets[0].EpisodeID)
	if out = request(handler, "POST", mappingURL, body, &session, false); out.Code != 403 {
		t.Fatal("mapping CSRF missing")
	}
	if out = request(handler, "POST", mappingURL, `{"files":[],"source_path":"/arbitrary"}`, &session, true); out.Code != 400 {
		t.Fatal("mapping accepted supplied path")
	}
	if out = request(handler, "POST", mappingURL, body, &session, true); out.Code != 200 || !strings.Contains(out.Body.String(), `"state":"importing"`) {
		t.Fatal(out.Code, out.Body)
	}
	managedBody := fmt.Sprintf(`{"name":"Managed fixture","kind":"torznab","url":%q,"api_key":"test-private-key","enabled":true}`, upstream.URL+"/api")
	if out = request(handler, "POST", "/api/v1/indexers", managedBody, &session, false); out.Code != 403 {
		t.Fatal("indexer create lacked CSRF")
	}
	if out = request(handler, "POST", "/api/v1/indexers", `{"name":"bad","unexpected":"field"}`, &session, true); out.Code != 400 {
		t.Fatal("unknown indexer field accepted")
	}
	out = request(handler, "POST", "/api/v1/indexers", managedBody, &session, true)
	var managed transfer.IndexerStatus
	if out.Code != 201 || json.Unmarshal(out.Body.Bytes(), &managed) != nil || managed.ID == "" || strings.Contains(out.Body.String(), "test-private-key") {
		t.Fatal("managed credential contract", out.Code, out.Body)
	}
	base := "/api/v1/indexers/" + managed.ID
	if out = request(handler, "PUT", base+"/enabled", `{"enabled":false,"revision":1}`, &session, false); out.Code != 403 {
		t.Fatal("toggle lacked CSRF")
	}
	if out = request(handler, "PUT", base+"/enabled", `{"enabled":false,"revision":1}`, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	if out = request(handler, "DELETE", base, `{"revision":1}`, &session, true); out.Code != 409 {
		t.Fatal("stale deletion accepted")
	}
	if out = request(handler, "DELETE", base, `{"revision":2}`, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	if out = request(handler, "GET", "/api/v1/library/items/1?page=1", "", &session, false); out.Code != 200 || !strings.Contains(out.Body.String(), "history_count") {
		t.Fatal(out.Code, out.Body)
	}
	if out = request(handler, "POST", "/api/v1/library/items/1/metadata", "{}", &session, false); out.Code != 403 {
		t.Fatal("metadata mutation lacked CSRF")
	}
	if out = request(handler, "POST", "/api/v1/library/items/1/metadata", "{}", &session, true); out.Code != 503 {
		t.Fatal("missing metadata dependency not handled")
	}
	// Usenet shares normal authentication, CSRF, opaque release selection and
	// write-only credentials; it can coexist with an active torrent client.
	body = fmt.Sprintf(`{"url":%q,"api_key":"test-private-key","download_path":"/completed"}`, upstream.URL+"/sab")
	for _, op := range []struct{ method, path string }{{"PUT", "/api/v1/integrations/sabnzbd"}, {"DELETE", "/api/v1/integrations/sabnzbd"}, {"POST", "/api/v1/integrations/sabnzbd/test"}} {
		if out = request(handler, op.method, op.path, body, &session, false); out.Code != 403 {
			t.Fatal("SAB CSRF missing", out.Code)
		}
	}
	if out = request(handler, "PUT", "/api/v1/integrations/sabnzbd", body, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	if out = request(handler, "GET", "/api/v1/integrations/sabnzbd", "", &session, false); out.Code != 200 || strings.Contains(out.Body.String(), "test-private-key") {
		t.Fatal("SAB credential leak", out.Body)
	}
	body = fmt.Sprintf(`{"name":"Owned Newznab","kind":"newznab","url":%q,"api_key":"test-private-key","enabled":true}`, upstream.URL+"/newznab")
	if out = request(handler, "POST", "/api/v1/indexers", body, &session, true); out.Code != 201 {
		t.Fatal("Newznab create", out.Code, out.Body)
	}
	second, _, e := library.New(db).AddMovie(ctx, metadata.Movie{TMDbID: 999, Title: "Owned NZB movie"}, false)
	if e != nil {
		t.Fatal(e)
	}
	out = request(handler, "GET", fmt.Sprintf("/api/v1/releases?movie_id=%d", second.ID), "", &session, false)
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &results) != nil {
		t.Fatal("NZB search", out.Body)
	}
	selected := ""
	for _, r := range results.Items {
		if r.Protocol == "usenet" {
			selected = r.ID
		}
	}
	if selected == "" {
		t.Fatal("NZB missing from mixed results")
	}
	out = request(handler, "POST", "/api/v1/downloads", fmt.Sprintf(`{"release_id":%q}`, selected), &session, true)
	if out.Code != 202 || json.Unmarshal(out.Body.Bytes(), &queued) != nil || queued.Item.Protocol != "usenet" {
		t.Fatal("NZB queue", out.Code, out.Body)
	}
	if out = request(handler, "DELETE", "/api/v1/integrations/sabnzbd", "", &session, true); out.Code != 409 {
		t.Fatal("active SAB disconnected")
	}
}
