package library

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"testing"
)

func TestKitsuOrderReviewScopesIDsAndPreservesCanonicalEpisodes(t *testing.T) {
	s, db, dir, item, ids := newAnimeOrderReviewStore(t)
	ctx := context.Background()
	old, e := s.AnimeOrder(ctx, item.ID)
	if e != nil {
		t.Fatal(e)
	}
	in := animeOrderReviewInput(old, ids)
	in.Provider = "kitsu"
	in.ProviderIDs = []int64{1}
	in.Entries[0].ProviderID = 1
	identity := metadata.AnimeIdentity{Provider: "kitsu", ID: 1, Title: "Owned identity", URL: metadata.AnimeIdentityURL("kitsu", 1)}
	wrong := identity
	wrong.Provider = "anilist"
	wrong.URL = metadata.AnimeIdentityURL("anilist", 1)
	if _, e = s.ReviewAnimeOrder(ctx, item.ID, in, []AnimeIdentity{wrong}); e == nil {
		t.Fatal("cross-provider ID reused")
	}
	r, e := s.ReviewAnimeOrder(ctx, item.ID, in, []AnimeIdentity{identity})
	if e != nil {
		t.Fatal(e)
	}
	if got, _ := s.AnimeOrder(ctx, item.ID); len(got.Identities) != 0 {
		t.Fatal("review changed state")
	}
	saved, e := s.ApplyAnimeOrder(ctx, item.ID, r.ReviewID)
	if e != nil || saved.Provider != "kitsu" || len(saved.Entries) != 3 {
		t.Fatal(saved, e)
	}
	if _, e = s.ApplyAnimeOrder(ctx, item.ID, r.ReviewID); e == nil {
		t.Fatal("replay accepted")
	}
	db.Close()
	reopened, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	got, e := New(reopened).AnimeOrder(ctx, item.ID)
	if e != nil || got.Provider != "kitsu" || len(got.Entries) != 3 || got.Identities[0].ID != 1 {
		t.Fatal(got, e)
	}
	snapshot, e := New(reopened).AnimeOrderSnapshot(ctx, item.ID, []int64{ids[1]})
	if e != nil || snapshot.Identities[0].Provider != "kitsu" {
		t.Fatal(snapshot, e)
	}
}
