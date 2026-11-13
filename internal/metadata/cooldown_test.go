package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// A TMDb 429 cooldown earned before a restart must still throttle requests
// after a new service is built on the same database (RS-0025).
func TestRateLimitCooldownSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := newClient(server.URL, server.Client())
	client.SetCooldown(NewSQLCooldown(db))
	token := "abcdefghijklmnopqrstuvwxyz012345"

	if _, err := client.Search(ctx, token, "query", 1); err == nil {
		t.Fatal("a 429 must produce an error")
	}
	if hits.Load() != 1 {
		t.Fatalf("unexpected provider traffic: %d", hits.Load())
	}
	var until int64
	if err := db.QueryRow(`SELECT cooldown_until FROM tmdb_provider_state WHERE id=1`).Scan(&until); err != nil {
		t.Fatal(err)
	}
	if until <= time.Now().Unix() {
		t.Fatalf("cooldown was not persisted: %d", until)
	}

	// Restart: a fresh client on the same database must not contact the
	// provider again while the persisted cooldown is active.
	restarted := newClient(server.URL, server.Client())
	restarted.SetCooldown(NewSQLCooldown(db))
	if _, err := restarted.Search(ctx, token, "query", 1); err == nil {
		t.Fatal("expected the persisted cooldown to throttle the request")
	}
	if hits.Load() != 1 {
		t.Fatalf("restart re-burst the provider: %d hits", hits.Load())
	}

	// Once the cooldown expires, requests flow again.
	if _, err := db.Exec(`UPDATE tmdb_provider_state SET cooldown_until=?`, time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Search(ctx, token, "query", 1); err == nil {
		t.Fatal("the fixture server still returns 429")
	}
	if hits.Load() != 2 {
		t.Fatalf("expired cooldown did not release requests: %d hits", hits.Load())
	}
}

// The rate-limit error honors the server-provided Retry-After value.
func TestRetryAfterDrivesCooldownLength(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client := newClient(server.URL, server.Client())
	_, err := client.Search(ctx, "abcdefghijklmnopqrstuvwxyz012345", "query", 1)
	limited, ok := err.(*Error)
	if !ok || limited.Code != "tmdb_rate_limited" {
		t.Fatalf("unexpected error: %v", err)
	}
	if limited.RetryAfter < 60 || limited.RetryAfter > 95 {
		t.Fatalf("Retry-After was not honored: %d", limited.RetryAfter)
	}
}
