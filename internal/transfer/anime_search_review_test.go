package transfer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
)

type animeSearchReviewHTTP struct {
	mu         sync.Mutex
	requests   int
	caps       int
	queries    []string
	categories []string
	deadlines  []time.Time
	observe    bool
}

type animeSearchReviewTransport struct {
	base    http.RoundTripper
	capture *animeSearchReviewHTTP
}

func (t animeSearchReviewTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.capture.mu.Lock()
	if t.capture.observe {
		deadline, _ := r.Context().Deadline()
		t.capture.deadlines = append(t.capture.deadlines, deadline)
	}
	t.capture.mu.Unlock()
	return t.base.RoundTrip(r)
}

func animeSearchTorznabReview(t *testing.T, mapped bool, mode string) (*workflowFixture, int64, *animeSearchReviewHTTP) {
	t.Helper()
	f, mediaID, targets := setupTV(t)
	ctx := context.Background()
	catalog := library.New(f.db)
	anime, err := catalog.Anime(ctx, mediaID)
	if err != nil {
		t.Fatal(err)
	}
	aliases := []library.AnimeAlias{}
	if mapped {
		for i, target := range targets {
			number := 5 + i
			aliases = append(aliases, library.AnimeAlias{EpisodeID: target.EpisodeID, AbsoluteNumber: &number})
		}
	}
	if _, err = catalog.SaveAnime(ctx, mediaID, library.AnimeUpdate{Category: "anime", Revision: anime.Revision, NumberingRevision: anime.NumberingRevision, Episodes: aliases}); err != nil {
		t.Fatal(err)
	}
	policy, err := catalog.AnimeSearch(ctx, mediaID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveAnimeSearch(ctx, mediaID, library.AnimeSearchUpdate{Revision: policy.Revision, CategoryRevision: policy.CategoryRevision, NumberingRevision: policy.NumberingRevision, Mode: mode, SearchTitle: "Selected & Alias", AlternateTitles: []string{"Selected & Alias", "Unused Alias"}}); err != nil {
		t.Fatal(err)
	}
	capture := &animeSearchReviewHTTP{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capture.mu.Lock()
		capture.requests++
		capture.mu.Unlock()
		if r.URL.Query().Get("apikey") != "review-private-key" {
			t.Error("real Torznab request omitted its configured fixture API key")
		}
		switch r.URL.Query().Get("t") {
		case "caps":
			capture.mu.Lock()
			capture.caps++
			capture.mu.Unlock()
			fmt.Fprint(w, `<caps><searching><search available="yes" supportedParams="q"/></searching></caps>`)
		case "search":
			capture.mu.Lock()
			capture.queries = append(capture.queries, r.URL.Query().Get("q"))
			capture.categories = append(capture.categories, r.URL.Query().Get("cat"))
			capture.mu.Unlock()
			if r.URL.Query().Get("extended") != "1" {
				t.Error("Anime Torznab search did not request normalized attributes")
			}
			// Every numeric query finds the same descriptor at the same enclosure;
			// the shared result must survive only once without exposing its URL.
			fmt.Fprintf(w, `<rss xmlns:torznab="http://torznab.com/schemas/2015/feed"><channel><item><title>Selected &amp; Alias - 005v2 [1080p][English][WEB]</title><guid>owned-shared-release</guid><enclosure url="http://%s/download?apikey=review-private-key" length="1024"/><torznab:attr name="seeders" value="5"/><torznab:attr name="magneturl" value="magnet:?xt=urn:btih:%s"/></item></channel></rss>`, r.Host, testHash)
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	adapter := NewTorznab()
	adapter.HTTP.Transport = animeSearchReviewTransport{base: adapter.HTTP.Transport, capture: capture}
	t.Cleanup(adapter.HTTP.CloseIdleConnections)
	f.s = New(f.db, f.s.secrets, f.cfg, f.client, adapter)
	f.s.importer.Prober = f.probe
	key := "review-private-key"
	if err = f.s.ConfigureIndexer(ctx, IndexerUpdate{URL: server.URL + "/api", APIKey: &key}); err != nil {
		t.Fatal(err)
	}
	capture.mu.Lock()
	capture.observe = true
	capture.mu.Unlock()
	return f, mediaID, capture
}

func TestReviewAnimeRealTorznabQueriesDeduplicateAndShareOneBudget(t *testing.T) {
	f, mediaID, capture := animeSearchTorznabReview(t, true, "absolute")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	results, err := f.s.SearchAnimeAutomatic(ctx, mediaID, 1, 1, false)
	if err != nil || len(results) != 1 {
		t.Fatal("equivalent numeric search responses were not deduplicated", results, err)
	}
	want := []string{"Selected & Alias 5", "Selected & Alias 05", "Selected & Alias 005"}
	capture.mu.Lock()
	queries := append([]string{}, capture.queries...)
	categories := append([]string{}, capture.categories...)
	deadlines := append([]time.Time{}, capture.deadlines...)
	capture.mu.Unlock()
	if !reflect.DeepEqual(queries, want) || !reflect.DeepEqual(categories, []string{"5000", "5000", "5000"}) {
		t.Fatal("queries did not preserve exact selected name/category or fanned out aliases", queries, categories)
	}
	if len(deadlines) != 3 || deadlines[0].IsZero() {
		t.Fatal("expected three bounded search requests with one cached caps read", deadlines)
	}
	capture.mu.Lock()
	capsCalls := capture.caps
	capture.mu.Unlock()
	if capsCalls != 1 {
		t.Fatalf("caps were fetched %d times; expected one cached read", capsCalls)
	}
	for _, deadline := range deadlines {
		if !deadline.Equal(deadlines[0]) {
			t.Fatal("numeric variants received separate timeout budgets", deadlines)
		}
	}
	if remaining := time.Until(deadlines[0]); remaining <= 0 || remaining > 10*time.Second {
		t.Fatal("Anime query batch did not use its shared ten-second budget", remaining)
	}
	public, err := json.Marshal(results)
	if err != nil || strings.Contains(string(public), "review-private-key") || strings.Contains(string(public), "magnet:") || strings.Contains(string(public), "/download?") {
		t.Fatal("normalized Anime release exposed private transport", string(public), err)
	}
}

func TestReviewAnimeDisabledUnmappedAndPackTargetsMakeNoTorznabRequests(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		mapped     bool
		episode    int
	}{{"disabled", "disabled", true, 1}, {"unmapped", "absolute", false, 1}, {"pack", "absolute", true, 0}} {
		t.Run(tc.name, func(t *testing.T) {
			f, mediaID, capture := animeSearchTorznabReview(t, tc.mapped, tc.mode)
			capture.mu.Lock()
			before := capture.requests
			capture.mu.Unlock()
			if _, err := f.s.SearchAnimeAutomatic(context.Background(), mediaID, 1, tc.episode, false); err == nil {
				t.Fatal("unreviewed/unsupported target was searched", tc.name)
			}
			capture.mu.Lock()
			after := capture.requests
			capture.mu.Unlock()
			if after != before {
				t.Fatal("unsupported automatic target contacted Torznab", before, after)
			}
		})
	}
}

func TestReviewAnimeSearchedPolicyCannotQueueAfterConsentChanges(t *testing.T) {
	f, mediaID, _ := animeSearchTorznabReview(t, true, "absolute")
	ctx := context.Background()
	releases, err := f.s.SearchAnimeAutomatic(ctx, mediaID, 1, 1, false)
	if err != nil || len(releases) != 1 {
		t.Fatal(releases, err)
	}
	catalog := library.New(f.db)
	prior, err := catalog.AnimeSearch(ctx, mediaID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveAnimeSearch(ctx, mediaID, library.AnimeSearchUpdate{Revision: prior.Revision, CategoryRevision: prior.CategoryRevision, NumberingRevision: prior.NumberingRevision, Mode: "disabled", SearchTitle: prior.SearchTitle, AlternateTitles: prior.AlternateTitles}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = f.s.Queue(ctx, releases[0].ID); err == nil {
		t.Fatal("release searched under revoked consent was queued")
	}
	var jobs, targets int
	if err = f.db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads),(SELECT COUNT(*) FROM episode_downloads)`).Scan(&jobs, &targets); err != nil || jobs != 0 || targets != 0 {
		t.Fatal("revoked searched policy left a partial reservation", jobs, targets, err)
	}
}

func TestReviewAnimeQueuedSearchSnapshotPersistsAfterPolicyChangesAndReopen(t *testing.T) {
	f, mediaID, _ := animeSearchTorznabReview(t, true, "absolute")
	ctx := context.Background()
	releases, err := f.s.SearchAnimeAutomatic(ctx, mediaID, 1, 1, false)
	if err != nil || len(releases) != 1 {
		t.Fatal(releases, err)
	}
	job, created, err := f.s.Queue(ctx, releases[0].ID)
	if err != nil || !created || job.AnimeSearch == nil || job.AnimeSearch.Mode != "absolute" || job.AnimeSearch.AbsoluteNumber == nil || *job.AnimeSearch.AbsoluteNumber != 5 || len(job.Targets) != 1 || job.Targets[0].AbsoluteNumber == nil || *job.Targets[0].AbsoluteNumber != 5 {
		t.Fatal("queue omitted exact searched policy/episode evidence", job, err)
	}
	before, err := json.Marshal(job.AnimeSearch)
	if err != nil {
		t.Fatal(err)
	}
	catalog := library.New(f.db)
	prior, err := catalog.AnimeSearch(ctx, mediaID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = catalog.SaveAnimeSearch(ctx, mediaID, library.AnimeSearchUpdate{Revision: prior.Revision, CategoryRevision: prior.CategoryRevision, NumberingRevision: prior.NumberingRevision, Mode: "disabled", SearchTitle: prior.SearchTitle, AlternateTitles: prior.AlternateTitles}); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	saved, err := f.s.Job(ctx, job.ID)
	if err != nil || saved.AnimeSearch == nil {
		t.Fatal("queued search snapshot lost after reopen", saved, err)
	}
	after, err := json.Marshal(saved.AnimeSearch)
	if err != nil || string(after) != string(before) || saved.Targets[0].AbsoluteNumber == nil || *saved.Targets[0].AbsoluteNumber != 5 {
		t.Fatal("later consent changes rewrote queued evidence", string(before), string(after), err)
	}
	if len(saved.AnimeSearch.Queries) != 3 || saved.AnimeSearch.SearchTitle != "Selected & Alias" {
		t.Fatal("queued query audit did not retain bounded variants", saved.AnimeSearch)
	}
	public, err := json.Marshal(saved)
	if err != nil || strings.Contains(string(public), "review-private-key") || strings.Contains(string(public), "magnet:") {
		t.Fatal("queued audit exposed private tracker transport", string(public), err)
	}
}
