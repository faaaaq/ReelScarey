package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/library"
)

func TestReviewAnimeSearchAPIRequiresExplicitAuthenticatedPerShowConsent(t *testing.T) {
	handler, db, accounts, code := rawFixture(t, nil)
	ctx := context.Background()
	session, err := accounts.Setup(ctx, "reviewadmin", "test-only-review-password", code)
	if err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,category,monitored) VALUES(1,'series',1,'Canonical Anime','anime',1),(2,'movie',2,'Anime film','anime',1),(3,'series',3,'Standard TV','standard',1)`,
		`INSERT INTO series_details(media_id) VALUES(1),(3)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(1,1,11,1,'Season','','2020-01-01',1,'loaded')`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(1,1,12,1,'Episode','','2020-01-01')`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	route := "/api/v1/library/items/1/anime-search"
	valid := `{"revision":0,"category_revision":1,"numbering_revision":1,"mode":"season","search_title":"Explicit alias","alternate_titles":["Explicit alias"]}`
	for _, verb := range []string{"GET", "PUT"} {
		if out := request(handler, verb, route, valid, nil, false); out.Code != 401 {
			t.Fatal("unprotected Anime-search endpoint", verb, out.Code, out.Body)
		}
	}
	if out := request(handler, "PUT", route, valid, &session, false); out.Code != 403 {
		t.Fatal("Anime-search consent accepted without CSRF", out.Code, out.Body)
	}
	r := httptest.NewRequest("PUT", route, strings.NewReader(valid))
	r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://foreign.invalid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatal("cross-origin search consent accepted", rec.Code, rec.Body)
	}
	out := request(handler, "GET", route, "", &session, false)
	var prior library.AnimeSearchSettings
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &prior) != nil || prior.Mode != "disabled" || prior.Revision != 0 {
		t.Fatal("new show is not disabled by default", out.Code, out.Body)
	}
	for _, tc := range []struct {
		name, body, contentType string
		want                    int
	}{
		{"missing MIME", valid, "", 415},
		{"text MIME", valid, "text/plain", 415},
		{"null", "null", "application/json", 400},
		{"unknown field", strings.TrimSuffix(valid, "}") + `,"all_anime":true}`, "application/json", 400},
		{"trailing object", valid + `{}`, "application/json", 400},
		{"unknown mode", strings.Replace(valid, `"season"`, `"automatic"`, 1), "application/json", 400},
		{"unreviewed search title", strings.Replace(valid, `"search_title":"Explicit alias"`, `"search_title":"Another title"`, 1), "application/json", 400},
		{"oversized", strings.Repeat(" ", 17<<10) + valid, "application/json", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("PUT", route, strings.NewReader(tc.body))
			r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
			r.Header.Set("Content-Type", tc.contentType)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != tc.want {
				t.Fatal("incorrect input boundary", rec.Code, tc.want, rec.Body)
			}
		})
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM anime_search_settings`).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid consent wrote saved search settings", count, err)
	}
	out = request(handler, "PUT", route, valid, &session, true)
	var saved library.AnimeSearchSettings
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &saved) != nil || saved.Mode != "season" || saved.Revision != 1 || saved.SearchTitle != "Explicit alias" {
		t.Fatal("explicit per-show consent not saved", out.Code, out.Body)
	}
	if out = request(handler, "PUT", route, valid, &session, true); out.Code != 409 {
		t.Fatal("old consent review overwrote newer policy", out.Code, out.Body)
	}
	if out = request(handler, "DELETE", route, "", &session, true); out.Code != 405 || out.Header().Get("Allow") != "GET, PUT" {
		t.Fatal("unexpected search-settings method", out.Code, out.Body)
	}
	if out = request(handler, "GET", "/api/v1/library/items/999/anime-search", "", &session, false); out.Code != 404 {
		t.Fatal("missing show not reported", out.Code, out.Body)
	}
	for _, tc := range []struct {
		id, numbering string
		want          int
	}{{"2", "0", 400}, {"3", "1", 409}} {
		body := strings.Replace(valid, `"numbering_revision":1`, `"numbering_revision":`+tc.numbering, 1)
		if out = request(handler, "PUT", "/api/v1/library/items/"+tc.id+"/anime-search", body, &session, true); out.Code != tc.want {
			t.Fatal("automatic Anime search accepted outside Anime series", tc.id, out.Code, out.Body)
		}
	}
	var downloads, runs int
	if err = db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads),(SELECT COUNT(*) FROM automation_runs)`).Scan(&downloads, &runs); err != nil || downloads != 0 || runs != 0 {
		t.Fatal("saving consent started a search or download", downloads, runs, err)
	}
}
