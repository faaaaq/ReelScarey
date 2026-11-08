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

func TestAniListRefreshCreatesReviewAndRetainsCanonicalIdentity(t *testing.T) {
	ctx := context.Background()
	_, db, accounts, code := rawFixture(t, nil)
	session, e := accounts.Setup(ctx, "refreshadmin", "fixture-refresh-password", code)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,category) VALUES(1,'movie',99,'Canonical film','anime')`); e != nil {
		t.Fatal(e)
	}
	store := library.New(db)
	old, e := store.AnimeOrder(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	initial, e := store.ReviewAnimeOrder(ctx, 1, library.AnimeOrderUpdate{Revision: old.Revision, CategoryRevision: old.CategoryRevision, NumberingRevision: old.NumberingRevision, ProviderIDs: []int64{41}, Entries: []library.AnimeOrderEntry{}}, []metadata.AnimeIdentity{{Provider: "anilist", ID: 41, Title: "Old facts", URL: "https://anilist.co/anime/41"}})
	if e != nil {
		t.Fatal(e)
	}
	saved, e := store.ApplyAnimeOrder(ctx, 1, initial.ReviewID)
	if e != nil {
		t.Fatal(e)
	}
	p := &reviewAnimeIdentityProvider{}
	h := New(db, config.Config{PublicURL: "http://example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{}, "test", Dependencies{Auth: accounts, AnimeProvider: p})
	route := "/api/v1/library/items/1/anime-order"
	resp := request(h, "POST", route+"/refresh", fmt.Sprintf(`{"revision":%d}`, saved.Revision), &session, true)
	if resp.Code != 200 {
		t.Fatal(resp.Code, resp.Body.String())
	}
	var review library.AnimeOrderReview
	if e = json.Unmarshal(resp.Body.Bytes(), &review); e != nil || review.Settings.Identities[0].Title != "Resolved provider title" {
		t.Fatal(review, e)
	}
	current, e := store.AnimeOrder(ctx, 1)
	if e != nil || current.Identities[0].Title != "Old facts" {
		t.Fatal("refresh applied without review", current, e)
	}
	resp = request(h, "PUT", route, `{"review_id":"`+review.ReviewID+`"}`, &session, true)
	if resp.Code != 200 {
		t.Fatal(resp.Code, resp.Body.String())
	}
	item, e := store.Item(ctx, 1)
	if e != nil || item.Title != "Canonical film" || item.TMDbID != 99 {
		t.Fatal("provider facts changed canonical identity", item, e)
	}
	calls := p.calls
	resp = request(h, "POST", route+"/refresh", fmt.Sprintf(`{"revision":%d}`, saved.Revision), &session, true)
	if resp.Code != 409 || p.calls != calls {
		t.Fatal("stale refresh made provider calls", resp.Code, p.calls)
	}
}
func TestNewFeatureRoutesKeepRequesterBoundaryAndMetadataScheduleRequiresCSRF(t *testing.T) {
	h, _, accounts, code := rawFixture(t, nil)
	ctx := context.Background()
	admin, e := accounts.Setup(ctx, "admin", "fixture-admin-password", code)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = accounts.CreateRequestAccount(ctx, "viewer", "fixture-viewer-password"); e != nil {
		t.Fatal(e)
	}
	viewer, e := accounts.Login(ctx, "viewer", "fixture-viewer-password")
	if e != nil {
		t.Fatal(e)
	}
	for _, p := range []struct{ method, path string }{{"GET", "/api/v1/system/metadata-refresh"}, {"POST", "/api/v1/system/metadata-refresh/run"}, {"POST", "/api/v1/library/refresh"}, {"POST", "/api/v1/library/items/1/anime-order/refresh"}, {"PUT", "/api/v1/library/series/1/subtitles"}, {"POST", "/api/v1/subtitles/files/1/edits"}, {"POST", "/api/v1/subtitles/edits/id/apply"}, {"POST", "/api/v1/youtube/bulk/review"}, {"POST", "/api/v1/youtube/bulk/queue"}, {"GET", "/api/v1/youtube/subscriptions"}, {"POST", "/api/v1/library/imports/id/matches/suggest"}, {"POST", "/api/v1/library/imports/id/matches/apply"}} {
		if out := request(h, p.method, p.path, "{}", &viewer, true); out.Code != 403 {
			t.Fatal("requester access", p, out.Code, out.Body.String())
		}
	}
	path := "/api/v1/system/metadata-refresh"
	out := request(h, "GET", path, "", &admin, true)
	if out.Code != 200 || !strings.Contains(out.Body.String(), `"enabled":false`) {
		t.Fatal(out.Code, out.Body.String())
	}
	body := `{"enabled":true,"interval_hours":12,"batch_size":3,"only_monitored":true,"revision":1}`
	if out = request(h, "PUT", path, body, &admin, false); out.Code != 403 {
		t.Fatal("missing CSRF accepted", out.Code)
	}
	if out = request(h, "PUT", path, body, &admin, true); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out = request(h, "PUT", path, body, &admin, true); out.Code != 409 {
		t.Fatal("stale schedule accepted", out.Code)
	}
	if out = request(h, "POST", "/api/v1/library/refresh", `{"kind":"movie"}`, &admin, false); out.Code != 403 {
		t.Fatal("library refresh accepted without CSRF", out.Code)
	}
	if out = request(h, "POST", "/api/v1/library/refresh", `{"kind":"movie"}`, &admin, true); out.Code != 202 || !strings.Contains(out.Body.String(), `"queued":0`) {
		t.Fatal("library refresh did not queue an empty movie library", out.Code, out.Body.String())
	}
	if out = request(h, "POST", "/api/v1/library/refresh", `{"kind":"music"}`, &admin, true); out.Code != 400 {
		t.Fatal("invalid library kind accepted", out.Code)
	}
	for _, page := range []string{"/metadata-refresh", "/youtube/subscriptions"} {
		r := httptest.NewRequest(http.MethodGet, "http://example.com"+page, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatal("missing direct page", page, w.Code)
		}
	}
}
