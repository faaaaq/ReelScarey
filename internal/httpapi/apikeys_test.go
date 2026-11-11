package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

	radarr := httptest.NewRequest(http.MethodGet, "/radarr/api/v3/movie", nil)
	radarr.Header.Set("X-Api-Key", created.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, radarr)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong scope accepted: %d %s", rec.Code, rec.Body)
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
