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

func TestReviewMappedAnimeAPIRequiresCurrentExplicitConsentAndRejectsForgedApproval(t *testing.T) {
	h, db, accounts, code := rawFixture(t, nil)
	session, err := accounts.Setup(context.Background(), "reviewadmin", "test-only-review-password", code)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,category,monitored) VALUES(1,'series',1,'Canonical Anime','anime',1)`,
		`INSERT INTO series_details(media_id) VALUES(1)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(1,1,11,1,'Regular','','2020-01-01',1,'loaded'),(2,1,12,0,'Specials','','2020-01-01',1,'loaded')`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,21,1,'Regular','','2020-01-01',1),(2,2,22,1,'Special','','2020-01-01',0)`,
		`INSERT INTO anime_orders(media_id,revision) VALUES(1,1)`,
		`INSERT INTO anime_order_entries(media_id,episode_id,release_title,label) VALUES(1,1,'Release source','1'),(1,2,'Release source','SP1')`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	route := "/api/v1/library/items/1/anime-search"
	valid := `{"revision":0,"category_revision":1,"numbering_revision":1,"mode":"mapped","include_specials":true,"allow_packs":true,"max_pack_episodes":3,"order_revision":1}`
	if out := request(h, "PUT", route, valid, nil, false); out.Code != 401 {
		t.Fatal("unauthenticated mapped consent accepted", out.Code, out.Body)
	}
	if out := request(h, "PUT", route, valid, &session, false); out.Code != 403 {
		t.Fatal("mapped consent accepted without CSRF", out.Code, out.Body)
	}
	for _, tc := range []struct {
		name, body, mime, origin string
		want                     int
	}{
		{"origin", valid, "application/json", "https://foreign.invalid", 403},
		{"text", valid, "text/plain", "", 415},
		{"null", "null", "application/json", "", 400},
		{"trailing", valid + `{}`, "application/json", "", 400},
		{"forged approval", strings.TrimSuffix(valid, "}") + `,"approved_order_revision":1}`, "application/json", "", 400},
		{"invalid bool", strings.Replace(valid, `"include_specials":true`, `"include_specials":"true"`, 1), "application/json", "", 400},
		{"negative bound", strings.Replace(valid, `"max_pack_episodes":3`, `"max_pack_episodes":-1`, 1), "application/json", "", 400},
		{"too large bound", strings.Replace(valid, `"max_pack_episodes":3`, `"max_pack_episodes":101`, 1), "application/json", "", 400},
		{"unreviewed order", strings.Replace(valid, `"order_revision":1`, `"order_revision":0`, 1), "application/json", "", 409},
		{"stale policy", strings.Replace(valid, `"revision":0`, `"revision":9`, 1), "application/json", "", 409},
		{"oversized", strings.Repeat(" ", 17<<10) + valid, "application/json", "", 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("PUT", route, strings.NewReader(tc.body))
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
		})
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM anime_search_settings`).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid mapped consent persisted", count, err)
	}
	out := request(h, "PUT", route, valid, &session, true)
	var saved library.AnimeSearchSettings
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &saved) != nil || saved.Mode != "mapped" || saved.ApprovedOrderRevision != 1 || saved.OrderRevision != 1 || !saved.IncludeSpecials || !saved.AllowPacks || saved.MaxPackEpisodes != 3 {
		t.Fatal("mapped consent not saved", out.Code, out.Body)
	}
	if _, err = db.Exec(`UPDATE anime_orders SET revision=2 WHERE media_id=1`); err != nil {
		t.Fatal(err)
	}
	out = request(h, "GET", route, "", &session, false)
	var stale library.AnimeSearchSettings
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &stale) != nil || stale.OrderRevision != 2 || stale.ApprovedOrderRevision != 1 || stale.ReadyCount != 0 {
		t.Fatal("API hides need to reapprove changed mapping", out.Code, out.Body)
	}
	var monitor, downloads int
	if err = db.QueryRow(`SELECT (SELECT monitored FROM episodes WHERE id=2),(SELECT COUNT(*) FROM downloads)`).Scan(&monitor, &downloads); err != nil || monitor != 0 || downloads != 0 {
		t.Fatal("policy consent changed monitoring or queued work", monitor, downloads, err)
	}
}
