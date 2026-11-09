package music

import (
	"context"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type cachedTestEntry struct {
	payload  []byte
	negative bool
	ttl      time.Duration
}

type testCache struct {
	entries  map[string]cachedTestEntry
	gets     int
	puts     int
	cleaned  int
	getErr   error
	putErr   error
	purgeErr error
}

func newTestCache() *testCache {
	return &testCache{entries: map[string]cachedTestEntry{}}
}

func (c *testCache) Get(_ context.Context, key string) ([]byte, bool, bool, error) {
	c.gets++
	if c.getErr != nil {
		return nil, false, false, c.getErr
	}
	entry, ok := c.entries[key]
	if !ok {
		return nil, false, false, nil
	}
	return entry.payload, entry.negative, true, nil
}

func (c *testCache) Put(_ context.Context, key string, payload []byte, ttl time.Duration, negative bool) error {
	c.puts++
	if c.putErr != nil {
		return c.putErr
	}
	c.entries[key] = cachedTestEntry{payload: append([]byte(nil), payload...), negative: negative, ttl: ttl}
	return nil
}

func (c *testCache) Cleanup(context.Context) error {
	c.cleaned++
	return c.purgeErr
}

func (c *testCache) Count(context.Context) (int, error) { return len(c.entries), nil }

func TestMusicBrainzCacheHitsMissesAndTTLs(t *testing.T) {
	body := `{"id":"` + testMBID + `","name":"Radiohead","sort-name":"Radiohead"}`
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch {
		case r.URL.Path == "/ws/2/artist/"+testMBID:
			io.WriteString(w, body)
		case r.URL.Path == "/ws/2/artist":
			io.WriteString(w, `{"count":1,"offset":0,"artists":[]}`)
		case r.URL.Path == "/ws/2/release-group/"+testRGID:
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cache := newTestCache()
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	client.SetCache(cache)
	ctx := context.Background()

	artist, err := client.Artist(ctx, testMBID)
	if err != nil || artist.Name != "Radiohead" || requests.Load() != 1 {
		t.Fatal("first artist request", artist, err, requests.Load())
	}
	artist, err = client.Artist(ctx, testMBID)
	if err != nil || artist.Name != "Radiohead" || requests.Load() != 1 {
		t.Fatal("cached artist request contacted provider", artist, err, requests.Load())
	}
	if cache.puts != 1 {
		t.Fatal("metadata cache writes", cache.puts)
	}
	for key, entry := range cache.entries {
		if !strings.Contains(key, server.URL+"/ws/2/artist/"+testMBID) || !strings.Contains(key, "fmt=json") {
			t.Fatalf("cache key %q", key)
		}
		if string(entry.payload) != body || entry.negative || entry.ttl != musicbrainzMetadataTTL {
			t.Fatalf("metadata entry %+v", entry)
		}
	}

	if _, err := client.SearchArtists(ctx, "radiohead", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SearchArtists(ctx, "radiohead", 1); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatal("search was not cached", requests.Load())
	}
	searchTTL := false
	for _, entry := range cache.entries {
		if entry.ttl == musicbrainzSearchTTL {
			searchTTL = true
		}
	}
	if !searchTTL {
		t.Fatal("search TTL", cache.entries)
	}

	var provider *Error
	if _, _, err := client.ReleaseGroup(ctx, testRGID); !errors.As(err, &provider) || provider.Code != "music_not_found" {
		t.Fatal("404", err)
	}
	provider = nil
	if _, _, err := client.ReleaseGroup(ctx, testRGID); !errors.As(err, &provider) || provider.Code != "music_not_found" {
		t.Fatal("cached 404", err)
	}
	if requests.Load() != 3 {
		t.Fatal("negative 404 was not cached", requests.Load())
	}
	negativeTTL := false
	for _, entry := range cache.entries {
		if entry.negative && entry.ttl == musicbrainzNegativeTTL && len(entry.payload) == 0 {
			negativeTTL = true
		}
	}
	if !negativeTTL {
		t.Fatal("negative TTL", cache.entries)
	}
}

func TestMusicBrainzCacheExpiryCleanupAndEmptyResults(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := &musicbrainzStore{db: db}

	// An expired row is a miss and the provider result is cached again.
	body := `{"id":"` + testMBID + `","name":"Radiohead"}`
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path == "/ws/2/artist/"+testMBID {
			io.WriteString(w, body)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	client.SetCache(store)
	key := server.URL + "/ws/2/artist/" + testMBID + "?fmt=json"
	if err := store.Put(ctx, key, []byte(body), time.Hour, false); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE musicbrainz_cache SET expires_at=unixepoch()-1`); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Artist(ctx, testMBID); err != nil || requests.Load() != 1 {
		t.Fatal("expired cache suppressed provider", err, requests.Load())
	}

	// Empty list metadata is a one-day negative observation, while a real
	// empty release list still returns to the caller.
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"release-group-count":0,"release-group-offset":0,"release-groups":[]}`)
	}))
	defer empty.Close()
	emptyClient := newClient(empty.URL+"/ws/2", empty.Client(), 0)
	emptyClient.SetCache(store)
	albums, err := emptyClient.ArtistAlbums(ctx, testMBID)
	if err != nil || len(albums) != 0 {
		t.Fatal("empty discography", albums, err)
	}
	var negative int
	if err := db.QueryRow(`SELECT negative FROM musicbrainz_cache WHERE key LIKE ?`, empty.URL+"%").Scan(&negative); err != nil || negative != 1 {
		t.Fatal("empty metadata was not negative-cached", negative, err)
	}

	// Cleanup is bounded and removes only expired rows.
	for i := 0; i < musicbrainzCleanupLimit+3; i++ {
		if _, err := db.Exec(`INSERT INTO musicbrainz_cache(key,payload,expires_at,negative) VALUES(?, '{}', unixepoch()-1, 0)`, "expired-"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := db.QueryRow(`SELECT COUNT(*) FROM musicbrainz_cache WHERE expires_at<=unixepoch()`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining == 0 {
		t.Fatal("cleanup did not remove expired rows")
	}
	if remaining >= musicbrainzCleanupLimit+3 {
		t.Fatal("cleanup was not bounded", remaining)
	}
}

func TestMusicBrainzCacheIgnoresErrorsAndInvalidBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ws/2/artist/" + testMBID:
			io.WriteString(w, `{not json`)
		case "/ws/2/artist/" + testRecordingID:
			io.WriteString(w, `{}`)
		case "/ws/2/release-group/" + testRGID:
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	var provider *Error

	// A successful but unreadable body is not cached.
	cache := newTestCache()
	client.SetCache(cache)
	if err := client.request(context.Background(), "/artist/"+testMBID, nil, &mbArtist{}, 2<<20); !errors.As(err, &provider) || provider.Code != "music_invalid_response" {
		t.Fatal("invalid body", err)
	}
	if cache.puts != 0 {
		t.Fatal("invalid body cached", cache.puts)
	}
	if _, err := client.Artist(context.Background(), testRecordingID); !errors.As(err, &provider) || provider.Code != "music_invalid_response" {
		t.Fatal("empty object", err)
	}
	if cache.puts != 0 {
		t.Fatal("empty object cached", cache.puts)
	}
	// Provider errors other than an explicit 404 are not cached either.
	if _, _, err := client.ReleaseGroup(context.Background(), testRGID); !errors.As(err, &provider) || provider.Code != "music_unavailable" {
		t.Fatal("provider error", err)
	}
	if cache.puts != 0 {
		t.Fatal("provider error cached", cache.puts)
	}
}

func TestMusicBrainzCacheFailuresFailOpen(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		io.WriteString(w, `{"id":"`+testMBID+`","name":"Radiohead"}`)
	}))
	defer server.Close()
	cache := newTestCache()
	cache.getErr = errors.New("cache read unavailable")
	cache.putErr = errors.New("cache write unavailable")
	cache.purgeErr = errors.New("cache cleanup unavailable")
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	client.SetCache(cache)
	if _, err := client.Artist(context.Background(), testMBID); err != nil || requests.Load() != 1 {
		t.Fatal("cache failure blocked matching", err, requests.Load())
	}
}

func TestMusicBrainzCooldownPersistsAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	store := &musicbrainzStore{db: db}
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	client.SetCooldownStore(store)
	var provider *Error
	if _, err := client.Artist(ctx, testMBID); !errors.As(err, &provider) || provider.Code != "music_rate_limited" || provider.RetryAfter != 7 {
		t.Fatal("429", err)
	}
	until, message, err := store.Cooldown(ctx)
	if err != nil || time.Until(until) < 5*time.Second || !strings.Contains(message, "paused until") {
		t.Fatal("persisted cooldown", until, message, err)
	}
	if _, err := client.Artist(ctx, testMBID); !errors.As(err, &provider) || provider.Code != "music_rate_limited" || requests.Load() != 1 {
		t.Fatal("cooldown did not prevent a second provider call", err, requests.Load())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	reopenedStore := &musicbrainzStore{db: reopened}
	until2, _, err := reopenedStore.Cooldown(ctx)
	if err != nil || until2.Unix() != until.Unix() {
		t.Fatal("cooldown did not survive reopen", until, until2, err)
	}
	reopenedClient := newClient(server.URL+"/ws/2", server.Client(), 0)
	reopenedClient.SetCooldownStore(reopenedStore)
	provider = nil
	if _, err := reopenedClient.Artist(ctx, testMBID); !errors.As(err, &provider) || provider.Code != "music_rate_limited" || requests.Load() != 1 {
		t.Fatal("reopened cooldown", err, requests.Load())
	}
}

func TestMusicBrainzServiceUnavailableAlsoStartsCooldown(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Retry-After", time.Now().Add(2*time.Minute).UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	store := &musicbrainzStore{db: db}
	client := newClient(server.URL+"/ws/2", server.Client(), 0)
	client.SetCooldownStore(store)
	var provider *Error
	if _, err := client.Artist(ctx, testMBID); !errors.As(err, &provider) || provider.Code != "music_rate_limited" || provider.RetryAfter < 100 {
		t.Fatal("503", err)
	}
	until, message, err := store.Cooldown(ctx)
	if err != nil || time.Until(until) < 100*time.Second || !strings.Contains(message, "paused until") {
		t.Fatal("503 cooldown", until, message, err)
	}
	if _, err := client.Artist(ctx, testMBID); !errors.As(err, &provider) || provider.Code != "music_rate_limited" || requests.Load() != 1 {
		t.Fatal("503 retried", err, requests.Load())
	}
}

func TestParseRetryAfterSecondsDatesAndBounds(t *testing.T) {
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	if got := parseRetryAfter("12", now); got != 12*time.Second {
		t.Fatal("seconds", got)
	}
	if got := parseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now); got != 90*time.Second {
		t.Fatal("http date", got)
	}
	if got := parseRetryAfter("", now); got != 5*time.Second {
		t.Fatal("default", got)
	}
	if got := parseRetryAfter("nonsense", now); got != 5*time.Second {
		t.Fatal("invalid", got)
	}
	if got := parseRetryAfter(now.Add(-time.Hour).Format(http.TimeFormat), now); got != time.Second {
		t.Fatal("past date", got)
	}
	if got := parseRetryAfter("999999", now); got != 24*time.Hour {
		t.Fatal("bounded", got)
	}
}

func TestMusicBrainzPublicAndLocalPacing(t *testing.T) {
	client := New()
	if client.interval != PublicMusicBrainzInterval || !strings.HasPrefix(client.BaseURL(), "https://musicbrainz.org") {
		t.Fatal("public defaults", client.interval, client.BaseURL())
	}
	client.SetBase("http://192.168.1.20:5000")
	if client.interval != LocalMusicBrainzInterval || client.BaseURL() != "http://192.168.1.20:5000" {
		t.Fatal("local pacing", client.interval, client.BaseURL())
	}
	client.SetBase(DefaultMusicBrainzBaseURL)
	if client.interval != PublicMusicBrainzInterval {
		t.Fatal("restored public pacing", client.interval)
	}
	for base, want := range map[string]bool{
		"http://localhost:5000/ws/2":   true,
		"http://127.0.0.1:5000/ws/2":   true,
		"http://10.1.2.3/ws/2":         true,
		"http://172.16.5.4/ws/2":       true,
		"http://192.168.0.9/ws/2":      true,
		"http://[::1]:5000/ws/2":       true,
		"http://[fd00::1]/ws/2":        true,
		"https://musicbrainz.org/ws/2": false,
		"https://mb.example/ws/2":      false,
		"http://mb.example/ws/2":       false,
	} {
		if got := MusicBrainzBaseIsLocal(base); got != want {
			t.Fatalf("local detection %s = %v, want %v", base, got, want)
		}
	}
}

func TestNormalizeMusicBrainzBaseURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://musicbrainz.org", "https://musicbrainz.org/ws/2", false},
		{" https://musicbrainz.org/ws/2/ ", "https://musicbrainz.org/ws/2", false},
		{"https://mb.example/prefix", "https://mb.example/prefix/ws/2", false},
		{"https://mb.example/prefix/ws/2", "https://mb.example/prefix/ws/2", false},
		{"http://localhost:5000", "http://localhost:5000/ws/2", false},
		{"http://127.0.0.1:5000/mirror/", "http://127.0.0.1:5000/mirror/ws/2", false},
		{"http://192.168.1.20:5000", "http://192.168.1.20:5000/ws/2", false},
		{"http://[fd00::1]:5000", "http://[fd00::1]:5000/ws/2", false},
		{"http://musicbrainz.org", "", true},
		{"http://mb.example", "", true},
		{"ftp://mb.example", "", true},
		{"https://user:pass@mb.example", "", true},
		{"https://mb.example?foo=bar", "", true},
		{"https://mb.example#fragment", "", true},
		{"https://mb.example/a/../b", "", true},
		{"https://mb.example/%2e%2e/b", "", true},
		{"https://mb.example/a%5cb", "", true},
		{"", "", true},
	}
	for _, tc := range cases {
		got, err := NormalizeMusicBrainzBaseURL(tc.in)
		if (err != nil) != tc.wantErr || (!tc.wantErr && got != tc.want) {
			t.Fatalf("normalize %q = %q, %v; want %q err=%v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestMusicBrainzStatusReportsSavedBaseAndCooldown(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	secrets, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(db, t.TempDir())
	service.SetSecrets(secrets)
	status, err := service.MusicBrainzStatus(ctx)
	if err != nil || status.BaseURL != DefaultMusicBrainzBaseURL || !status.UsingDefault || status.CacheEntries != 0 {
		t.Fatal("default status", status, err)
	}
	status, err = service.SaveMusicBrainzBaseURL(ctx, "http://192.168.1.5:5000/")
	if err != nil || status.BaseURL != "http://192.168.1.5:5000/ws/2" || status.UsingDefault {
		t.Fatal("saved status", status, err)
	}
	if service.client.BaseURL() != status.BaseURL || service.client.interval != LocalMusicBrainzInterval {
		t.Fatal("saved base was not applied", service.client.BaseURL(), service.client.interval)
	}
	restored := NewService(db, t.TempDir())
	restored.SetSecrets(secrets)
	if err := restored.LoadMusicBrainzBase(ctx); err != nil || restored.client.BaseURL() != status.BaseURL {
		t.Fatal("startup base load", restored.client.BaseURL(), err)
	}
	store := &musicbrainzStore{db: db}
	if err := store.SetCooldown(ctx, time.Now().Add(10*time.Minute), "MusicBrainz is limiting requests. Matching is paused until later."); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO musicbrainz_cache(key,payload,expires_at,negative) VALUES('live','{}',unixepoch()+3600,0),('expired','{}',unixepoch()-1,0)`); err != nil {
		t.Fatal(err)
	}
	status, err = service.MusicBrainzStatus(ctx)
	if err != nil || status.CooldownUntil == 0 || !strings.Contains(status.Message, "paused") || status.CacheEntries != 1 {
		t.Fatal("cooldown/cache status", status, err)
	}
	status, err = service.DeleteMusicBrainzBaseURL(ctx)
	if err != nil || status.BaseURL != DefaultMusicBrainzBaseURL || !status.UsingDefault {
		t.Fatal("reset status", status, err)
	}
	if service.client.BaseURL() != DefaultMusicBrainzBaseURL || service.client.interval != PublicMusicBrainzInterval {
		t.Fatal("reset base was not applied", service.client.BaseURL(), service.client.interval)
	}
}

func TestMusicBrainzStoreRejectsUnavailableSecrets(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	var provider *Error
	if _, err := service.SaveMusicBrainzBaseURL(ctx, "https://mb.example"); !errors.As(err, &provider) || provider.Code != "music_musicbrainz_unavailable" {
		t.Fatal("save without secure store", err)
	}
	provider = nil
	if _, err := service.DeleteMusicBrainzBaseURL(ctx); !errors.As(err, &provider) || provider.Code != "music_musicbrainz_unavailable" {
		t.Fatal("delete without secure store", err)
	}
}
