package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type reviewAnimeIdentityProvider struct{ calls int }

func (p *reviewAnimeIdentityProvider) SearchAnime(context.Context, string) ([]metadata.AnimeIdentity, error) {
	p.calls++
	return []metadata.AnimeIdentity{}, nil
}
func (p *reviewAnimeIdentityProvider) Anime(_ context.Context, id int64) (metadata.AnimeIdentity, error) {
	p.calls++
	return metadata.AnimeIdentity{Provider: "anilist", ID: id, Title: "Resolved provider title", URL: fmt.Sprintf("https://anilist.co/anime/%d", id)}, nil
}

func TestReviewAnimeOrderAPIRejectsInvalidReviewsBeforeProviderCalls(t *testing.T) {
	_, db, accounts, code := rawFixture(t, nil)
	session, err := accounts.Setup(context.Background(), "reviewadmin", "test-only-review-password", code)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,category,monitored) VALUES(1,'series',1,'Canonical Anime','anime',1),(2,'series',2,'Other Anime','anime',1)`,
		`INSERT INTO series_details(media_id) VALUES(1),(2)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(1,1,11,0,'Specials','','2020-01-01',1,'loaded')`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,12,1,'Special','','2020-01-01',0)`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	p := &reviewAnimeIdentityProvider{}
	h := New(db, config.Config{ConfigDir: t.TempDir(), MediaDir: "/media", DownloadsDir: "/downloads", PublicURL: "http://example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{"index.html": {Data: []byte("test")}}, "test", Dependencies{Auth: accounts, AnimeProvider: p})
	route := "/api/v1/library/items/1/anime-order"
	valid := `{"revision":0,"category_revision":1,"numbering_revision":1,"provider_ids":[41],"entries":[{"episode_id":1,"provider_id":41,"release_title":"Release order","label":"SP1"}]}`
	for _, pair := range [][2]string{{"GET", route}, {"POST", route + "/preview"}, {"PUT", route}, {"GET", "/api/v1/metadata/anime?query=title"}, {"GET", route + "/sources"}, {"POST", route + "/propose"}} {
		if out := request(h, pair[0], pair[1], valid, nil, false); out.Code != 401 {
			t.Fatal("unprotected endpoint", pair, out.Code, out.Body)
		}
	}
	if out := request(h, "POST", route+"/preview", valid, &session, false); out.Code != 403 {
		t.Fatal("missing CSRF accepted", out.Code, out.Body)
	}
	for _, tc := range []struct {
		name, body, mime, origin string
		want                     int
	}{
		{"foreign origin", valid, "application/json", "https://foreign.invalid", 403},
		{"missing MIME", valid, "", "", 415}, {"text MIME", valid, "text/plain", "", 415},
		{"null", "null", "application/json", "", 400},
		{"trailing", valid + `{}`, "application/json", "", 400},
		{"forged facts", strings.TrimSuffix(valid, "}") + `,"identities":[{"id":41,"title":"Injected"}]}`, "application/json", "", 400},
		{"oversized", strings.Repeat(" ", (512<<10)+1) + valid, "application/json", "", 413},
		{"stale revision", strings.Replace(valid, `"revision":0`, `"revision":7`, 1), "application/json", "", 409},
		{"foreign episode", strings.Replace(valid, `"episode_id":1`, `"episode_id":999`, 1), "application/json", "", 400},
		{"duplicate providers", strings.Replace(valid, `[41]`, `[41,41]`, 1), "application/json", "", 400},
		{"nine new providers", strings.Replace(valid, `[41]`, `[41,42,43,44,45,46,47,48,49]`, 1), "application/json", "", 400},
		{"twenty-one providers", strings.Replace(valid, `[41]`, `[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,41]`, 1), "application/json", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", route+"/preview", strings.NewReader(tc.body))
			r.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
			r.Header.Set("Content-Type", tc.mime)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			out := httptest.NewRecorder()
			h.ServeHTTP(out, r)
			if out.Code != tc.want {
				t.Fatal(out.Code, tc.want, out.Body)
			}
			if p.calls != 0 {
				t.Fatal("invalid review contacted provider", p.calls)
			}
		})
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM anime_order_reviews`).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid review persisted", count, err)
	}
	out := request(h, "POST", route+"/preview", valid, &session, true)
	var review library.AnimeOrderReview
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &review) != nil || len(review.Settings.Identities) != 1 || review.Settings.Identities[0].Title != "Resolved provider title" || p.calls != 1 {
		t.Fatal("provider-backed preview failed", out.Code, out.Body, p.calls)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM anime_order_entries`).Scan(&count); err != nil || count != 0 {
		t.Fatal("preview applied mapping", count, err)
	}
	body := `{"review_id":"` + review.ReviewID + `"}`
	if out = request(h, "PUT", "/api/v1/library/items/2/anime-order", body, &session, true); out.Code != 409 {
		t.Fatal("foreign review accepted", out.Code, out.Body)
	}
	out = request(h, "PUT", route, body, &session, true)
	var saved library.AnimeOrderSettings
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &saved) != nil || saved.Revision != 1 || len(saved.Entries) != 1 || saved.Entries[0].Label != "SP1" {
		t.Fatal("review not applied", out.Code, out.Body)
	}
	if out = request(h, "PUT", route, body, &session, true); out.Code != 409 {
		t.Fatal("review replay accepted", out.Code, out.Body)
	}
	if p.calls != 1 {
		t.Fatal("apply refreshed provider facts", p.calls)
	}
	var monitored, searches, downloads int
	if err = db.QueryRow(`SELECT (SELECT monitored FROM episodes WHERE id=1),(SELECT COUNT(*) FROM anime_search_settings),(SELECT COUNT(*) FROM downloads)`).Scan(&monitored, &searches, &downloads); err != nil || monitored != 0 || searches != 0 || downloads != 0 {
		t.Fatal("order apply changed consent or queued work", monitored, searches, downloads, err)
	}
}
