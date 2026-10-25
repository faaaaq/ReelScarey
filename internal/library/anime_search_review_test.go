package library

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func newAnimeSearchReviewStore(t *testing.T) (*Store, *sql.DB, string, Item) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	s := New(db)
	item, _ := animeReviewShow(t, s, 990100)
	prior, err := s.Anime(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.SaveAnime(ctx, item.ID, AnimeUpdate{Category: "anime", Revision: prior.Revision, NumberingRevision: prior.NumberingRevision})
	if err != nil {
		t.Fatal(err)
	}
	return s, db, dir, item
}

func animeSearchReviewUpdate(prior AnimeSearchSettings, mode string) AnimeSearchUpdate {
	return AnimeSearchUpdate{Revision: prior.Revision, CategoryRevision: prior.CategoryRevision, NumberingRevision: prior.NumberingRevision, Mode: mode, SearchTitle: "Romanized title", AlternateTitles: []string{"Romanized title"}}
}

func TestReviewAnimeSearchDefaultsOffNoopDoesNotCreateConsent(t *testing.T) {
	s, db, _, item := newAnimeSearchReviewStore(t)
	ctx := context.Background()
	prior, err := s.AnimeSearch(ctx, item.ID)
	if err != nil || prior.Mode != "disabled" || prior.Revision != 0 || prior.Category != "anime" {
		t.Fatal("new Anime show must not inherit automatic search consent", prior, err)
	}
	var before int
	if err = db.QueryRow(`SELECT revision FROM item_automation WHERE media_id=?`, item.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	unchanged, err := s.SaveAnimeSearch(ctx, item.ID, AnimeSearchUpdate{Revision: prior.Revision, CategoryRevision: prior.CategoryRevision, NumberingRevision: prior.NumberingRevision, Mode: "disabled"})
	if err != nil || unchanged.Revision != 0 || unchanged.NumberingRevision != prior.NumberingRevision {
		t.Fatal("disabled no-op changed revisions", unchanged, err)
	}
	var count, after int
	if err = db.QueryRow(`SELECT COUNT(*) FROM anime_search_settings WHERE media_id=?`, item.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("default no-op fabricated saved consent", count, err)
	}
	if err = db.QueryRow(`SELECT revision FROM item_automation WHERE media_id=?`, item.ID).Scan(&after); err != nil || after != before {
		t.Fatal("disabled no-op invalidated unrelated review", before, after, err)
	}
}

func TestReviewAnimeSearchExplicitConsentPersistsAndFencesOldEditors(t *testing.T) {
	s, db, dir, item := newAnimeSearchReviewStore(t)
	ctx := context.Background()
	prior, err := s.AnimeSearch(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	optIn := animeSearchReviewUpdate(prior, "absolute")
	saved, err := s.SaveAnimeSearch(ctx, item.ID, optIn)
	if err != nil || saved.Mode != "absolute" || saved.Revision != 1 || saved.SearchTitle != "Romanized title" || len(saved.AlternateTitles) != 1 || saved.NumberingRevision <= prior.NumberingRevision {
		t.Fatal("explicit per-show consent not saved", saved, err)
	}
	if _, err = s.SaveAnimeSearch(ctx, item.ID, optIn); err == nil {
		t.Fatal("stale settings editor overwrote explicit consent")
	}
	noop := animeSearchReviewUpdate(saved, "absolute")
	same, err := s.SaveAnimeSearch(ctx, item.ID, noop)
	if err != nil || same.Revision != saved.Revision || same.NumberingRevision != saved.NumberingRevision {
		t.Fatal("identical saved search policy is not a no-op", same, err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	current, err := New(reopened).AnimeSearch(ctx, item.ID)
	if err != nil || current.Mode != "absolute" || current.Revision != saved.Revision || current.SearchTitle != saved.SearchTitle || len(current.AlternateTitles) != 1 {
		t.Fatal("per-show consent or alias lost across restart", current, err)
	}
}

func TestReviewAnimeSearchRejectsUnreviewedNamesAndInvalidModesAtomically(t *testing.T) {
	s, db, _, item := newAnimeSearchReviewStore(t)
	ctx := context.Background()
	prior, err := s.AnimeSearch(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*AnimeSearchUpdate){
		func(in *AnimeSearchUpdate) { in.Mode = "automatic" },
		func(in *AnimeSearchUpdate) { in.Mode = "ABSOLUTE" },
		func(in *AnimeSearchUpdate) { in.SearchTitle = "Unreviewed title" },
		func(in *AnimeSearchUpdate) { in.AlternateTitles = []string{"Romanized title", "romanized.title"} },
		func(in *AnimeSearchUpdate) { in.AlternateTitles = []string{"!!!"}; in.SearchTitle = "" },
		func(in *AnimeSearchUpdate) { in.AlternateTitles = []string{"Hidden\ntitle"}; in.SearchTitle = "" },
		func(in *AnimeSearchUpdate) {
			in.AlternateTitles = []string{strings.Repeat("界", 70)}
			in.SearchTitle = ""
		},
		func(in *AnimeSearchUpdate) {
			in.AlternateTitles = []string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
			in.SearchTitle = "one"
		},
	} {
		in := animeSearchReviewUpdate(prior, "season")
		change(&in)
		if _, err = s.SaveAnimeSearch(ctx, item.ID, in); err == nil {
			t.Fatal("invalid naming/mode was accepted", in)
		}
		current, err := s.AnimeSearch(ctx, item.ID)
		if err != nil || current.Mode != "disabled" || current.Revision != 0 || current.NumberingRevision != prior.NumberingRevision {
			t.Fatal("rejected policy partially changed consent or revisions", current, err)
		}
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM anime_search_settings WHERE media_id=?`, item.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid policy persisted a row", count, err)
	}
}

func TestReviewAnimeSearchFencesCategoryAndCanonicalEpisodeMetadata(t *testing.T) {
	for _, mutation := range []string{"category", "episode-metadata"} {
		t.Run(mutation, func(t *testing.T) {
			s, _, _, item := newAnimeSearchReviewStore(t)
			ctx := context.Background()
			prior, err := s.AnimeSearch(ctx, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			anime, err := s.Anime(ctx, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mutation == "category" {
				if _, err = s.SaveAnime(ctx, item.ID, AnimeUpdate{Category: "standard", Revision: anime.Revision, NumberingRevision: anime.NumberingRevision}); err != nil {
					t.Fatal(err)
				}
			} else {
				season, err := s.TVSeason(ctx, item.ID, 1)
				if err != nil {
					t.Fatal(err)
				}
				if err = s.MonitorTV(ctx, item.ID, season.Revision, nil, nil, false); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = s.SaveAnimeSearch(ctx, item.ID, animeSearchReviewUpdate(prior, "season")); err == nil {
				t.Fatal("unreviewed category/episode revision accepted", mutation)
			}
		})
	}
}

func TestReviewAnimeSearchCanAlwaysRevokeConsentAfterMetadataTitleChanges(t *testing.T) {
	for _, change := range []string{"unqueryable-canonical", "canonical-now-an-alias", "selected-canonical-outdated"} {
		t.Run(change, func(t *testing.T) {
			s, _, _, item := newAnimeSearchReviewStore(t)
			ctx := context.Background()
			prior, err := s.AnimeSearch(ctx, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			input := AnimeSearchUpdate{Revision: prior.Revision, CategoryRevision: prior.CategoryRevision, NumberingRevision: prior.NumberingRevision, Mode: "season"}
			newTitle := strings.Repeat("Long title ", 22)
			if change == "canonical-now-an-alias" {
				input.AlternateTitles = []string{"Future Canonical"}
				newTitle = "Future Canonical"
			} else if change == "selected-canonical-outdated" {
				input.SearchTitle = prior.CanonicalTitle
				newTitle = "Renamed Canonical"
			}
			saved, err := s.SaveAnimeSearch(ctx, item.ID, input)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.RefreshSeries(ctx, item.ID, metadata.Series{Movie: metadata.Movie{TMDbID: item.TMDbID, Title: newTitle}}, saved.NumberingRevision); err != nil {
				t.Fatal(err)
			}
			current, err := s.AnimeSearch(ctx, item.ID)
			if err != nil {
				t.Fatal(err)
			}
			disabled, err := s.SaveAnimeSearch(ctx, item.ID, AnimeSearchUpdate{Revision: current.Revision, CategoryRevision: current.CategoryRevision, NumberingRevision: current.NumberingRevision, Mode: "disabled", SearchTitle: current.SearchTitle, AlternateTitles: current.AlternateTitles})
			if err != nil || disabled.Mode != "disabled" || disabled.Revision != current.Revision+1 {
				t.Fatal("metadata changes prevented explicit revocation of search consent", change, disabled, err)
			}
		})
	}
}
