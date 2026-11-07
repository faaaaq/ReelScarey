package automation

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func animeSearchAutomationReviewFixture(t *testing.T) fixture {
	t.Helper()
	f := setup(t)
	for _, q := range []string{
		`DELETE FROM media_items`,
		`INSERT INTO media_items(id,kind,tmdb_id,title,year,category,monitored) VALUES(5,'series',5,'Canonical Anime',2020,'anime',1)`,
		`INSERT INTO series_details(media_id) VALUES(5)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',1,1,'loaded')`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','2020-01-01',1)`,
	} {
		if _, err := f.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	f.i.title = "[Fansub] Canonical Anime S01E01v2 [1080p][English][WEB]"
	return f
}

func animeSearchAutomationReviewPolicy(t *testing.T, f fixture, mode string) {
	t.Helper()
	ctx := context.Background()
	s := library.New(f.db)
	p, err := s.AnimeSearch(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveAnimeSearch(ctx, 5, library.AnimeSearchUpdate{Revision: p.Revision, CategoryRevision: p.CategoryRevision, NumberingRevision: p.NumberingRevision, Mode: mode, AlternateTitles: []string{"Approved Alias"}}); err != nil {
		t.Fatal(err)
	}
}

func animeSearchAutomationReviewAlias(t *testing.T, f fixture, category string, number *int) {
	t.Helper()
	ctx := context.Background()
	s := library.New(f.db)
	p, err := s.Anime(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveAnime(ctx, 5, library.AnimeUpdate{Category: category, Revision: p.Revision, NumberingRevision: p.NumberingRevision, Episodes: []library.AnimeAlias{{EpisodeID: 1, AbsoluteNumber: number}}}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewAnimeAutomaticSearchRequiresBothConsentAndUsableNumbering(t *testing.T) {
	for _, mode := range []string{"disabled", "absolute"} {
		t.Run(mode, func(t *testing.T) {
			f := animeSearchAutomationReviewFixture(t)
			ctx := context.Background()
			if mode != "disabled" {
				animeSearchAutomationReviewPolicy(t, f, mode)
			}
			configure(t, f.s, true, "download", 10)
			if _, err := f.s.ProcessNext(ctx); err != nil {
				t.Fatal(err)
			}
			if f.i.calls != 0 {
				t.Fatal("disabled/unmapped show contacted indexers", f.i.calls)
			}
			var count int
			if err := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); err != nil || count != 0 {
				t.Fatal("disabled/unmapped show queued work", count, err)
			}
			if mode == "absolute" {
				runs, err := f.s.Runs(ctx)
				if err != nil || len(runs) != 1 {
					t.Fatal(runs, err)
				}
				results, err := f.s.Results(ctx, runs[0].ID)
				if err != nil || len(results) != 1 || results[0].Outcome != "needs_numbering" {
					t.Fatal("unmapped absolute episode not explained", results, err)
				}
			}
		})
	}
}

func TestReviewAnimeConsentDoesNotEnableGlobalAutomationOrSkipSearchOnly(t *testing.T) {
	f := animeSearchAutomationReviewFixture(t)
	ctx := context.Background()
	animeSearchAutomationReviewPolicy(t, f, "season")
	configure(t, f.s, false, "search", 10)
	if worked, err := f.s.ProcessNext(ctx); worked || err != nil || f.i.calls != 0 {
		t.Fatal("per-show consent enabled the paused global schedule", worked, f.i.calls, err)
	}
	if err := f.s.Request(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	runs, err := f.s.Runs(ctx)
	if err != nil || len(runs) != 1 {
		t.Fatal(runs, err)
	}
	results, err := f.s.Results(ctx, runs[0].ID)
	if err != nil || len(results) != 1 || results[0].Outcome != "match" {
		t.Fatal("explicit Anime one-off did not retain search-only behavior", results, err)
	}
	var count int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); err != nil || count != 0 {
		t.Fatal("search-only Anime run queued downloads", count, err)
	}
}

func TestReviewAnimeMetainfoFetchCannotReusePolicyOrIdentityChangedAwayAndBack(t *testing.T) {
	for _, change := range []string{"policy", "alternate-titles", "absolute-alias", "category", "show-monitoring", "episode-monitoring", "quality-profile"} {
		t.Run(change, func(t *testing.T) {
			f := animeSearchAutomationReviewFixture(t)
			ctx := context.Background()
			mode := "season"
			if change == "absolute-alias" {
				mode = "absolute"
				n := 25
				animeSearchAutomationReviewAlias(t, f, "anime", &n)
				f.i.title = "[Fansub] Canonical Anime - 025v2 [1080p][English][WEB]"
			}
			animeSearchAutomationReviewPolicy(t, f, mode)
			configure(t, f.s, true, "download", 10)
			f.i.onFetch = func() {
				s := library.New(f.db)
				switch change {
				case "policy":
					animeSearchAutomationReviewPolicy(t, f, "disabled")
					animeSearchAutomationReviewPolicy(t, f, "season")
				case "alternate-titles":
					for _, alias := range []string{"Other Alias", "Approved Alias"} {
						p, err := s.AnimeSearch(ctx, 5)
						if err != nil {
							t.Fatal(err)
						}
						if _, err = s.SaveAnimeSearch(ctx, 5, library.AnimeSearchUpdate{Revision: p.Revision, CategoryRevision: p.CategoryRevision, NumberingRevision: p.NumberingRevision, Mode: p.Mode, AlternateTitles: []string{alias}}); err != nil {
							t.Fatal(err)
						}
					}
				case "absolute-alias":
					for _, number := range []int{26, 25} {
						animeSearchAutomationReviewAlias(t, f, "anime", &number)
					}
				case "category":
					for _, category := range []string{"standard", "anime"} {
						animeSearchAutomationReviewAlias(t, f, category, nil)
					}
				case "show-monitoring", "episode-monitoring":
					for _, monitored := range []bool{false, true} {
						show, err := s.TVShow(ctx, 5)
						if err != nil {
							t.Fatal(err)
						}
						var season, episode *int
						if change == "episode-monitoring" {
							a, b := 1, 1
							season, episode = &a, &b
						}
						if err = s.MonitorTV(ctx, 5, show.Revision, season, episode, monitored); err != nil {
							t.Fatal(err)
						}
					}
				case "quality-profile":
					store := quality.New(f.db)
					for _, seeders := range []int{2, 1} {
						p, err := store.Get(ctx, 1)
						if err != nil {
							t.Fatal(err)
						}
						p.MinSeeders = seeders
						if _, err = store.Save(ctx, p); err != nil {
							t.Fatal(err)
						}
					}
				}
			}
			if _, err := f.s.ProcessNext(ctx); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); err != nil || count != 0 {
				t.Fatal("stale automatic decision queued a download", change, count, err)
			}
		})
	}
}

func TestReviewAnimeQueuePersistsDeduplicatesAndImportsUnambiguousFiles(t *testing.T) {
	f := animeSearchAutomationReviewFixture(t)
	ctx := context.Background()
	animeSearchAutomationReviewPolicy(t, f, "season")
	configure(t, f.s, true, "download", 10)
	if _, err := f.s.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	jobs, err := f.w.List(ctx, 1)
	if err != nil || len(jobs.Items) != 1 || len(jobs.Items[0].Targets) != 1 || jobs.Items[0].Targets[0].EpisodeID != 1 {
		t.Fatal("automatic Anime queue did not reserve exactly one canonical episode", jobs, err)
	}
	jobID := jobs.Items[0].ID
	before := f.i.calls
	if err = f.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := database.Open(ctx, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	secrets, err := securestore.New(ctx, db, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	w := transfer.New(db, secrets, f.cfg, &client{}, f.i)
	scheduler := New(db, w)
	configure(t, scheduler, true, "download", 10)
	if _, err = scheduler.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	if f.i.calls != before {
		t.Fatal("reserved Anime episode was searched again after restart", f.i.calls, before)
	}
	var downloads, reservations int
	if err = db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads),(SELECT COUNT(*) FROM episode_downloads)`).Scan(&downloads, &reservations); err != nil || downloads != 1 || reservations != 1 {
		t.Fatal("restart duplicated queue/reservation", downloads, reservations, err)
	}
	if _, err = w.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE downloads SET next_run=0`); err != nil {
		t.Fatal(err)
	}
	if _, err = w.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	job, err := w.Job(ctx, jobID)
	if err != nil || job.State != "importing" || job.ErrorCode != "" {
		t.Fatal("an unambiguous automatic Anime download did not import by itself", job.State, job.ErrorCode, err)
	}
	var confirmed int
	if err = db.QueryRow(`SELECT mapping_confirmed FROM download_tv WHERE download_id=?`, jobID).Scan(&confirmed); err != nil || confirmed != 1 {
		t.Fatal("the automatic Anime mapping was not journaled", confirmed, err)
	}
	// The journaled import (ffprobe verification, registration) runs on the
	// next claim; nothing reaches the library before it.
	var files int
	if err = db.QueryRow(`SELECT COUNT(*) FROM media_files`).Scan(&files); err != nil || files != 0 {
		t.Fatal("files appeared before the journaled import", files, err)
	}
}
