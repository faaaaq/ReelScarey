package metadata

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// Racing 429 responses with different Retry-After values must never shorten
// an already-earned cooldown, in memory or in the persisted provider state
// (NEW-D1). The shorter, later write must lose against the longer deadline.
func TestConcurrent429CooldownWritesNeverShortenCooldown(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var client *Client
	var hits atomic.Int64
	arrived := make(chan struct{}, 2)
	bothInFlight := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		arrived <- struct{}{}
		if n == 1 {
			// Hold the first response until both requests are in flight, so
			// both have already passed the cooldown gate client-side.
			<-bothInFlight
			w.Header().Set("Retry-After", "300")
		} else {
			// Wait until the 300-second cooldown from the first response has
			// been recorded, so this shorter Retry-After is processed last.
			deadline := time.Now().Add(5 * time.Second)
			for {
				client.mu.Lock()
				longer := client.retryAt.After(time.Now().Add(280 * time.Second))
				client.mu.Unlock()
				if longer {
					break
				}
				if time.Now().After(deadline) {
					t.Error("first 429 cooldown was never recorded")
					break
				}
				time.Sleep(2 * time.Millisecond)
			}
			w.Header().Set("Retry-After", "5")
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client = newClient(server.URL, server.Client())
	client.SetCooldown(NewSQLCooldown(db))

	var wg sync.WaitGroup
	results := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = client.Movie(ctx, token, 11)
		}()
	}
	for range 2 {
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			t.Fatal("both requests never reached the provider")
		}
	}
	close(bothInFlight)
	wg.Wait()
	for i, err := range results {
		var e *Error
		if !errors.As(err, &e) || e.Code != "tmdb_rate_limited" {
			t.Fatalf("request %d: unexpected error: %v", i, err)
		}
	}
	if hits.Load() != 2 {
		t.Fatalf("unexpected provider traffic: %d hits", hits.Load())
	}

	client.mu.Lock()
	effective := client.retryAt
	client.mu.Unlock()
	if !effective.After(time.Now().Add(280 * time.Second)) {
		t.Fatalf("a shorter 429 write shortened the in-memory cooldown: only %v remaining", time.Until(effective))
	}
	var until int64
	if err := db.QueryRow(`SELECT cooldown_until FROM tmdb_provider_state WHERE id=1`).Scan(&until); err != nil {
		t.Fatal(err)
	}
	if until < time.Now().Add(280*time.Second).Unix() {
		t.Fatalf("a shorter 429 write shortened the persisted cooldown: %d", until)
	}
}
