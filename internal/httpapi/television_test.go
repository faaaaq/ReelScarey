package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"strings"
	"testing"
)

type tvProvider struct {
	testProvider
	seriesCalls int
}

func (p *tvProvider) SearchSeries(context.Context, string, string, int) (metadata.SeriesResults, error) {
	return metadata.SeriesResults{Page: 1, TotalPages: 1, TotalResults: 1, Items: []metadata.Series{tvFixture()}}, nil
}
func tvFixture() metadata.Series {
	return metadata.Series{Movie: metadata.Movie{TMDbID: 42, Title: "Canonical TV title", Overview: "Provider TV synopsis"}, SeriesStatus: "Ended", Seasons: []metadata.SeasonSummary{{TMDbID: 44, Number: 1, Title: "Season 1", EpisodeCount: 1}}}
}
func (p *tvProvider) Series(context.Context, string, int64) (metadata.Series, error) {
	p.seriesCalls++
	if p.fail {
		return metadata.Series{}, &metadata.Error{Code: "tmdb_unavailable", Message: "Provider offline."}
	}
	return tvFixture(), nil
}
func (p *tvProvider) Season(context.Context, string, int64, int) (metadata.Season, error) {
	return metadata.Season{SeasonSummary: tvFixture().Seasons[0], Episodes: []metadata.Episode{{TMDbID: 45, Number: 1, Title: "Pilot"}}}, nil
}
func TestTVProtectedCanonicalCatalogAndMonitoring(t *testing.T) {
	p := &tvProvider{}
	handler, _, auth, code := rawFixture(t, p)
	ctx := context.Background()
	session, e := auth.Setup(ctx, "testadmin", "test-only-password", code)
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range []string{"/api/v1/metadata/series?query=test", "/api/v1/metadata/series/42", "/api/v1/library/series/1", "/api/v1/library/series/1/seasons/1"} {
		if out := request(handler, "GET", path, "", nil, false); out.Code != 401 {
			t.Fatal("unprotected TV read", path, out.Code)
		}
	}
	for _, v := range []struct{ method, path, body string }{{"POST", "/api/v1/library/series", `{"tmdb_id":42}`}, {"POST", "/api/v1/library/series/1/refresh", `{"revision":1}`}, {"POST", "/api/v1/library/series/1/seasons/1/refresh", `{"revision":1}`}, {"PUT", "/api/v1/library/series/1/monitor", `{"monitored":false,"revision":1}`}, {"PUT", "/api/v1/library/series/1/seasons/1/monitor", `{"monitored":false,"revision":1}`}, {"PUT", "/api/v1/library/series/1/seasons/monitor", `{"monitored":false,"revision":1,"season_ids":[1]}`}, {"PUT", "/api/v1/library/series/1/seasons/1/episodes/1/monitor", `{"monitored":false,"revision":1}`}, {"PUT", "/api/v1/library/series/1/seasons/1/episodes/monitor", `{"monitored":false,"revision":1,"episode_ids":[1]}`}} {
		if out := request(handler, v.method, v.path, v.body, &session, false); out.Code != 403 {
			t.Fatal("TV CSRF missing", v.path, out.Code)
		}
	}
	if out := request(handler, "PUT", "/api/v1/providers/tmdb", `{"token":"test-only-valid-provider-token"}`, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	if out := request(handler, "POST", "/api/v1/library/series", `{"tmdb_id":42,"title":"Spoofed"}`, &session, true); out.Code != 400 {
		t.Fatal("spoofed metadata accepted")
	}
	out := request(handler, "POST", "/api/v1/library/series", `{"tmdb_id":42,"monitored":false}`, &session, true)
	if out.Code != 201 || !strings.Contains(out.Body.String(), "Canonical TV title") {
		t.Fatal(out.Code, out.Body)
	}
	var added struct {
		Item library.Item `json:"item"`
	}
	if e = json.Unmarshal(out.Body.Bytes(), &added); e != nil {
		t.Fatal(e)
	}
	base := fmt.Sprintf("/api/v1/library/series/%d", added.Item.ID)
	p.fail = true
	out = request(handler, "POST", "/api/v1/library/series", `{"tmdb_id":42,"monitored":true}`, &session, true)
	if out.Code != 200 || p.seriesCalls != 1 || !strings.Contains(out.Body.String(), `"monitored":false`) {
		t.Fatal("duplicate changed metadata or choices", out.Code, out.Body)
	}
	out = request(handler, "GET", "/api/v1/metadata/series?query=test", "", &session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"in_library":true`) {
		t.Fatal(out.Code, out.Body)
	}
	out = request(handler, "POST", base+"/refresh", `{"revision":1}`, &session, true)
	if out.Code != 502 {
		t.Fatal("offline refresh not reported", out.Code)
	}
	out = request(handler, "GET", base, "", &session, false)
	var show library.TVSeries
	if e = json.Unmarshal(out.Body.Bytes(), &show); e != nil || show.Revision != 1 || len(show.Seasons) != 1 {
		t.Fatal(out.Body, e)
	}
	out = request(handler, "POST", base+"/seasons/1/refresh", `{"revision":1}`, &session, true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	out = request(handler, "PUT", base+"/seasons/1/episodes/1/monitor", `{"revision":2,"monitored":false}`, &session, true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	out = request(handler, "PUT", base+"/monitor", `{"revision":2,"monitored":true}`, &session, true)
	if out.Code != 409 {
		t.Fatal("stale write accepted", out.Code)
	}
	if out = request(handler, "DELETE", "/api/v1/providers/tmdb", "", &session, true); out.Code != 200 {
		t.Fatal(out.Code)
	}
	out = request(handler, "GET", base+"/seasons/1", "", &session, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "Pilot") || !strings.Contains(out.Body.String(), `"monitored":false`) {
		t.Fatal("cached episodes unavailable", out.Code, out.Body)
	}
	var saved library.TVSeasonDetails
	if e = json.Unmarshal(out.Body.Bytes(), &saved); e != nil || len(saved.Episodes) != 1 {
		t.Fatal(out.Body, e)
	}
	batch := fmt.Sprintf(`{"revision":%d,"monitored":true,"episode_ids":[%d]}`, saved.Revision, saved.Episodes[0].ID)
	out = request(handler, "PUT", base+"/seasons/1/episodes/monitor", batch, &session, true)
	if out.Code != 200 {
		t.Fatal("batch monitoring failed", out.Code, out.Body)
	}
	out = request(handler, "PUT", base+"/seasons/1/episodes/monitor", batch, &session, true)
	if out.Code != 409 {
		t.Fatal("stale batch accepted", out.Code, out.Body)
	}
	seasonBatch := fmt.Sprintf(`{"revision":%d,"monitored":false,"season_ids":[%d]}`, saved.Revision+1, saved.Season.ID)
	out = request(handler, "PUT", base+"/seasons/monitor", seasonBatch, &session, true)
	if out.Code != 200 {
		t.Fatal("season batch failed", out.Code, out.Body)
	}
	out = request(handler, "PUT", base+"/seasons/monitor", seasonBatch, &session, true)
	if out.Code != 409 {
		t.Fatal("stale season batch accepted", out.Code, out.Body)
	}
	out = request(handler, "GET", "/series/"+fmt.Sprint(added.Item.ID), "", nil, false)
	if out.Code != 200 || !strings.Contains(out.Body.String(), "<!doctype html>") {
		t.Fatal("TV deep link broken", out.Code)
	}
}
