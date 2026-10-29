package library

import (
	"context"
	"testing"
)

func TestReviewMappedAnimeRequiresExplicitCurrentOrderApproval(t *testing.T) {
	s, db, _, item, ids := newAnimeOrderReviewStore(t)
	ctx := context.Background()
	p, err := s.AnimeSearch(ctx, item.ID)
	if err != nil || p.Mode != "disabled" || p.IncludeSpecials || p.AllowPacks || p.MaxPackEpisodes != 20 || p.ApprovedOrderRevision != 0 {
		t.Fatal("mapped automation not disabled by default", p, err)
	}
	old, err := s.AnimeOrder(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.ReviewAnimeOrder(ctx, item.ID, animeOrderReviewInput(old, ids), nil)
	if err != nil {
		t.Fatal(err)
	}
	order, err := s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.AnimeSearch(ctx, item.ID)
	if err != nil || p.Mode != "disabled" || p.ApprovedOrderRevision != 0 {
		t.Fatal("saving mapping enabled consent", p, err)
	}
	in := AnimeSearchUpdate{Revision: p.Revision, CategoryRevision: p.CategoryRevision, NumberingRevision: p.NumberingRevision, Mode: "mapped", MaxPackEpisodes: 20, OrderRevision: order.Revision - 1}
	if _, err = s.SaveAnimeSearch(ctx, item.ID, in); err == nil {
		t.Fatal("unapproved current order enabled mapped automation")
	}
	in.OrderRevision = order.Revision
	in.IncludeSpecials = true
	in.AllowPacks = true
	saved, err := s.SaveAnimeSearch(ctx, item.ID, in)
	if err != nil || saved.Mode != "mapped" || !saved.IncludeSpecials || !saved.AllowPacks || saved.ApprovedOrderRevision != order.Revision || saved.OrderRevision != order.Revision {
		t.Fatal("explicit mapped consent missing", saved, err)
	}
	noop := AnimeSearchUpdate{Revision: saved.Revision, CategoryRevision: saved.CategoryRevision, NumberingRevision: saved.NumberingRevision, Mode: saved.Mode, SearchTitle: saved.SearchTitle, AlternateTitles: saved.AlternateTitles, IncludeSpecials: saved.IncludeSpecials, AllowPacks: saved.AllowPacks, MaxPackEpisodes: saved.MaxPackEpisodes, OrderRevision: saved.OrderRevision}
	after, err := s.SaveAnimeSearch(ctx, item.ID, noop)
	if err != nil || after.Revision != saved.Revision || after.NumberingRevision != saved.NumberingRevision {
		t.Fatal("mapped no-op bumped revisions", saved, after, err)
	}
	order, err = s.AnimeOrder(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	edit := animeOrderReviewInput(order, ids)
	edit.Entries[0].ReleaseTitle = "Changed release source"
	review, err = s.ReviewAnimeOrder(ctx, item.ID, edit, nil)
	if err != nil {
		t.Fatal(err)
	}
	newOrder, err := s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := s.AnimeSearch(ctx, item.ID)
	if err != nil || stale.ApprovedOrderRevision != saved.ApprovedOrderRevision || stale.OrderRevision != newOrder.Revision || stale.ReadyCount != 0 {
		t.Fatal("mapping edit silently reapproved automation", stale, err)
	}
	noop.Revision = stale.Revision
	noop.NumberingRevision = stale.NumberingRevision
	if _, err = s.SaveAnimeSearch(ctx, item.ID, noop); err == nil {
		t.Fatal("old order approval accepted against changed order")
	}
	noop.OrderRevision = stale.OrderRevision
	reapproved, err := s.SaveAnimeSearch(ctx, item.ID, noop)
	if err != nil || reapproved.ApprovedOrderRevision != newOrder.Revision || reapproved.Revision <= stale.Revision {
		t.Fatal("fresh mapped reapproval failed", reapproved, err)
	}
	var absolute1, absolute2, downloads int
	if err = db.QueryRow(`SELECT (SELECT absolute_number FROM anime_episode_numbers WHERE episode_id=?),(SELECT absolute_number FROM anime_episode_numbers WHERE episode_id=?),(SELECT COUNT(*) FROM downloads)`, ids[1], ids[2]).Scan(&absolute1, &absolute2, &downloads); err != nil || absolute1 != 25 || absolute2 != 26 || downloads != 0 {
		t.Fatal("mapped consent changed absolute aliases or queued work", absolute1, absolute2, downloads, err)
	}
}

func TestReviewMappedAnimeBoundsAndRevocationDoNotRequireUsableOrder(t *testing.T) {
	s, _, _, item, ids := newAnimeOrderReviewStore(t)
	ctx := context.Background()
	order, err := s.AnimeOrder(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.ReviewAnimeOrder(ctx, item.ID, animeOrderReviewInput(order, ids), nil)
	if err != nil {
		t.Fatal(err)
	}
	order, err = s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.AnimeSearch(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	in := AnimeSearchUpdate{Revision: p.Revision, CategoryRevision: p.CategoryRevision, NumberingRevision: p.NumberingRevision, Mode: "mapped", AllowPacks: true, MaxPackEpisodes: 20, OrderRevision: order.Revision}
	for _, n := range []int{-1, 1, 101} {
		bad := in
		bad.MaxPackEpisodes = n
		if _, err = s.SaveAnimeSearch(ctx, item.ID, bad); err == nil {
			t.Fatal("out-of-bound mapped pack size accepted", n)
		}
	}
	p, err = s.SaveAnimeSearch(ctx, item.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	order, err = s.AnimeOrder(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	empty := AnimeOrderUpdate{Revision: order.Revision, CategoryRevision: order.CategoryRevision, NumberingRevision: order.NumberingRevision}
	review, err = s.ReviewAnimeOrder(ctx, item.ID, empty, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID); err != nil {
		t.Fatal(err)
	}
	p, err = s.AnimeSearch(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	disabled, err := s.SaveAnimeSearch(ctx, item.ID, AnimeSearchUpdate{Revision: p.Revision, CategoryRevision: p.CategoryRevision, NumberingRevision: p.NumberingRevision, Mode: "disabled", MaxPackEpisodes: 20})
	if err != nil || disabled.Mode != "disabled" {
		t.Fatal("unable to revoke consent after order removed", disabled, err)
	}
}
