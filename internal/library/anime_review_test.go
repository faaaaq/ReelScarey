package library

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func animeReviewNumber(n int) *int { return &n }

func animeReviewStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db), db
}

func animeReviewShow(t *testing.T, s *Store, tmdb int64) (Item, metadata.Season) {
	t.Helper()
	ctx := context.Background()
	season := metadata.Season{SeasonSummary: metadata.SeasonSummary{TMDbID: tmdb + 1, Number: 1, Title: "Season 1", EpisodeCount: 2}, Episodes: []metadata.Episode{{TMDbID: tmdb + 2, Number: 1, Title: "Opening"}, {TMDbID: tmdb + 3, Number: 2, Title: "Return"}}}
	show := metadata.Series{Movie: metadata.Movie{TMDbID: tmdb, Title: "Anime review show"}, Seasons: []metadata.SeasonSummary{season.SeasonSummary}}
	item, _, err := s.AddSeries(ctx, show, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSeason(ctx, item.ID, season, 1); err != nil {
		t.Fatal(err)
	}
	return item, season
}

func TestReviewAnimeCatalogListsAllFilmsUnderMovies(t *testing.T) {
	s, _ := animeReviewStore(t)
	ctx := context.Background()
	standard, _, err := s.AddMovie(ctx, metadata.Movie{TMDbID: 1, Title: "日本語 anime-looking title"}, false)
	if err != nil {
		t.Fatal(err)
	}
	film, _, err := s.AddMovie(ctx, metadata.Movie{TMDbID: 2, Title: "Animated film"}, false, "anime")
	if err != nil {
		t.Fatal(err)
	}
	show, _, err := s.AddSeries(ctx, metadata.Series{Movie: metadata.Movie{TMDbID: 2, Title: "Animated show"}}, true, "anime")
	if err != nil || show.ID == film.ID || standard.Category != "standard" {
		t.Fatal("classification changed canonical type or guessed Anime", show, film, standard, err)
	}
	duplicate, created, err := s.AddMovie(ctx, metadata.Movie{TMDbID: 1, Title: "Changed title"}, true, "anime")
	if err != nil || created || duplicate.ID != standard.ID || duplicate.Category != "standard" || duplicate.Monitored || duplicate.Title != standard.Title {
		t.Fatal("duplicate add reclassified or changed existing title", duplicate, created, err)
	}
	for _, tc := range []struct {
		kind string
		ids  []int64
	}{{"movie", []int64{standard.ID, film.ID}}, {"series", nil}, {"anime", []int64{show.ID}}} {
		page, err := s.List(ctx, tc.kind, 1)
		if err != nil || page.Total != len(tc.ids) || page.CollectionTotal != len(tc.ids) {
			t.Fatal("section count mismatch", tc.kind, page, err)
		}
		seen := map[int64]bool{}
		for _, item := range page.Items {
			seen[item.ID] = true
		}
		for _, id := range tc.ids {
			if !seen[id] {
				t.Fatal("title missing from section", tc.kind, id)
			}
		}
	}
	counts, err := s.Counts(ctx)
	if err != nil || counts.Movies != 2 || counts.Series != 0 || counts.Anime != 1 {
		t.Fatal("overview does not match separate sections", counts, err)
	}
}

func TestReviewAnimeAliasesAreExplicitAtomicAndRevisionFenced(t *testing.T) {
	s, db := animeReviewStore(t)
	ctx := context.Background()
	item, _ := animeReviewShow(t, s, 100)
	foreign, _ := animeReviewShow(t, s, 200)
	initial, err := s.Anime(ctx, item.ID)
	other, otherErr := s.Anime(ctx, foreign.ID)
	if err != nil || otherErr != nil || len(initial.Episodes) != 2 || initial.Episodes[0].AbsoluteNumber != nil {
		t.Fatal(initial, err, otherErr)
	}
	if _, err = db.Exec(`UPDATE episodes SET monitored=0 WHERE id=?`, initial.Episodes[1].EpisodeID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,?,'TV Shows/Original/shared.mkv',10,'sha','{}',1,'existing')`, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(?,1),(?,1)`, initial.Episodes[0].EpisodeID, initial.Episodes[1].EpisodeID); err != nil {
		t.Fatal(err)
	}
	makeUpdate := func() AnimeUpdate {
		return AnimeUpdate{Category: "anime", Revision: initial.Revision, NumberingRevision: initial.NumberingRevision, Episodes: []AnimeAlias{{EpisodeID: initial.Episodes[0].EpisodeID, AbsoluteNumber: animeReviewNumber(25)}, {EpisodeID: initial.Episodes[1].EpisodeID, AbsoluteNumber: animeReviewNumber(26)}}}
	}
	for _, change := range []func(*AnimeUpdate){
		func(in *AnimeUpdate) { in.Episodes[1].AbsoluteNumber = animeReviewNumber(25) },
		func(in *AnimeUpdate) { in.Episodes[1].EpisodeID = in.Episodes[0].EpisodeID },
		func(in *AnimeUpdate) { in.Episodes[1].EpisodeID = other.Episodes[0].EpisodeID },
		func(in *AnimeUpdate) { in.Episodes[1].AbsoluteNumber = animeReviewNumber(0) },
		func(in *AnimeUpdate) { in.Episodes[1].AbsoluteNumber = animeReviewNumber(100001) },
	} {
		in := makeUpdate()
		change(&in)
		if _, err = s.SaveAnime(ctx, item.ID, in); !errors.Is(err, ErrAnimeInput) {
			t.Fatal("invalid alias map accepted", in, err)
		}
		got, err := s.Anime(ctx, item.ID)
		if err != nil || got.Category != "standard" || got.Revision != initial.Revision || got.NumberingRevision != initial.NumberingRevision || got.Episodes[0].AbsoluteNumber != nil {
			t.Fatal("invalid map partially changed classification or revisions", got, err)
		}
	}
	updated, err := s.SaveAnime(ctx, item.ID, makeUpdate())
	if err != nil || updated.Category != "anime" || *updated.Episodes[0].AbsoluteNumber != 25 || updated.Revision != initial.Revision+1 || updated.NumberingRevision != initial.NumberingRevision+1 {
		t.Fatal("explicit map not saved", updated, err)
	}
	if _, err = s.SaveAnime(ctx, item.ID, makeUpdate()); !errors.Is(err, ErrAnimeRevision) {
		t.Fatal("stale alias map accepted", err)
	}
	var assignmentBefore, assignmentAfter int
	if err = db.QueryRow(`SELECT revision FROM item_automation WHERE media_id=?`, item.ID).Scan(&assignmentBefore); err != nil {
		t.Fatal(err)
	}
	noop := makeUpdate()
	noop.Revision, noop.NumberingRevision = updated.Revision, updated.NumberingRevision
	same, err := s.SaveAnime(ctx, item.ID, noop)
	if err != nil || same.Revision != updated.Revision || same.NumberingRevision != updated.NumberingRevision {
		t.Fatal("no-op map bumped revisions", same, err)
	}
	if err = db.QueryRow(`SELECT revision FROM item_automation WHERE media_id=?`, item.ID).Scan(&assignmentAfter); err != nil || assignmentAfter != assignmentBefore {
		t.Fatal("no-op map invalidated unrelated review", assignmentBefore, assignmentAfter, err)
	}
	season, err := s.TVSeason(ctx, item.ID, 1)
	if err != nil || !season.Episodes[0].Monitored || season.Episodes[1].Monitored || !season.Episodes[0].Available || !season.Episodes[1].Available || season.Episodes[0].FilePath != "TV Shows/Original/shared.mkv" {
		t.Fatal("classification changed choices or file associations", season, err)
	}
}

func TestReviewAnimeMetadataRefreshRetainsAliasesAndRejectsIdentityChanges(t *testing.T) {
	s, _ := animeReviewStore(t)
	ctx := context.Background()
	item, season := animeReviewShow(t, s, 300)
	initial, err := s.Anime(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	update := AnimeUpdate{Category: "anime", Revision: initial.Revision, NumberingRevision: initial.NumberingRevision, Episodes: []AnimeAlias{{EpisodeID: initial.Episodes[0].EpisodeID, AbsoluteNumber: animeReviewNumber(101)}}}
	saved, err := s.SaveAnime(ctx, item.ID, update)
	if err != nil {
		t.Fatal(err)
	}
	season.Episodes[0].Title = "Refreshed title"
	if err = s.SaveSeason(ctx, item.ID, season, saved.NumberingRevision); err != nil {
		t.Fatal(err)
	}
	fresh, err := s.Anime(ctx, item.ID)
	if err != nil || fresh.Episodes[0].Title != "Refreshed title" || fresh.Episodes[0].AbsoluteNumber == nil || *fresh.Episodes[0].AbsoluteNumber != 101 {
		t.Fatal("title refresh lost explicit map", fresh, err)
	}
	update.Revision, update.NumberingRevision = saved.Revision, saved.NumberingRevision
	if _, err = s.SaveAnime(ctx, item.ID, update); !errors.Is(err, ErrAnimeRevision) {
		t.Fatal("old pre-refresh numbering editor accepted", err)
	}
	season.Episodes[0].TMDbID++
	if err = s.SaveSeason(ctx, item.ID, season, fresh.NumberingRevision); err == nil {
		t.Fatal("changed provider identity silently reused an absolute alias")
	}
	after, err := s.Anime(ctx, item.ID)
	if err != nil || after.NumberingRevision != fresh.NumberingRevision || after.Episodes[0].AbsoluteNumber == nil || *after.Episodes[0].AbsoluteNumber != 101 {
		t.Fatal("rejected metadata refresh changed saved map", after, err)
	}
}
