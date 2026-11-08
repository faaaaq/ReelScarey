package automation

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

func feedItem(title string, seeds int) transfer.Candidate {
	return transfer.Candidate{
		Release: transfer.Release{Title: title, Size: 2 << 30, Seeders: &seeds},
		Guid:    title,
	}
}

func TestRSSDefaultsToEnabledFifteenMinuteChecks(t *testing.T) {
	f := setup(t)
	v, e := f.s.GetRSS(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	if !v.Enabled || v.Interval != 15 {
		t.Fatalf("release detection must be enabled every 15 minutes by default: %+v", v)
	}
}

func TestRSSGrabsMonitoredMissingMovieEvenWhenScheduledSearchIsPaused(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.i.recent = []transfer.Candidate{feedItem("A.Test.Movie.2020.1080p.English.WEB", 5)}
	configure(t, f.s, false, "search", 10)

	worked, err := f.s.ProcessRSS(ctx)
	if err != nil || !worked {
		t.Fatal(worked, err)
	}
	var downloads int
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); err != nil || downloads != 1 {
		t.Fatalf("expected one RSS grab, got %d (%v)", downloads, err)
	}
	var outcome, release string
	if err = f.db.QueryRow(`SELECT outcome,release_title FROM rss_results`).Scan(&outcome, &release); err != nil {
		t.Fatal(err)
	}
	if outcome != "queued" || release != "A.Test.Movie.2020.1080p.English.WEB" {
		t.Fatalf("unexpected feed result: %s %s", outcome, release)
	}
}

func TestRSSProcessesEachFeedEntryOnce(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.i.recent = []transfer.Candidate{feedItem("A.Test.Movie.2020.1080p.English.WEB", 5)}
	if _, err := f.s.ProcessRSS(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`UPDATE rss_sync SET next_run=0`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ProcessRSS(ctx); err != nil {
		t.Fatal(err)
	}
	var downloads, seen int
	if err := f.db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads),(SELECT COUNT(*) FROM rss_seen)`).Scan(&downloads, &seen); err != nil {
		t.Fatal(err)
	}
	if downloads != 1 || seen != 1 {
		t.Fatalf("a feed entry was grabbed or processed twice: downloads=%d seen=%d", downloads, seen)
	}
}

func TestRSSNeverReplacesARegisteredMovie(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, err := f.db.Exec(`INSERT INTO media_files(movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,'Movies/existing.mkv',10,'existing','{"width":1920,"height":1080}',1,'existing')`); err != nil {
		t.Fatal(err)
	}
	f.i.recent = []transfer.Candidate{feedItem("A.Test.Movie.2020.1080p.English.WEB", 5)}
	if _, err := f.s.ProcessRSS(ctx); err != nil {
		t.Fatal(err)
	}
	var downloads, results int
	if err := f.db.QueryRow(`SELECT (SELECT COUNT(*) FROM downloads),(SELECT COUNT(*) FROM rss_results)`).Scan(&downloads, &results); err != nil {
		t.Fatal(err)
	}
	if downloads != 0 || results != 0 {
		t.Fatalf("feed checking replaced or reported a registered file: downloads=%d results=%d", downloads, results)
	}
}

func TestRSSRecordsAProfileRejection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.i.recent = []transfer.Candidate{feedItem("A.Test.Movie.2020.1080p.English.WEB", 0)}
	if _, err := f.s.ProcessRSS(ctx); err != nil {
		t.Fatal(err)
	}
	var outcome string
	if err := f.db.QueryRow(`SELECT outcome FROM rss_results`).Scan(&outcome); err != nil {
		t.Fatal(err)
	}
	if outcome != "rejected" {
		t.Fatalf("expected a profile rejection record, got %s", outcome)
	}
	var downloads int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads)
	if downloads != 0 {
		t.Fatal("a rejected feed entry was queued")
	}
}

func TestRSSGrabsAMonitoredMissingTVEpisode(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, e := f.db.Exec(`DELETE FROM media_items;INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored) VALUES(5,'series',5,'Show',2020,1);INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',1,1,'2020-01-01');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','2020-01-01',1)`)
	if e != nil {
		t.Fatal(e)
	}
	f.i.recent = []transfer.Candidate{feedItem("Show.S01E01.1080p.English.WEB", 5)}
	if _, e = f.s.ProcessRSS(ctx); e != nil {
		t.Fatal(e)
	}
	jobs, e := f.w.List(ctx, 1)
	if e != nil || len(jobs.Items) != 1 || jobs.Items[0].Kind != "series" || len(jobs.Items[0].Targets) != 1 {
		t.Fatalf("RSS did not reserve the exact episode: %+v (%v)", jobs, e)
	}
	var episodeID int64
	if e = f.db.QueryRow(`SELECT episode_id FROM episode_downloads`).Scan(&episodeID); e != nil || episodeID != 1 {
		t.Fatalf("RSS reserved the wrong episode: %d (%v)", episodeID, e)
	}
}
