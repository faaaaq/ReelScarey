package automation

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/library"
)

func animeMappedReviewFixture(t *testing.T) fixture {
	t.Helper()
	f := setup(t)
	for _, q := range []string{
		`DELETE FROM media_items`,
		`INSERT INTO media_items(id,kind,tmdb_id,title,category,monitored) VALUES(5,'series',5,'Canonical Anime','anime',1)`,
		`INSERT INTO series_details(media_id) VALUES(5)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,0,'Explicitly monitored specials','','2000-01-01',1,1,'loaded'),(2,5,20,1,'Season one','','2000-01-01',1,1,'loaded'),(3,5,30,2,'Season two','','2000-01-01',1,1,'loaded')`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'Special','','2000-01-01',1),(2,2,21,1,'Regular one','','2000-01-01',1),(3,3,31,1,'Regular two','','2000-01-01',1)`,
		`INSERT INTO anime_orders(media_id,revision) VALUES(5,1)`,
		`INSERT INTO anime_order_entries(media_id,episode_id,release_title,label) VALUES(5,1,'Release Source','1'),(5,2,'Release Source','2'),(5,3,'Release Source','3')`,
	} {
		if _, err := f.db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	f.i.title = "[Fansub] Release Source - 001-003 [1080p] English WEB"
	return f
}

func animeMappedReviewConsent(t *testing.T, f fixture, mode string, specials, packs bool, max int) library.AnimeSearchSettings {
	t.Helper()
	s := library.New(f.db)
	p, err := s.AnimeSearch(context.Background(), 5)
	if err != nil {
		t.Fatal(err)
	}
	p, err = s.SaveAnimeSearch(context.Background(), 5, library.AnimeSearchUpdate{Revision: p.Revision, CategoryRevision: p.CategoryRevision, NumberingRevision: p.NumberingRevision, Mode: mode, OrderRevision: p.OrderRevision, IncludeSpecials: specials, AllowPacks: packs, MaxPackEpisodes: max})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReviewMappedAnimeAutomationRequiresNewConsentAndHonorsSearchOnly(t *testing.T) {
	for _, mode := range []string{"disabled", "season", "absolute"} {
		t.Run(mode, func(t *testing.T) {
			f := animeMappedReviewFixture(t)
			if mode != "disabled" {
				animeMappedReviewConsent(t, f, mode, true, true, 3)
			}
			configure(t, f.s, true, "download", 1)
			if _, err := f.s.ProcessNext(context.Background()); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); err != nil || count != 0 {
				t.Fatal("legacy consent enabled mapped pack automation", mode, count, err)
			}
			if mode == "disabled" && f.i.calls != 0 {
				t.Fatal("saved order without consent contacted indexer", f.i.calls)
			}
		})
	}
	f := animeMappedReviewFixture(t)
	animeMappedReviewConsent(t, f, "mapped", true, true, 3)
	configure(t, f.s, true, "search", 1)
	if _, err := f.s.ProcessNext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.i.calls == 0 {
		t.Fatal("mapped search-only skipped provider")
	}
	var count int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); err != nil || count != 0 {
		t.Fatal("mapped search-only queued work", count, err)
	}
}

func TestReviewMappedAnimeAutomationQueuesExactCohortBeyondBatchAndDoesNotDuplicate(t *testing.T) {
	f := animeMappedReviewFixture(t)
	p := animeMappedReviewConsent(t, f, "mapped", true, true, 3)
	configure(t, f.s, true, "download", 1)
	ctx := context.Background()
	if _, err := f.s.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	page, err := f.w.List(ctx, 1)
	if err != nil || page.Total != 1 || len(page.Items) != 1 {
		t.Fatal("exact mapped cohort was not queued", page, err)
	}
	job := page.Items[0]
	if job.AnimeSearch == nil || job.AnimeSearch.Mode != "mapped" || job.AnimeSearch.PolicyRevision != p.Revision || job.AnimeOrder == nil || len(job.Targets) != 3 {
		t.Fatal("mapped audit/target set incomplete", job)
	}
	for n, target := range job.Targets {
		if target.EpisodeID != int64(n+1) || target.Season != n || target.Number != 1 {
			t.Fatal("mapped cohort canonical IDs changed", job)
		}
	}
	if err = f.s.Request(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	var jobs, reservations, mappings int
	if err = f.db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads),(SELECT COUNT(*) FROM episode_downloads),(SELECT COUNT(*) FROM download_mappings)`).Scan(&jobs, &reservations, &mappings); err != nil || jobs != 1 || reservations != 3 || mappings != 0 {
		t.Fatal("mapped auto dedup or mandatory mapping violated", jobs, reservations, mappings, err)
	}
}

func TestReviewMappedAnimeAutomationRefusesUnknownPartialSupersetAndUnconsentedPacks(t *testing.T) {
	for _, tc := range []struct {
		name, title     string
		specials, packs bool
		max             int
	}{
		{"implicit complete", "Release Source Complete [1080p] English WEB", true, true, 3},
		{"partial", "Release Source - 001-002 [1080p] English WEB", true, true, 3},
		{"superset", "Release Source - 001-004 [1080p] English WEB", true, true, 3},
		{"wrong source", "Other Source - 001-003 [1080p] English WEB", true, true, 3},
		{"wrong typed family", "Release Source - SP001-SP003 [1080p] English WEB", true, true, 3},
		{"unconsented specials", "Release Source - 001-003 [1080p] English WEB", false, true, 3},
		{"unconsented pack", "Release Source - 001-003 [1080p] English WEB", true, false, 3},
		{"pack exceeds bound", "Release Source - 001-003 [1080p] English WEB", true, true, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := animeMappedReviewFixture(t)
			f.i.title = tc.title
			animeMappedReviewConsent(t, f, "mapped", tc.specials, tc.packs, tc.max)
			configure(t, f.s, true, "download", 1)
			if _, err := f.s.ProcessNext(context.Background()); err != nil {
				t.Fatal(err)
			}
			var count int
			if err := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); err != nil || count != 0 {
				t.Fatal("ambiguous or unconsented mapped cohort queued", tc.name, count, err)
			}
		})
	}
}

func TestReviewMappedAnimeAutomationFencesEveryMemberDuringMetainfo(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, fixture)
	}{
		{"disable policy", func(t *testing.T, f fixture) { animeMappedReviewConsent(t, f, "disabled", false, false, 20) }},
		{"order revision", func(t *testing.T, f fixture) {
			if _, err := f.db.Exec(`UPDATE anime_orders SET revision=revision+1 WHERE media_id=5`); err != nil {
				t.Fatal(err)
			}
		}},
		{"nonseed episode unmonitored", func(t *testing.T, f fixture) {
			if _, err := f.db.Exec(`UPDATE episodes SET monitored=0 WHERE id=3`); err != nil {
				t.Fatal(err)
			}
		}},
		{"nonseed season unmonitored", func(t *testing.T, f fixture) {
			if _, err := f.db.Exec(`UPDATE seasons SET monitored=0 WHERE id=3`); err != nil {
				t.Fatal(err)
			}
		}},
		{"parent unmonitored", func(t *testing.T, f fixture) {
			if _, err := f.db.Exec(`UPDATE media_items SET monitored=0 WHERE id=5`); err != nil {
				t.Fatal(err)
			}
		}},
		{"nonseed future date", func(t *testing.T, f fixture) {
			if _, err := f.db.Exec(`UPDATE episodes SET air_date='9999-01-01' WHERE id=3`); err != nil {
				t.Fatal(err)
			}
		}},
		{"nonseed invalid date", func(t *testing.T, f fixture) {
			if _, err := f.db.Exec(`UPDATE episodes SET air_date='2020-02-30' WHERE id=3`); err != nil {
				t.Fatal(err)
			}
		}},
		{"nonseed metadata unloaded", func(t *testing.T, f fixture) {
			if _, err := f.db.Exec(`UPDATE seasons SET loaded_at='' WHERE id=3`); err != nil {
				t.Fatal(err)
			}
		}},
		{"profile revision", func(t *testing.T, f fixture) {
			if _, err := f.db.Exec(`UPDATE quality_profiles SET revision=revision+1 WHERE id=1`); err != nil {
				t.Fatal(err)
			}
		}},
		{"schedule paused", func(t *testing.T, f fixture) { configure(t, f.s, false, "download", 1) }},
		{"lease lost", func(t *testing.T, f fixture) {
			if _, err := f.db.Exec(`UPDATE search_schedule SET lease_owner='another-worker' WHERE id=1`); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := animeMappedReviewFixture(t)
			animeMappedReviewConsent(t, f, "mapped", true, true, 3)
			configure(t, f.s, true, "download", 1)
			fetches := 0
			f.i.onFetch = func() { fetches++; tc.change(t, f) }
			_, _ = f.s.ProcessNext(context.Background())
			if fetches == 0 {
				t.Fatal("test did not reach metainfo fence")
			}
			for _, table := range []string{"downloads", "episode_downloads", "download_targets"} {
				var count int
				if err := f.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
					t.Fatal("changed mapped member left partial reservation", tc.name, table, count, err)
				}
			}
		})
	}
}
