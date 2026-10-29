package library

import (
	"context"
	"database/sql"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func newAnimeOrderReviewStore(t *testing.T) (*Store, *sql.DB, string, Item, map[int]int64) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db)
	seasons := []metadata.SeasonSummary{{TMDbID: 991010, Number: 0, Title: "Specials", EpisodeCount: 1}, {TMDbID: 991011, Number: 1, Title: "Season 1", EpisodeCount: 1}, {TMDbID: 991012, Number: 2, Title: "Season 2", EpisodeCount: 1}}
	item, _, err := s.AddSeries(ctx, metadata.Series{Movie: metadata.Movie{TMDbID: 991000, Title: "Reviewed Anime"}, Seasons: seasons}, true, "anime")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[int]int64{}
	for _, season := range seasons {
		show, err := s.TVShow(ctx, item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.SaveSeason(ctx, item.ID, metadata.Season{SeasonSummary: season, Episodes: []metadata.Episode{{TMDbID: 991100 + int64(season.Number), Number: 1, Title: "Canonical episode"}}}, show.Revision); err != nil {
			t.Fatal(err)
		}
		detail, err := s.TVSeason(ctx, item.ID, season.Number)
		if err != nil || len(detail.Episodes) != 1 {
			t.Fatal(detail, err)
		}
		ids[season.Number] = detail.Episodes[0].ID
	}
	anime, err := s.Anime(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, b := 25, 26
	if _, err = s.SaveAnime(ctx, item.ID, AnimeUpdate{Category: "anime", Revision: anime.Revision, NumberingRevision: anime.NumberingRevision, Episodes: []AnimeAlias{{EpisodeID: ids[1], AbsoluteNumber: &a}, {EpisodeID: ids[2], AbsoluteNumber: &b}}}); err != nil {
		t.Fatal(err)
	}
	return s, db, dir, item, ids
}

func animeOrderReviewInput(prior AnimeOrderSettings, ids map[int]int64) AnimeOrderUpdate {
	return AnimeOrderUpdate{Revision: prior.Revision, CategoryRevision: prior.CategoryRevision, NumberingRevision: prior.NumberingRevision, Entries: []AnimeOrderEntry{{EpisodeID: ids[1], ReleaseTitle: "Release Order", Label: "001"}, {EpisodeID: ids[2], ReleaseTitle: "Release Order", Label: "002"}, {EpisodeID: ids[0], ReleaseTitle: "Release Order", Label: "SP1"}}}
}

func TestReviewAnimeOrderProposalRequiresChosenContiguousSubgroupAndExactCanonicalIDs(t *testing.T) {
	s, db, _, item, ids := newAnimeOrderReviewStore(t)
	ctx := context.Background()
	base := metadata.EpisodeGroupDetails{ID: "order", Name: "Alternative release order", Groups: []metadata.EpisodeSubgroup{
		{ID: "chosen", Name: "Part one", Episodes: []metadata.GroupEpisode{{ID: 991102, ShowID: 991000, Season: 2, Number: 1, Order: 1}, {ID: 991100, ShowID: 991000, Season: 0, Number: 1, Order: 0}}},
		{ID: "other", Name: "Not chosen", Episodes: []metadata.GroupEpisode{{ID: 991101, ShowID: 991000, Season: 1, Number: 1, Order: 0}}},
	}}
	list, err := s.ProposeAnimeOrder(ctx, item.ID, base, "", 0, "Release order", "episode", 1)
	if err != nil || len(list.Groups) != 2 || len(list.Entries) != 0 {
		t.Fatal("whole order was flattened", list, err)
	}
	out, err := s.ProposeAnimeOrder(ctx, item.ID, base, "chosen", 0, "Release order", "SP", 3)
	if err != nil || len(out.Entries) != 2 || out.Entries[0].EpisodeID != ids[0] || out.Entries[0].Label != "SP3" || out.Entries[1].EpisodeID != ids[2] || out.Entries[1].Label != "SP4" {
		t.Fatal("selected order not respected", out, err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*metadata.EpisodeGroupDetails)
	}{
		{"gap", func(v *metadata.EpisodeGroupDetails) { v.Groups[0].Episodes[0].Order = 2 }},
		{"duplicate order", func(v *metadata.EpisodeGroupDetails) { v.Groups[0].Episodes[0].Order = 0 }},
		{"foreign show", func(v *metadata.EpisodeGroupDetails) { v.Groups[0].Episodes[0].ShowID = 999 }},
		{"duplicate canonical", func(v *metadata.EpisodeGroupDetails) { v.Groups[0].Episodes[0].ID = v.Groups[0].Episodes[1].ID }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := base
			v.Groups = append([]metadata.EpisodeSubgroup{}, base.Groups...)
			v.Groups[0].Episodes = append([]metadata.GroupEpisode{}, base.Groups[0].Episodes...)
			tc.mutate(&v)
			if _, err := s.ProposeAnimeOrder(ctx, item.ID, v, "chosen", 0, "Release order", "episode", 1); err == nil {
				t.Fatal("ambiguous order accepted")
			}
		})
	}
	// Matching season/number/title does not authorize a mismatched provider ID.
	v := base
	v.Groups = append([]metadata.EpisodeSubgroup{}, base.Groups...)
	v.Groups[0].Episodes = append([]metadata.GroupEpisode{}, base.Groups[0].Episodes...)
	v.Groups[0].Episodes[0].ID = 999999
	out, err = s.ProposeAnimeOrder(ctx, item.ID, v, "chosen", 0, "Release order", "episode", 1)
	if err != nil || len(out.Entries) != 1 || len(out.Unmatched) != 1 || out.Entries[0].Label != "1" || out.Unmatched[0].TMDbID != 999999 {
		t.Fatal("identity mismatch was inferred", out, err)
	}
	var count int
	if err = db.QueryRow(`SELECT (SELECT COUNT(*) FROM anime_orders)+(SELECT COUNT(*) FROM anime_order_reviews)+(SELECT COUNT(*) FROM anime_order_entries)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("proposal wrote mapping/review state", count, err)
	}
}

func TestReviewAnimeOrderIdentityAmbiguityExpiryReplayAndNoop(t *testing.T) {
	s, db, _, item, ids := newAnimeOrderReviewStore(t)
	ctx := context.Background()
	state, err := s.AnimeOrder(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	identities := []metadata.AnimeIdentity{{Provider: "anilist", ID: 41, Title: "One", URL: "https://anilist.co/anime/41"}, {Provider: "anilist", ID: 42, Title: "Two", URL: "https://anilist.co/anime/42"}}
	in := animeOrderReviewInput(state, ids)
	in.ProviderIDs = []int64{41, 42}
	in.Entries[0].ProviderID = 41
	in.Entries[1].ProviderID = 42
	in.Entries[1].Label = "1"
	if _, err = s.ReviewAnimeOrder(ctx, item.ID, in, identities); err == nil {
		t.Fatal("provider ID hid ambiguous filename identity")
	}
	in.Entries[1].Label = "2"
	review, err := s.ReviewAnimeOrder(ctx, item.ID, in, identities)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE anime_order_reviews SET expires_at=1 WHERE id=?`, review.ReviewID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID); err == nil {
		t.Fatal("expired review applied")
	}
	review, err = s.ReviewAnimeOrder(ctx, item.ID, in, identities)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID); err == nil {
		t.Fatal("applied review replayed")
	}
	noop := AnimeOrderUpdate{Revision: saved.Revision, CategoryRevision: saved.CategoryRevision, NumberingRevision: saved.NumberingRevision, ProviderIDs: []int64{42, 41}, Entries: saved.Entries}
	review, err = s.ReviewAnimeOrder(ctx, item.ID, noop, append([]metadata.AnimeIdentity{}, saved.Identities...))
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID)
	if err != nil || after.Revision != saved.Revision || after.NumberingRevision != saved.NumberingRevision {
		t.Fatal("no-op changed revisions", saved, after, err)
	}
}

func TestReviewAnimeOrderFilmLinksIdentityWithoutInventingEpisodeOrder(t *testing.T) {
	s, _ := animeReviewStore(t)
	ctx := context.Background()
	film, _, err := s.AddMovie(ctx, metadata.Movie{TMDbID: 998001, Title: "Anime film"}, true, "anime")
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.AnimeOrder(ctx, film.ID)
	if err != nil || old.NumberingRevision != 0 || len(old.Episodes) != 0 {
		t.Fatal(old, err)
	}
	identity := metadata.AnimeIdentity{Provider: "anilist", ID: 41, Title: "Provider Anime Film", Format: "MOVIE", URL: "https://anilist.co/anime/41"}
	in := AnimeOrderUpdate{Revision: old.Revision, CategoryRevision: old.CategoryRevision, NumberingRevision: old.NumberingRevision, ProviderIDs: []int64{41}}
	review, err := s.ReviewAnimeOrder(ctx, film.ID, in, []metadata.AnimeIdentity{identity})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.ApplyAnimeOrder(ctx, film.ID, review.ReviewID)
	if err != nil || saved.Revision != 1 || len(saved.Identities) != 1 || len(saved.Entries) != 0 || saved.NumberingRevision != 0 {
		t.Fatal(saved, err)
	}
	in.Revision = saved.Revision
	in.Entries = []AnimeOrderEntry{{EpisodeID: 1, ProviderID: 41, ReleaseTitle: "Anime film", Label: "1"}}
	if _, err = s.ReviewAnimeOrder(ctx, film.ID, in, []metadata.AnimeIdentity{identity}); err == nil {
		t.Fatal("film identity fabricated episode order")
	}
}

func TestReviewAnimeOrderSnapshotRejectsDuplicatesMissingEntriesAndLaterEdit(t *testing.T) {
	s, db, _, item, ids := newAnimeOrderReviewStore(t)
	ctx := context.Background()
	old, err := s.AnimeOrder(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.ReviewAnimeOrder(ctx, item.ID, animeOrderReviewInput(old, ids), nil)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID)
	if err != nil {
		t.Fatal(err)
	}
	for _, selection := range [][]int64{nil, {ids[1], ids[1]}, {ids[1], 999999}} {
		if _, err = s.AnimeOrderSnapshot(ctx, item.ID, selection); err == nil {
			t.Fatal("invalid canonical selection produced snapshot", selection)
		}
	}
	snapshot, err := s.AnimeOrderSnapshot(ctx, item.ID, []int64{ids[0], ids[2]})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateAnimeOrderSnapshot(ctx, tx, &snapshot)
	tx.Rollback()
	if err != nil {
		t.Fatal("fresh exact selection rejected", err)
	}
	update := animeOrderReviewInput(saved, ids)
	update.Entries[0].ReleaseTitle = "Changed release title"
	review, err = s.ReviewAnimeOrder(ctx, item.ID, update, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID); err != nil {
		t.Fatal(err)
	}
	tx, err = db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateAnimeOrderSnapshot(ctx, tx, &snapshot)
	tx.Rollback()
	if err == nil {
		t.Fatal("old queued decision adopted changed mapping")
	}
	// A snapshot remains the old evidence; validation must not rewrite it.
	if snapshot.Revision != saved.Revision || snapshot.Entries[0].ReleaseTitle != "Release Order" {
		t.Fatal("validation rewrote immutable evidence", snapshot)
	}
}

func TestReviewAnimeOrderPreviewRequiresSeparateApplyAndPreservesCanonicalState(t *testing.T) {
	s, db, _, item, ids := newAnimeOrderReviewStore(t)
	ctx := context.Background()
	prior, err := s.AnimeOrder(ctx, item.ID)
	if err != nil || prior.Revision != 0 || len(prior.Entries) != 0 || len(prior.Episodes) != 3 {
		t.Fatal("unexpected initial order state", prior, err)
	}
	showBefore, err := s.TVShow(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.ReviewAnimeOrder(ctx, item.ID, animeOrderReviewInput(prior, ids), nil)
	if err != nil || review.ReviewID == "" || len(review.Settings.Entries) != 3 || review.ExpiresAt <= 0 {
		t.Fatal("explicit mapping review missing", review, err)
	}
	unchanged, err := s.AnimeOrder(ctx, item.ID)
	if err != nil || unchanged.Revision != prior.Revision || len(unchanged.Entries) != 0 || unchanged.NumberingRevision != prior.NumberingRevision {
		t.Fatal("preview applied an order or bumped canonical revisions", unchanged, err)
	}
	saved, err := s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID)
	if err != nil || saved.Revision != 1 || len(saved.Entries) != 3 || saved.NumberingRevision <= prior.NumberingRevision {
		t.Fatal("reviewed order not applied", saved, err)
	}
	show, err := s.TVShow(ctx, item.ID)
	if err != nil || !show.Monitored || len(show.Seasons) != 3 || show.Seasons[0].Monitored || show.Seasons[1].Monitored != showBefore.Seasons[1].Monitored {
		t.Fatal("order mapping changed parent/special monitoring", show, err)
	}
	for _, expected := range []struct {
		id       int64
		absolute int
	}{{ids[1], 25}, {ids[2], 26}} {
		var number int
		if err = db.QueryRow(`SELECT absolute_number FROM anime_episode_numbers WHERE episode_id=?`, expected.id).Scan(&number); err != nil || number != expected.absolute {
			t.Fatal("order apply overwrote unrelated owner absolute aliases", expected.id, number, err)
		}
	}
	var searches, downloads int
	if err = db.QueryRow(`SELECT (SELECT COUNT(*) FROM anime_search_settings),(SELECT COUNT(*) FROM downloads)`).Scan(&searches, &downloads); err != nil || searches != 0 || downloads != 0 {
		t.Fatal("order apply implicitly opted into search or downloads", searches, downloads, err)
	}
}

func TestReviewAnimeOrderRejectsAmbiguousDuplicateAndForeignTargetsAtomically(t *testing.T) {
	s, _, _, item, ids := newAnimeOrderReviewStore(t)
	ctx := context.Background()
	prior, err := s.AnimeOrder(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	foreign, _ := animeReviewShow(t, s, 995000)
	foreignSeason, err := s.TVSeason(ctx, foreign.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*AnimeOrderUpdate){
		func(in *AnimeOrderUpdate) { in.Entries[1].EpisodeID = in.Entries[0].EpisodeID },
		func(in *AnimeOrderUpdate) { in.Entries[1].EpisodeID = foreignSeason.Episodes[0].ID },
		func(in *AnimeOrderUpdate) { in.Entries[1].Label = "1" },
		func(in *AnimeOrderUpdate) { in.Entries[1].Label = "001"; in.Entries[1].ReleaseTitle = "release.order" },
		func(in *AnimeOrderUpdate) { in.Entries[1].Label = "1-2" },
		func(in *AnimeOrderUpdate) { in.Entries[1].Label = "S00E00" },
		func(in *AnimeOrderUpdate) { in.Entries[1].Label = "SP0" },
		func(in *AnimeOrderUpdate) { in.Entries[1].ProviderID = 42 },
		func(in *AnimeOrderUpdate) { in.Entries[1].ReleaseTitle = "Hidden\nname" },
	} {
		input := animeOrderReviewInput(prior, ids)
		change(&input)
		if _, err = s.ReviewAnimeOrder(ctx, item.ID, input, nil); err == nil {
			t.Fatal("ambiguous/foreign mapping accepted", input)
		}
		current, err := s.AnimeOrder(ctx, item.ID)
		if err != nil || current.Revision != 0 || len(current.Entries) != 0 || current.NumberingRevision != prior.NumberingRevision {
			t.Fatal("invalid review partially changed canonical/order state", current, err)
		}
	}
}

func TestReviewAnimeOrderApplyFencesLaterReviewsAndMetadataChanges(t *testing.T) {
	for _, change := range []string{"another-order", "category", "monitoring"} {
		t.Run(change, func(t *testing.T) {
			s, _, _, item, ids := newAnimeOrderReviewStore(t)
			ctx := context.Background()
			prior, err := s.AnimeOrder(ctx, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			review, err := s.ReviewAnimeOrder(ctx, item.ID, animeOrderReviewInput(prior, ids), nil)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "another-order":
				input := animeOrderReviewInput(prior, ids)
				input.Entries[0].ReleaseTitle = "New Reviewed Title"
				newer, err := s.ReviewAnimeOrder(ctx, item.ID, input, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.ApplyAnimeOrder(ctx, item.ID, newer.ReviewID); err != nil {
					t.Fatal(err)
				}
			case "category":
				p, err := s.Anime(ctx, item.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.SaveAnime(ctx, item.ID, AnimeUpdate{Category: "standard", Revision: p.Revision, NumberingRevision: p.NumberingRevision}); err != nil {
					t.Fatal(err)
				}
			case "monitoring":
				show, err := s.TVShow(ctx, item.ID)
				if err != nil {
					t.Fatal(err)
				}
				if err = s.MonitorTV(ctx, item.ID, show.Revision, nil, nil, false); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.ApplyAnimeOrder(ctx, item.ID, review.ReviewID); err == nil {
				t.Fatal("stale reviewed mapping overwrote newer state", change)
			}
		})
	}
}

func TestReviewAnimeOrderSavedReviewAndAppliedMappingSurviveDatabaseReopen(t *testing.T) {
	s, db, dir, item, ids := newAnimeOrderReviewStore(t)
	ctx := context.Background()
	prior, err := s.AnimeOrder(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	review, err := s.ReviewAnimeOrder(ctx, item.ID, animeOrderReviewInput(prior, ids), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	saved, err := New(reopened).ApplyAnimeOrder(ctx, item.ID, review.ReviewID)
	if err != nil || saved.Revision != 1 || len(saved.Entries) != 3 {
		t.Fatal("saved review was lost on restart", saved, err)
	}
	current, err := New(reopened).AnimeOrder(ctx, item.ID)
	if err != nil || current.Revision != saved.Revision || len(current.Entries) != len(saved.Entries) {
		t.Fatal("applied order was not durable", current, err)
	}
}
