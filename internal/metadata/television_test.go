package metadata

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTVMetadataContractAndValidation(t *testing.T) {
	bad := ""
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 30) {
			t.Error("missing authorization")
		}
		if r.URL.Query().Get("language") != "en-US" {
			t.Error("language missing")
		}
		if bad != "" {
			fmt.Fprint(w, bad)
			return
		}
		switch r.URL.Path {
		case "/search/tv":
			if r.URL.Query().Get("query") != "A & B" || r.URL.Query().Get("include_adult") != "false" || r.URL.Query().Get("page") != "2" {
				t.Error("incorrect search parameters")
			}
			fmt.Fprint(w, `{"page":2,"total_pages":800,"total_results":1000,"results":[{"id":42,"name":"A & B","first_air_date":"2025-01-02","poster_path":"/tv.jpg"}]}`)
		case "/tv/42":
			fmt.Fprint(w, `{"id":42,"name":"Canonical show","status":"Returning Series","seasons":[{"id":43,"season_number":0,"name":"Specials","episode_count":1},{"id":44,"season_number":1,"name":"Season 1","episode_count":2}]}`)
		case "/tv/42/season/1":
			fmt.Fprint(w, `{"id":44,"season_number":1,"name":"Season 1","episodes":[{"id":45,"episode_number":1,"season_number":1,"name":"Pilot","air_date":"2025-01-02","runtime":45},{"id":46,"episode_number":2,"season_number":1,"name":"","air_date":null,"runtime":0}]}`)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	c := newClient(server.URL, server.Client())
	ctx := context.Background()
	token := strings.Repeat("x", 30)
	out, e := c.SearchSeries(ctx, token, "A & B", 2)
	if e != nil || out.TotalPages != 500 || len(out.Items) != 1 || out.Items[0].Title != "A & B" || out.Items[0].Year == nil || *out.Items[0].Year != 2025 || out.Items[0].PosterURL == "" {
		t.Fatal(out, e)
	}
	show, e := c.Series(ctx, token, 42)
	if e != nil || len(show.Seasons) != 2 || show.Seasons[0].Number != 0 {
		t.Fatal(show, e)
	}
	season, e := c.Season(ctx, token, 42, 1)
	if e != nil || len(season.Episodes) != 2 || season.Episodes[1].Title != "Episode 2" || season.Episodes[1].AirDate != "" || season.Episodes[1].RuntimeMinutes != nil {
		t.Fatal(season, e)
	}
	for _, body := range []string{`{"id":44,"season_number":2}`, `{"id":44,"season_number":1,"episodes":[{"id":1,"episode_number":1,"season_number":1,"name":"a"},{"id":2,"episode_number":1,"season_number":1,"name":"b"}]}`, `{"id":44,"season_number":1,"episodes":[{"id":1,"episode_number":1,"season_number":0,"name":"a"}]}`} {
		bad = body
		if _, e = c.Season(ctx, token, 42, 1); e == nil {
			t.Fatal("invalid season accepted", body)
		}
	}
	bad = `{"id":99,"name":"Other show"}`
	if _, e = c.Series(ctx, token, 42); e == nil {
		t.Fatal("different series accepted")
	}
	bad = ""
	if _, e = c.Series(ctx, token, 999); e == nil || !strings.Contains(e.Error(), "show or season") {
		t.Fatal("TV-specific not-found missing", e)
	}
	if calls < 8 {
		t.Fatal("contract not exercised")
	}
}
