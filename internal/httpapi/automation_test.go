package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/automation"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func TestAutomationAPIRequiresAuthCSRFAndRevisions(t *testing.T) {
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
	h := New(db, config.Config{PublicURL: "http://example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("test")}}, "test", Dependencies{Auth: accounts, Automation: automation.New(db, nil)})
	request := func(method, path, body string, authenticated, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://example.com/api/v1"+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "http://example.com")
		if authenticated {
			r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
		}
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/automation", "/automation/rss", "/automation/runs", "/automation/items", "/automation/item-results", "/quality-profiles", "/library/movies/1/versions"} {
		if w := request("GET", path, "", false, false); w.Code != 401 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	if w := request("POST", "/automation/search-item", `{"media_id":1}`, false, false); w.Code != 401 {
		t.Fatal("search-item must authenticate", w.Code)
	}
	if w := request("POST", "/automation/search-item", `{"media_id":1}`, true, false); w.Code != 403 {
		t.Fatal("search-item must require CSRF", w.Code)
	}
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(7,'movie',7,'Focus movie')`); e != nil {
		t.Fatal(e)
	}
	if w := request("POST", "/automation/search-item", `{"media_id":7}`, true, true); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var focusMedia string
	if e = db.QueryRow(`SELECT focus_media FROM search_schedule WHERE id=1`).Scan(&focusMedia); e != nil || focusMedia != "7" {
		t.Fatal("the focused search was not recorded", focusMedia, e)
	}
	if w := request("POST", "/automation/search-item", `{"media_id":999}`, true, true); w.Code != 404 {
		t.Fatal("unknown titles must be refused", w.Code)
	}
	if w := request("GET", "/automation/item-results?media_id=7", "", true, false); w.Code != 200 || !strings.Contains(w.Body.String(), `"outcome":"none"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e = db.Exec(`INSERT INTO automation_runs(id,started_at,state,mode) VALUES('r1',1,'completed','search');INSERT INTO automation_results(run_id,media_id,episode_id,target,outcome,message,decisions) VALUES('r1',7,NULL,'Focus movie','match','A release matched.','[]')`); e != nil {
		t.Fatal(e)
	}
	if w := request("GET", "/automation/item-results?media_id=7", "", true, false); w.Code != 200 || !strings.Contains(w.Body.String(), `"outcome":"match"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	schedule := `{"enabled":true,"mode":"search","interval_minutes":60,"batch_size":5,"default_profile_id":1,"revision":1}`
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(101,'movie',101,'Version movie'),(102,'series',102,'Version show')`); e != nil {
		t.Fatal(e)
	}
	versions := `{"enabled":true,"hd_profile_id":1,"uhd_profile_id":2,"revision":0}`
	if w := request("GET", "/library/movies/101/versions", "", true, false); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/library/movies/101/versions", versions, true, false); w.Code != 403 {
		t.Fatal("version settings need CSRF", w.Code)
	}
	if w := request("PUT", "/library/movies/101/versions", versions, true, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/library/movies/101/versions", versions, true, true); w.Code != 409 {
		t.Fatal("stale version settings accepted", w.Code)
	}
	if w := request("PUT", "/library/movies/102/versions", versions, true, true); w.Code != 404 {
		t.Fatal("TV version settings accepted", w.Code)
	}
	if w := request("DELETE", "/library/movies/101/versions", "{}", true, true); w.Code != 405 {
		t.Fatal(w.Code)
	}
	if w := request("PUT", "/automation", schedule, true, false); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/automation", schedule, true, true); w.Code != 200 || !strings.Contains(w.Body.String(), "waiting_indexer") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/automation", schedule, true, true); w.Code != 409 {
		t.Fatal("stale settings accepted", w.Code, w.Body.String())
	}
	if w := request("GET", "/automation/rss", "", true, false); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":true`) || !strings.Contains(w.Body.String(), `"interval_minutes":15`) {
		t.Fatal("release detection must be enabled every 15 minutes by default", w.Code, w.Body.String())
	}
	rss := `{"enabled":true,"interval_minutes":15,"revision":1}`
	if w := request("PUT", "/automation/rss", rss, true, false); w.Code != 403 {
		t.Fatal("release detection needs CSRF", w.Code)
	}
	if w := request("PUT", "/automation/rss", rss, true, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/automation/rss", rss, true, true); w.Code != 409 {
		t.Fatal("stale release detection settings accepted", w.Code)
	}
	if w := request("POST", "/automation/rss/run", `{}`, true, true); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/automation/rss/run", `{}`, true, true); w.Code != 409 {
		t.Fatal("duplicate release check accepted", w.Code)
	}
	if w := request("POST", "/automation/run", `{"path":"/untrusted"}`, true, true); w.Code != 400 {
		t.Fatal("unknown field accepted", w.Code)
	}
	if w := request("POST", "/automation/run", `{}`, true, true); w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("POST", "/automation/run", `{}`, true, true); w.Code != 409 {
		t.Fatal("duplicate request accepted", w.Code)
	}
	p := `{"name":"Custom","qualities":[720,1080],"cutoff":1080,"language":"english","min_seeders":2,"max_size_gib":30,"required_terms":"web","rejected_terms":"av1","revision":0}`
	if w := request("POST", "/quality-profiles", p, true, false); w.Code != 403 {
		t.Fatal("profile mutation missing CSRF", w.Code)
	}
	w := request("POST", "/quality-profiles", p, true, true)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var profile map[string]any
	if e = json.Unmarshal(w.Body.Bytes(), &profile); e != nil {
		t.Fatal(e)
	}
	if profile["revision"] != float64(1) {
		t.Fatal(profile)
	}
	if w := request("POST", "/quality-profiles", strings.Replace(p, `"cutoff":1080`, `"cutoff":2160`, 1), true, true); w.Code != 400 {
		t.Fatal("invalid cutoff accepted", w.Code)
	}
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',1,'Fixture')`); e != nil {
		t.Fatal(e)
	}
	if w := request("PUT", "/automation/items/1", `{"profile_id":1,"revision":0,"monitored":false}`, true, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/automation/items/1", `{"profile_id":2,"revision":0}`, true, true); w.Code != 409 {
		t.Fatal("assignment conflict ignored", w.Code)
	}
}
