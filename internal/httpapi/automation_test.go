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
	for _, path := range []string{"/automation", "/automation/runs", "/automation/items", "/quality-profiles"} {
		if w := request("GET", path, "", false, false); w.Code != 401 {
			t.Fatal(path, w.Code, w.Body.String())
		}
	}
	schedule := `{"enabled":true,"mode":"search","interval_minutes":60,"batch_size":5,"default_profile_id":1,"revision":1}`
	if w := request("PUT", "/automation", schedule, true, false); w.Code != 403 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/automation", schedule, true, true); w.Code != 200 || !strings.Contains(w.Body.String(), "waiting_indexer") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("PUT", "/automation", schedule, true, true); w.Code != 409 {
		t.Fatal("stale settings accepted", w.Code, w.Body.String())
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
