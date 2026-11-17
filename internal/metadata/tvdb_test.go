package metadata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

type tvdbTestTransport func(*http.Request) (*http.Response, error)

func (f tvdbTestTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestTVDBFallbackSearchAndDefaultEpisodeOrder(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	secret, err := securestore.New(ctx, db, dir)
	if err != nil {
		t.Fatal(err)
	}
	s := New(secret, newClient("https://tmdb.invalid", &http.Client{Transport: tvdbTestTransport(func(req *http.Request) (*http.Response, error) {
		body := `{"page":1,"total_pages":1,"total_results":0,"results":[]}`
		if req.URL.Query().Get("query") == "Exact Primary" {
			body = `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":42,"name":"Exact Primary"}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}))
	s.SetDefaultToken(strings.Repeat("a", 40))
	var logins, fallbackSearches int
	s.tvdb.http.Transport = tvdbTestTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "api4.thetvdb.com" {
			t.Fatalf("leaked request to %s", req.URL.Host)
		}
		var body string
		if req.URL.Path == "/v4/login" {
			logins++
			var in map[string]string
			if err := json.NewDecoder(req.Body).Decode(&in); err != nil || in["apikey"] != "fixture-key" || in["pin"] != "fixture-pin" {
				t.Fatalf("login payload was invalid: %v", err)
			}
			body = `{"data":{"token":"fixture-bearer"}}`
		} else {
			if req.Header.Get("Authorization") != "Bearer fixture-bearer" {
				t.Fatal("missing bearer authorization")
			}
			switch req.URL.Path {
			case "/v4/search":
				fallbackSearches++
				if req.URL.Query().Get("type") != "series" {
					t.Fatal("type filter missing")
				}
				body = `{"data":[{"tvdb_id":"42","type":"series","name":"XG DAYS","primary_language":"jpn"}]}`
			case "/v4/series/42/extended":
				body = `{"data":{"id":42,"name":"XG DAYS","firstAired":"2024-01-09","originalLanguage":"jpn","defaultSeasonType":1,"status":{"name":"Continuing"},"seasons":[{"id":8,"number":1,"type":{"id":1}},{"id":9,"number":1,"type":{"id":2}}]}}`
			case "/v4/series/42/episodes/default":
				if req.URL.Query().Get("season") != "1" {
					t.Fatal("wrong season")
				}
				body = `{"data":{"episodes":[{"id":55,"seriesId":42,"seasonNumber":1,"number":1,"name":"Our days","aired":"2024-01-09"}]}}`
			default:
				t.Fatalf("unexpected request %s", req.URL.Path)
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	if err = s.ConfigureTVDB(ctx, TVDBCredentials{Key: "fixture-key", PIN: "fixture-pin"}); err != nil {
		t.Fatal(err)
	}
	primary, err := s.SearchSeries(ctx, "Exact Primary", 1)
	if err != nil || len(primary.Items) != 1 || primary.Items[0].TMDbID != 42 || fallbackSearches != 0 {
		t.Fatalf("primary: %+v %v", primary, err)
	}
	for _, query := range []string{"XG DAYS", "https://www.thetvdb.com/series/xg-days"} {
		fallback, err := s.SearchSeries(ctx, query, 1)
		if err != nil || len(fallback.Items) != 1 || fallback.Items[0].TVDbID != 42 || fallback.Items[0].TMDbID != -42 {
			t.Fatalf("fallback: %+v %v", fallback, err)
		}
	}
	show, err := s.Series(ctx, -42)
	if err != nil || show.OriginalLanguage != "ja" || len(show.Seasons) != 1 || show.Seasons[0].TMDbID != -8 {
		t.Fatalf("series: %+v %v", show, err)
	}
	season, err := s.Season(ctx, -42, 1)
	if err != nil || len(season.Episodes) != 1 || season.Episodes[0].TMDbID != -55 || season.Episodes[0].Title != "Our days" {
		t.Fatalf("season: %+v %v", season, err)
	}
	if logins != 1 {
		t.Fatalf("logged in %d times", logins)
	}
}
