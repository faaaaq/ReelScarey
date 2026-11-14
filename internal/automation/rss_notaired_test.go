package automation

import (
	"context"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// A feed entry for a monitored episode whose air date is in the future is
// recorded once with the explicit not_aired outcome and is then marked seen,
// so it can neither spam results nor be silently lost without a trace. A
// release the saved profile would reject anyway is recorded as rejected, the
// same honesty rule the announce path applies.
func TestRSSRecordsNotAiredForFutureEpisode(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, e := f.db.Exec(`DELETE FROM media_items;INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored) VALUES(5,'series',5,'Show',2020,1);INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',1,1,'2020-01-01');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','2026-12-01',1)`)
	if e != nil {
		t.Fatal(e)
	}
	f.i.recent = []transfer.Candidate{feedItem("Show.S01E01.1080p.English.WEB", 5)}
	if _, e = f.s.ProcessRSS(ctx); e != nil {
		t.Fatal(e)
	}
	var outcome, message string
	if e = f.db.QueryRow(`SELECT outcome,message FROM rss_results`).Scan(&outcome, &message); e != nil {
		t.Fatal(e)
	}
	if outcome != "not_aired" || message == "" {
		t.Fatalf("expected an explicit not_aired record, got %s/%s", outcome, message)
	}
	var downloads int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); e != nil || downloads != 0 {
		t.Fatalf("an unaired feed entry was grabbed: %d (%v)", downloads, e)
	}
	// A later pass must not record the entry again.
	if _, e = f.db.Exec(`UPDATE rss_sync SET next_run=0`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessRSS(ctx); e != nil {
		t.Fatal(e)
	}
	var results int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM rss_results`).Scan(&results); e != nil || results != 1 {
		t.Fatalf("the not_aired entry was recorded twice: %d (%v)", results, e)
	}
}

// A future-episode release that could never pass the saved profile is labeled
// rejected rather than promising a grab that will never happen.
func TestRSSRecordsRejectedInsteadOfNotAiredWhenProfileFails(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, e := f.db.Exec(`DELETE FROM media_items;INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored) VALUES(5,'series',5,'Show',2020,1);INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',1,1,'2020-01-01');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','2026-12-01',1)`)
	if e != nil {
		t.Fatal(e)
	}
	f.i.recent = []transfer.Candidate{feedItem("Show.S01E01.1080p.English.WEB", 0)}
	if _, e = f.s.ProcessRSS(ctx); e != nil {
		t.Fatal(e)
	}
	var outcome string
	if e = f.db.QueryRow(`SELECT outcome FROM rss_results`).Scan(&outcome); e != nil {
		t.Fatal(e)
	}
	if outcome != "rejected" {
		t.Fatalf("a profile-rejected future release must be labeled rejected, got %s", outcome)
	}
}
