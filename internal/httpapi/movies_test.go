package httpapi

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testProvider struct {
	movieCalls int
	fail       bool
}

func (p *testProvider) Validate(_ context.Context, token string) error {
	if strings.Contains(token, "invalid") {
		return &metadata.Error{Code: "tmdb_unauthorized", Message: "Token rejected."}
	}
	return nil
}
func (p *testProvider) Search(context.Context, string, string, int) (metadata.Results, error) {
	return metadata.Results{Page: 1, TotalPages: 1, TotalResults: 1, Items: []metadata.Movie{{TMDbID: 11, Title: "Search fixture", PosterPath: "/poster.jpg"}}}, nil
}
func (p *testProvider) Movie(context.Context, string, int64) (metadata.Movie, error) {
	p.movieCalls++
	if p.fail {
		return metadata.Movie{}, &metadata.Error{Code: "tmdb_unavailable", Message: "Provider offline."}
	}
	return metadata.Movie{TMDbID: 11, Title: "Canonical provider title", Overview: "Canonical overview", PosterPath: "/poster.jpg"}, nil
}
func TestTMDbSharedDefaultStatusAndReplacement(t *testing.T) {
	provider := &testProvider{}
	handler, _, accounts, code := rawFixtureWithDefaultToken(t, provider, "test-shared-default-token")
	session, err := accounts.Setup(context.Background(), "testadmin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	read := func() map[string]any {
		out := request(handler, "GET", "/api/v1/providers/tmdb", "", &session, false)
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	body := read()
	if body["configured"] != true || body["using_default"] != true || body["default_available"] != true {
		t.Fatal("shared default not reported", body)
	}
	out := request(handler, "PUT", "/api/v1/providers/tmdb", `{"token":"test-user-provider-token-value"}`, &session, true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	body = read()
	if body["configured"] != true || body["using_default"] != false || body["default_available"] != true {
		t.Fatal("saved token did not replace the default", body)
	}
	out = request(handler, "DELETE", "/api/v1/providers/tmdb", "", &session, true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	body = read()
	if body["using_default"] != true {
		t.Fatal("removing the saved token did not fall back to the default", body)
	}
}

func request(handler http.Handler, method, path, body string, session *auth.Session, csrf bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if session != nil {
		r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
	}
	out := httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	return out
}
func TestAuthProtectionCookieAndLogout(t *testing.T) {
	handler, _, _, code := rawFixture(t, nil)
	for _, path := range []string{"/api/v1/settings", "/api/v1/dashboard", "/api/v1/library?kind=movie", "/api/v1/providers/tmdb", "/api/v1/metadata/movies?query=test"} {
		if out := request(handler, "GET", path, "", nil, false); out.Code != 401 {
			t.Fatalf("unprotected endpoint %s: %d", path, out.Code)
		}
	}
	out := request(handler, "GET", "/api/v1/auth/session", "", nil, false)
	if !strings.Contains(out.Body.String(), `"setup_required":true`) {
		t.Fatal(out.Body)
	}
	if strings.Contains(out.Body.String(), "totp_enabled") {
		t.Fatal("unauthenticated session status reveals two-factor state")
	}
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "test-only-password", "setup_token": code})
	out = request(handler, "POST", "/api/v1/auth/setup", string(body), nil, false)
	if out.Code != 201 {
		t.Fatal(out.Code, out.Body)
	}
	cookies := out.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].Path != "/" {
		t.Fatal("unsafe session cookie")
	}
	if strings.Contains(out.Body.String(), cookies[0].Value) {
		t.Fatal("raw session token exposed to JavaScript")
	}
	var session auth.Session
	if err := json.Unmarshal(out.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}
	session.Token = cookies[0].Value
	if out := request(handler, "PUT", "/api/v1/settings", validSettings, &session, false); out.Code != 403 {
		t.Fatal("CSRF not enforced")
	}
	if out := request(handler, "PUT", "/api/v1/settings", validSettings, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	out = request(handler, "POST", "/api/v1/auth/logout", "{}", &session, true)
	if out.Code != 200 || out.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	if out := request(handler, "GET", "/api/v1/settings", "", &session, false); out.Code != 401 {
		t.Fatal("revoked session accepted")
	}
	out = request(handler, "POST", "/api/v1/auth/login", `{"username":"admin","password":"wrong"}`, nil, false)
	if out.Code != 401 {
		t.Fatal("wrong password accepted")
	}
	out = request(handler, "POST", "/api/v1/auth/login", `{"username":"admin","password":"test-only-password"}`, nil, false)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	for range 10 {
		out = request(handler, "POST", "/api/v1/auth/login", `{"username":"admin","password":"wrong"}`, nil, false)
	}
	if out.Code != 429 {
		t.Fatal("authentication not rate limited")
	}
	r := httptest.NewRequest("GET", "http://evil.test/api/v1/auth/session", nil)
	out = httptest.NewRecorder()
	handler.ServeHTTP(out, r)
	if out.Code != 403 {
		t.Fatal("DNS rebinding host not rejected")
	}
}
func TestSearchAddCanonicalMetadataAndDuplicateRetry(t *testing.T) {
	provider := &testProvider{}
	handler, db, service, code := rawFixture(t, provider)
	session, err := service.Setup(context.Background(), "admin", "test-only-password", code)
	if err != nil {
		t.Fatal(err)
	}
	out := request(handler, "GET", "/api/v1/metadata/movies?query=test", "", &session, false)
	if out.Code != 409 {
		t.Fatal("unconfigured provider not reported")
	}
	token := "test-provider-token-for-api-tests"
	out = request(handler, "PUT", "/api/v1/providers/tmdb", `{"token":"`+token+`"}`, &session, true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	out = request(handler, "GET", "/api/v1/providers/tmdb", "", &session, false)
	if strings.Contains(out.Body.String(), token) || strings.Contains(out.Body.String(), "ciphertext") {
		t.Fatal("secret exposed")
	}
	out = request(handler, "PUT", "/api/v1/providers/tmdb", `{"token":"invalid-provider-token-value"}`, &session, true)
	if out.Code != 502 {
		t.Fatal("invalid replacement accepted")
	}
	out = request(handler, "GET", "/api/v1/metadata/movies?query=test", "", &session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"in_library":false`) {
		t.Fatal(out.Code, out.Body)
	}
	out = request(handler, "POST", "/api/v1/library/movies", `{"tmdb_id":11,"title":"Spoofed title"}`, &session, true)
	if out.Code != 400 {
		t.Fatal("untrusted metadata accepted")
	}
	out = request(handler, "POST", "/api/v1/library/movies", `{"tmdb_id":11,"monitored":false}`, &session, true)
	if out.Code != 201 || !strings.Contains(out.Body.String(), "Canonical provider title") {
		t.Fatal(out.Code, out.Body)
	}
	provider.fail = true
	out = request(handler, "POST", "/api/v1/library/movies", `{"tmdb_id":11,"monitored":true}`, &session, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"monitored":false`) || provider.movieCalls != 1 {
		t.Fatal("duplicate retry called offline provider or changed monitoring")
	}
	out = request(handler, "GET", "/api/v1/metadata/movies?query=test", "", &session, false)
	if !strings.Contains(out.Body.String(), `"in_library":true`) {
		t.Fatal("membership flag missing")
	}
	out = request(handler, "GET", "/api/v1/library?kind=movie&page=1", "", &session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Canonical overview") || !strings.Contains(out.Body.String(), `"total":1`) {
		t.Fatal(out.Code, out.Body)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM media_items").Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate persisted")
	}
	out = request(handler, "DELETE", "/api/v1/providers/tmdb", "", &session, true)
	if out.Code != 200 {
		t.Fatal("disconnect failed")
	}
	out = request(handler, "GET", "/api/v1/library?kind=movie", "", &session, false)
	if !strings.Contains(out.Body.String(), "Canonical provider title") {
		t.Fatal("disconnect removed library metadata")
	}
}
