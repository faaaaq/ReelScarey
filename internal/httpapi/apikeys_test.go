package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/auth"
)

func TestAPIKeyScopesCompatAndRevocation(t *testing.T) {
	handler, db, service, code := rawFixture(t, nil)
	ctx := context.Background()
	if _, err := service.Setup(ctx, "admin", "admin-test-password", code); err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateAPIKey(ctx, auth.APIKeyInput{Name: "Sonarr integration", Scopes: []string{"compat-sonarr"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Token) != 46 || !strings.HasPrefix(created.Token, "rs_") {
		t.Fatalf("unexpected token shape: %q", created.Token)
	}
	var stored string
	if err := db.QueryRow(`SELECT token_hash FROM api_keys WHERE id=?`, created.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, created.Token) || stored == created.Token {
		t.Fatal("plaintext token was stored")
	}

	status := httptest.NewRequest(http.MethodGet, "/sonarr/api/v3/system/status", nil)
	status.Header.Set("X-Api-Key", created.Token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, status)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"appName":"ReelScarey"`) {
		t.Fatalf("sonarr status: %d %s", rec.Code, rec.Body)
	}

	for _, path := range []string{"/radarr/api/v3/movie", "/sonarr/api/v3/movie"} {
		radarr := httptest.NewRequest(http.MethodGet, path, nil)
		radarr.Header.Set("X-Api-Key", created.Token)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, radarr)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("wrong scope accepted for %s: %d %s", path, rec.Code, rec.Body)
		}
	}
	command := httptest.NewRequest(http.MethodPost, "/api/v3/command", strings.NewReader(`{"name":"MovieSearch","movieId":1}`))
	command.Header.Set("X-Api-Key", created.Token)
	command.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, command)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-scope command accepted: %d %s", rec.Code, rec.Body)
	}

	anonymous := httptest.NewRequest(http.MethodGet, "/sonarr/api/v3/system/status", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, anonymous)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous compat accepted: %d %s", rec.Code, rec.Body)
	}

	if err := service.RevokeAPIKey(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	revoked := httptest.NewRequest(http.MethodGet, "/sonarr/api/v3/system/status", nil)
	revoked.Header.Set("X-Api-Key", created.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, revoked)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key accepted: %d %s", rec.Code, rec.Body)
	}
}

// RS-0024: a session placed in the request context must never grant
// compatibility access by mere presence. Only an administrator session may
// inspect compatibility output; requester sessions (and any other future role)
// are rejected exactly like anonymous requests. This pins the defense-in-depth
// fix so a future change to the context pre-filter cannot silently widen the
// compatibility surface.
func TestCompatAuthorizedRejectsRequesterSession(t *testing.T) {
	server := &Server{}
	r := httptest.NewRequest(http.MethodGet, "/api/v3/rootfolder", nil)
	r = r.WithContext(context.WithValue(r.Context(), sessionKey{}, auth.Session{Role: "requester"}))
	rec := httptest.NewRecorder()
	if server.compatAuthorized(rec, r, "sonarr", "/rootfolder") {
		t.Fatal("a requester session in context was granted compatibility access")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("requester session must be rejected with 401, got %d %s", rec.Code, rec.Body)
	}

	admin := httptest.NewRequest(http.MethodGet, "/api/v3/rootfolder", nil)
	admin = admin.WithContext(context.WithValue(admin.Context(), sessionKey{}, auth.Session{Role: "admin"}))
	rec = httptest.NewRecorder()
	if !server.compatAuthorized(rec, admin, "sonarr", "/rootfolder") {
		t.Fatalf("an administrator session must keep diagnostic access: %d %s", rec.Code, rec.Body)
	}
}

// RS-0022 (pinned behavior): the root-folder endpoint deliberately reports the
// real configured library paths, because real Sonarr/Radarr clients validate
// and display them when connecting. The absolute-path disclosure to scoped
// compat keys is documented in docs/SECURITY.md; this test pins the response
// shape so the disclosure stays a conscious, reviewed decision.
func TestCompatRootFoldersPinConfiguredPaths(t *testing.T) {
	handler, _ := fixture(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v3/rootfolder", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rootfolder read: %d %s", rec.Code, rec.Body)
	}
	var folders []struct {
		ID         int    `json:"id"`
		Path       string `json:"path"`
		Accessible bool   `json:"accessible"`
		FreeSpace  int    `json:"freeSpace"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &folders); err != nil {
		t.Fatal(err)
	}
	if len(folders) != 2 {
		t.Fatalf("expected the two configured roots, got %d: %s", len(folders), rec.Body)
	}
	// Paths follow the platform separator because the handler uses
	// filepath.Join(MediaDir, folder).
	wantMovie := filepath.Join("/media", "Movies")
	wantSeries := filepath.Join("/media", "TV Shows")
	if folders[0].ID != 1 || folders[1].ID != 2 || folders[0].Path != wantMovie || folders[1].Path != wantSeries {
		t.Fatalf("unexpected folder entries: %+v", folders)
	}
	// The fixture media directory does not exist, so accessible stays honest.
	if folders[0].Accessible || folders[1].Accessible {
		t.Fatalf("accessible must reflect a live stat: %+v", folders)
	}
}

func TestAPIKeyAdministrationReturnsTokenOnce(t *testing.T) {
	handler, _ := fixture(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/api-keys", strings.NewReader(`{"name":"autobrr","scopes":["compat-prowlarr"],"expires_at":0}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://example.com")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", rec.Code, rec.Body)
	}
	var created auth.CreatedAPIKey
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token == "" || created.Token == created.Prefix {
		t.Fatal("response did not contain a one-time token")
	}
	list := httptest.NewRequest(http.MethodGet, "/api/v1/api-keys", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, list)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), created.Token) {
		t.Fatalf("list leaked the token or failed: %d %s", rec.Code, rec.Body)
	}
}
