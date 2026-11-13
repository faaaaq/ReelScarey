package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"
)

// A full-library refresh enqueues the whole library at once and the refresher
// processes batches of up to 25 titles per tick, so the TMDb client must
// proactively space outgoing requests instead of bursting the provider
// (RS-0017). The reactive 429 Retry-After cooldown alone cannot prevent the
// initial burst.
func TestTMDbRequestsArePacedProactively(t *testing.T) {
	var mu sync.Mutex
	var stamps []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		stamps = append(stamps, time.Now())
		mu.Unlock()
		_, _ = w.Write([]byte(`{"id":11,"title":"Paced","release_date":"2020-01-02"}`))
	}))
	defer server.Close()

	client := newClient(server.URL, server.Client())
	const interval = 150 * time.Millisecond
	client.interval = interval
	const burst = 6
	var wg sync.WaitGroup
	results := make([]error, burst)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = client.Movie(context.Background(), token, 11)
		}()
	}
	wg.Wait()
	for i, err := range results {
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(stamps) != burst {
		t.Fatalf("unexpected provider traffic: %d requests", len(stamps))
	}
	sort.Slice(stamps, func(i, j int) bool { return stamps[i].Before(stamps[j]) })
	// Tolerant margin: the limiter guarantees >= interval between scheduled
	// slots, but wall-clock measurement of server arrival can only lose
	// resolution, so assert a generous 100 ms floor for a 150 ms interval.
	for i := 1; i < len(stamps); i++ {
		if gap := stamps[i].Sub(stamps[i-1]); gap < 100*time.Millisecond {
			t.Fatalf("burst was not paced: consecutive requests %v apart, below the configured %v minimum", gap, interval)
		}
	}
}

// The default pacing interval must stay conservative relative to the audited
// ~1.7 titles/s nominal refresh budget; TV titles cost multiple calls.
func TestTMDbDefaultPacingIntervalIsConservative(t *testing.T) {
	if tmdbMinInterval != 600*time.Millisecond {
		t.Fatalf("tmdbMinInterval changed unexpectedly: %v", tmdbMinInterval)
	}
}
