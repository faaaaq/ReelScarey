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

func TestReviewAnimeAPISecurityAndStrictLongShowPayloads(t *testing.T) {
	handler, db, accounts, code := rawFixture(t, nil)
	session, err := accounts.Setup(context.Background(), "reviewadmin", "test-only-review-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,monitored) VALUES(1,'movie',1,'Reviewed film',0)`); err != nil {
		t.Fatal(err)
	}
	route := "/api/v1/library/items/1/anime"
	valid := `{"category":"anime","revision":1,"numbering_revision":0,"episodes":[]}`
	for _, verb := range []string{"GET", "PUT"} {
		if out := request(handler, verb, route, valid, nil, false); out.Code != 401 {
			t.Fatal("unprotected Anime endpoint", verb, out.Code, out.Body)
		}
	}
	if out := request(handler, "PUT", route, valid, &session, false); out.Code != 403 {
		t.Fatal("Anime mutation missing CSRF accepted", out.Code)
	}
	r := httptest.NewRequest("PUT", route, strings.NewReader(valid))
	r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	r.Header.Set("X-CSRF-Token", session.CSRFToken)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://other.invalid")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	if rec.Code != 403 {
		t.Fatal("cross-origin Anime mutation accepted", rec.Code)
	}
	for _, tc := range []struct {
		name, body, contentType string
		want                    int
	}{
		{"missing content type", valid, "", 415},
		{"text content type", valid, "text/plain", 415},
		{"null", "null", "application/json", 400},
		{"array", "[]", "application/json", 400},
		{"unknown field", strings.TrimSuffix(valid, "}") + `,"all_episodes":true}`, "application/json", 400},
		{"nested unknown field", strings.Replace(valid, `"episodes":[]`, `"episodes":[{"episode_id":1,"absolute_number":1,"guessed":true}]`, 1), "application/json", 400},
		{"trailing object", valid + `{}`, "application/json", 400},
		{"invalid category", strings.Replace(valid, "anime", "japanese", 1), "application/json", 400},
		{"oversized prefix", strings.Repeat(" ", (512<<10)+1) + valid, "application/json", 413},
		{"oversized tail", valid + strings.Repeat(" ", (512<<10)+1), "application/json", 413},
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
			var category string
			var revision int
			if err := db.QueryRow(`SELECT category,category_revision FROM media_items WHERE id=1`).Scan(&category, &revision); err != nil || category != "standard" || revision != 1 {
				t.Fatal("rejected request changed classification", category, revision, err)
			}
		})
	}
	tooMany := library.AnimeUpdate{Category: "anime", Revision: 1, NumberingRevision: 0, Episodes: make([]library.AnimeAlias, 5001)}
	b, err := json.Marshal(tooMany)
	if err != nil {
		t.Fatal(err)
	}
	if out := request(handler, "PUT", route, string(b), &session, true); out.Code != 400 {
		t.Fatal("more than 5000 mapping entries accepted", out.Code, out.Body)
	}
	// Real long-running shows can exceed the ordinary 16 KiB mutation cap. Keep
	// the endpoint's documented larger bound without loosening other handlers.
	if out := request(handler, "PUT", route, strings.Repeat(" ", 20<<10)+valid, &session, true); out.Code != 200 {
		t.Fatal("valid documented-size Anime request rejected", out.Code, out.Body)
	}
	if out := request(handler, "PUT", route, valid, &session, true); out.Code != 409 {
		t.Fatal("stale category revision accepted", out.Code, out.Body)
	}
	if out := request(handler, "GET", "/api/v1/library?kind=movie", "", &session, false); out.Code != 200 || !strings.Contains(out.Body.String(), "Reviewed film") {
		t.Fatal("classified film missing from Movies catalog", out.Code, out.Body)
	}
	if out := request(handler, "DELETE", route, "", &session, true); out.Code != 405 || out.Header().Get("Allow") != "GET, PUT" {
		t.Fatal("unexpected Anime method behavior", out.Code, out.Body)
	}
	if out := request(handler, "GET", "/api/v1/library/items/999/anime", "", &session, false); out.Code != 404 {
		t.Fatal("missing Anime title was not a 404", out.Code, out.Body)
	}
}

func TestReviewAnimeAddRetryRetainsExistingSection(t *testing.T) {
	provider := &testProvider{}
	handler, _, accounts, code := rawFixture(t, provider)
	session, err := accounts.Setup(context.Background(), "reviewadmin", "test-only-review-password", code)
	if err != nil {
		t.Fatal(err)
	}
	if out := request(handler, "PUT", "/api/v1/providers/tmdb", `{"token":"test-only-valid-provider-token"}`, &session, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	first := request(handler, "POST", "/api/v1/library/movies", `{"tmdb_id":11,"monitored":false,"category":"anime"}`, &session, true)
	if first.Code != 201 || !strings.Contains(first.Body.String(), `"category":"anime"`) {
		t.Fatal(first.Code, first.Body)
	}
	provider.fail = true
	retry := request(handler, "POST", "/api/v1/library/movies", `{"tmdb_id":11,"monitored":true,"category":"standard"}`, &session, true)
	if retry.Code != 200 || provider.movieCalls != 1 || !strings.Contains(retry.Body.String(), `"category":"anime"`) || !strings.Contains(retry.Body.String(), `"monitored":false`) {
		t.Fatal("duplicate retry changed section/monitoring or contacted provider", retry.Code, retry.Body)
	}
}
