package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type kitsuReviewProvider struct{ calls int }

func (p *kitsuReviewProvider) Anime(_ context.Context, id int64) (metadata.AnimeIdentity, error) {
	p.calls++
	return metadata.AnimeIdentity{Provider: "kitsu", ID: id, Title: "Kitsu identity", URL: fmt.Sprintf("https://kitsu.io/anime/%d", id)}, nil
}
func (p *kitsuReviewProvider) SearchAnime(ctx context.Context, _ string) ([]metadata.AnimeIdentity, error) {
	v, e := p.Anime(ctx, 41)
	return []metadata.AnimeIdentity{v}, e
}

func TestKitsuAPIUsesScopedIdentityForSearchReviewAndRefresh(t *testing.T) {
	ctx := context.Background()
	_, db, accounts, code := rawFixture(t, nil)
	session, e := accounts.Setup(ctx, "kitsuadmin", "fixture-kitsu-password", code)
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
	initial, e := store.ReviewAnimeOrder(ctx, 1, library.AnimeOrderUpdate{Revision: old.Revision, CategoryRevision: old.CategoryRevision, NumberingRevision: old.NumberingRevision, ProviderIDs: []int64{41}}, []metadata.AnimeIdentity{{Provider: "anilist", ID: 41, Title: "Different AniList identity", URL: "https://anilist.co/anime/41"}})
	if e != nil {
		t.Fatal(e)
	}
	saved, e := store.ApplyAnimeOrder(ctx, 1, initial.ReviewID)
	if e != nil {
		t.Fatal(e)
	}
	ani, kitsu := &reviewAnimeIdentityProvider{}, &kitsuReviewProvider{}
	h := New(db, config.Config{PublicURL: "http://example.com"}, slog.New(slog.NewTextHandler(io.Discard, nil)), fstest.MapFS{}, "test", Dependencies{Auth: accounts, AnimeProvider: ani, KitsuProvider: kitsu})
	for _, query := range []string{"provider=unknown", "provider=kitsu&provider=anilist", "provider=kitsu&extra=x"} {
		out := request(h, "GET", "/api/v1/metadata/anime?query=title&"+query, "", &session, false)
		if out.Code != 400 {
			t.Fatal(out.Code, out.Body)
		}
	}
	if ani.calls != 0 || kitsu.calls != 0 {
		t.Fatal("invalid provider contacted upstream")
	}
	out := request(h, "GET", "/api/v1/metadata/anime?query=title&provider=kitsu", "", &session, false)
	if out.Code != 200 || kitsu.calls != 1 || ani.calls != 0 {
		t.Fatal(out.Code, out.Body)
	}
	route := "/api/v1/library/items/1/anime-order"
	body := fmt.Sprintf(`{"provider":"kitsu","revision":%d,"category_revision":%d,"numbering_revision":%d,"provider_ids":[41],"entries":[]}`, saved.Revision, saved.CategoryRevision, saved.NumberingRevision)
	out = request(h, "POST", route+"/preview", body, &session, true)
	var review library.AnimeOrderReview
	if out.Code != 200 || json.Unmarshal(out.Body.Bytes(), &review) != nil || len(review.Settings.Identities) != 1 || review.Settings.Identities[0].Provider != "kitsu" || kitsu.calls != 2 {
		t.Fatal("same numeric ID reused another provider", out.Code, out.Body)
	}
	current, e := store.AnimeOrder(ctx, 1)
	if e != nil || current.Identities[0].Provider != "anilist" {
		t.Fatal("preview applied", e)
	}
	out = request(h, "PUT", route, `{"review_id":"`+review.ReviewID+`"}`, &session, true)
	if out.Code != 200 {
		t.Fatal(out.Code, out.Body)
	}
	current, e = store.AnimeOrder(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	out = request(h, "POST", route+"/refresh", fmt.Sprintf(`{"revision":%d}`, current.Revision), &session, true)
	if out.Code != 200 || ani.calls != 0 || kitsu.calls != 3 {
		t.Fatal("refresh used wrong provider", out.Code, out.Body)
	}
	item, e := store.Item(ctx, 1)
	if e != nil || item.TMDbID != 99 || item.Title != "Canonical film" {
		t.Fatal("canonical identity changed", e)
	}
}
