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
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/subtitle"
)

type subtitleProviderFixture struct{ err error }

func (p *subtitleProviderFixture) Test(context.Context, subtitle.Credentials) error { return p.err }
func (*subtitleProviderFixture) Search(context.Context, subtitle.Credentials, subtitle.Target, []string) ([]subtitle.Candidate, error) {
	return nil, nil
}
func (*subtitleProviderFixture) Fetch(context.Context, subtitle.Credentials, subtitle.Candidate) (subtitle.Download, error) {
	return subtitle.Download{}, nil
}
func TestSubtitlesAPIAuthCSRFRevisionsAndWriteOnlyCredentials(t *testing.T) {
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
	provider := &subtitleProviderFixture{}
	cfg := config.Config{PublicURL: "http://example.com", ConfigDir: dir}
	service := subtitle.New(db, secrets, cfg, provider)
	h := New(db, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("test")}}, "test", Dependencies{Auth: accounts, Subtitle: service})
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
	for _, path := range []string{"/subtitles/preferences", "/providers/opensubtitles", "/subtitles/files", "/subtitles/files/1"} {
		if w := request("GET", path, "", false, false); w.Code != 401 {
			t.Fatal(path, w.Code)
		}
	}
	for _, v := range []struct{ method, path, body string }{{"PUT", "/subtitles/preferences", `{}`}, {"PUT", "/providers/opensubtitles", `{}`}, {"DELETE", "/providers/opensubtitles", ``}, {"POST", "/providers/opensubtitles/test", `{}`}, {"POST", "/subtitles/files/1/search", `{}`}, {"POST", "/subtitles/candidates/opaque/download", `{"reviewed":true}`}} {
		if w := request(v.method, v.path, v.body, true, false); w.Code != 403 {
			t.Fatal(v.path, w.Code)
		}
	}
	w := request("GET", "/subtitles/preferences", "", true, false)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct {
		Preferences subtitle.Preferences `json:"preferences"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if result.Preferences.Enabled || len(result.Preferences.Tracks) != 3 {
		t.Fatal(result)
	}
	result.Preferences.Enabled = true
	result.Preferences.DownloadFormat = "original"
	body, _ := json.Marshal(result.Preferences)
	if w = request("PUT", "/subtitles/preferences", string(body), true, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"download_format":"original"`) {
		t.Fatal("format not saved", w.Body.String())
	}
	if w = request("PUT", "/subtitles/preferences", string(body), true, true); w.Code != 409 {
		t.Fatal("stale preferences accepted", w.Code)
	}
	if w = request("PUT", "/providers/opensubtitles", `{"username":"fixture","password":"private-password","api_key":"private-key","user_agent":"ReelScarey v0.7.0","url":"http://unsafe"}`, true, true); w.Code != 400 {
		t.Fatal("unknown provider URL accepted", w.Code)
	}
	credentials := `{"username":"fixture","password":"private-password","api_key":"private-key","user_agent":"ReelScarey v0.7.0"}`
	if w = request("PUT", "/providers/opensubtitles", credentials, true, true); w.Code != 200 || strings.Contains(w.Body.String(), "private-password") || strings.Contains(w.Body.String(), "private-key") {
		t.Fatal("write-only provider contract", w.Code, w.Body.String())
	}
	if w = request("PUT", "/providers/opensubtitles", `{"username":"fixture","user_agent":"ReelScarey v0.7.0"}`, true, true); w.Code != 200 {
		t.Fatal("unchanged secrets required", w.Code, w.Body.String())
	}
	if w = request("POST", "/subtitles/candidates/opaque/download", `{"reviewed":false}`, true, true); w.Code != 400 {
		t.Fatal("review bypassed", w.Code)
	}
	if w = request("POST", "/subtitles/candidates/opaque/download", `{"reviewed":true,"path":"/unsafe"}`, true, true); w.Code != 400 {
		t.Fatal("path injection accepted", w.Code)
	}
	if w = request("GET", "/subtitles/files?page=0", "", true, false); w.Code != 400 {
		t.Fatal("invalid page", w.Code)
	}
	if w = request("GET", "/subtitles/files/1", "", true, false); w.Code != 404 {
		t.Fatal("unknown media", w.Code)
	}
	provider.err = &subtitle.Error{Code: "provider_auth", Message: "Provider rejected credentials."}
	if w = request("POST", "/providers/opensubtitles/test", `{}`, true, true); w.Code != 502 {
		t.Fatal("upstream auth must not log out app", w.Code, w.Body.String())
	}
	if w = request("GET", "/subtitles/preferences", "", true, false); w.Code != 200 {
		t.Fatal("app session lost", w.Code)
	}
	if w = request("DELETE", "/providers/opensubtitles", "", true, true); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = request("GET", "/providers/opensubtitles", "", true, false); w.Code != 200 || !strings.Contains(w.Body.String(), `"configured":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
}
