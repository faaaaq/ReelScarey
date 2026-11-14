package automation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// The feed check sees each entry once and feed pages rotate, so a release for
// a monitored episode discovered before its air date must be kept for a
// durable deferral and grabbed on the air date even when the feed no longer
// carries the entry — the same contract the announce deferral established.
// Before the durable deferral existed the entry was recorded once, marked
// seen, and the release was permanently lost.

const deferTestTitle = "Show.S01E01.1080p.English.WEB"

func deferTestLibrary(t *testing.T, f fixture, airDate string) {
	t.Helper()
	if _, e := f.db.Exec(`DELETE FROM media_items;INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored) VALUES(5,'series',5,'Show',2020,1);INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',1,1,'2020-01-01');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','` + airDate + `',1)`); e != nil {
		t.Fatal(e)
	}
}

// failingFetchIndexer fails the metainfo fetch with a temporary transfer
// error, the same shape a real adapter reports during an outage.
type failingFetchIndexer struct{ *indexer }

func (i *failingFetchIndexer) Fetch(context.Context, transfer.IndexerConfig, transfer.Candidate) (transfer.Payload, string, error) {
	return transfer.Payload{}, "", &transfer.Error{Code: "source_unavailable", Temporary: true, Message: "The source is temporarily unavailable."}
}

// countingClient records client submissions on top of the ordinary fixture
// client so the test can prove the exact dispatch reached the client.
type countingClient struct {
	client
	adds int
}

func (c *countingClient) Add(ctx context.Context, cfg transfer.ClientConfig, tag, save string, payload transfer.Payload) error {
	c.adds++
	return c.client.Add(ctx, cfg, tag, save, payload)
}

func TestRSSDeferredFeedReleaseSurvivesRotationAndDispatchesAfterAirDate(t *testing.T) {
	cl := &countingClient{}
	f := setupFixture(t, cl)
	ctx := context.Background()
	sunday := time.Date(2026, 11, 15, 20, 0, 0, 0, time.Local)
	monday := time.Date(2026, 11, 16, 12, 0, 0, 0, time.Local)
	f.s.now = func() time.Time { return sunday }
	deferTestLibrary(t, f, "2026-11-16")
	f.i.recent = []transfer.Candidate{feedItem(deferTestTitle, 5)}

	if _, e := f.s.ProcessRSS(ctx); e != nil {
		t.Fatal(e)
	}
	var outcome, message string
	if e := f.db.QueryRow(`SELECT outcome,message FROM rss_results`).Scan(&outcome, &message); e != nil {
		t.Fatal(e)
	}
	if outcome != "not_aired" || !strings.Contains(message, "deferred to 2026-11-16") {
		t.Fatalf("expected a deferring not_aired record, got %s/%s", outcome, message)
	}
	var state string
	var nextRun int64
	if e := f.db.QueryRow(`SELECT state,next_run FROM rss_deferrals`).Scan(&state, &nextRun); e != nil {
		t.Fatalf("the early feed release was not kept for its air date: %v", e)
	}
	if state != "deferred" {
		t.Fatalf("deferral state %s", state)
	}
	if midnight := time.Date(2026, 11, 16, 0, 0, 0, 0, time.Local).Unix(); nextRun != midnight {
		t.Fatalf("deferral must wake at the air-date local midnight, got %d want %d", nextRun, midnight)
	}
	var downloads, seen int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); e != nil || downloads != 0 {
		t.Fatalf("an unaired feed entry was grabbed: %d (%v)", downloads, e)
	}
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM rss_seen`).Scan(&seen); e != nil || seen != 1 {
		t.Fatalf("the entry must still be marked seen once: %d (%v)", seen, e)
	}

	// Monday: the feed no longer carries the entry and the application
	// restarts. The durable deferral must still grab the release.
	if e := f.db.Close(); e != nil {
		t.Fatal(e)
	}
	db, e := database.Open(ctx, f.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	reopenedSecrets, e := securestore.New(ctx, db, f.dir)
	if e != nil {
		t.Fatal(e)
	}
	f.i = &indexer{title: "A.Test.Movie.2020.1080p.English.WEB"}
	w := transfer.New(db, reopenedSecrets, f.cfg, cl, f.i)
	s := New(db, w)
	s.now = func() time.Time { return monday }
	if _, e = s.ProcessRSSDeferrals(ctx); e != nil {
		t.Fatal(e)
	}
	if e = db.QueryRow(`SELECT state,outcome FROM rss_deferrals`).Scan(&state, &outcome); e != nil {
		t.Fatal(e)
	}
	if state != "queued" || outcome != "queued" {
		t.Fatalf("the deferred release was not grabbed on its air date: %s/%s", state, outcome)
	}
	if e = db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); e != nil || downloads != 1 {
		t.Fatalf("the deferred release did not create its download: %d (%v)", downloads, e)
	}
	// The exact release reaches the real download client through the transfer
	// worker, and a repeated wake pass must not submit it again.
	if _, e = w.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	if cl.adds != 1 {
		t.Fatalf("the deferred release was submitted %d times", cl.adds)
	}
	if _, e = s.ProcessRSSDeferrals(ctx); e != nil {
		t.Fatal(e)
	}
	if cl.adds != 1 || downloads != 1 {
		t.Fatalf("a resolved deferral must never resubmit: adds=%d downloads=%d", cl.adds, downloads)
	}
}

// An air date that moves after the deferral was recorded must never cause an
// early grab: at the originally scheduled wake the episode is still unaired,
// so the deferral reschedules to the new local midnight.
func TestRSSDeferralReschedulesWhenAirDateMovesLater(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sunday := time.Date(2026, 11, 15, 20, 0, 0, 0, time.Local)
	monday := time.Date(2026, 11, 16, 0, 1, 0, 0, time.Local)
	f.s.now = func() time.Time { return sunday }
	deferTestLibrary(t, f, "2026-11-16")
	f.i.recent = []transfer.Candidate{feedItem(deferTestTitle, 5)}
	if _, e := f.s.ProcessRSS(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`UPDATE episodes SET air_date='2026-11-26' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	f.s.now = func() time.Time { return monday }
	if _, e := f.s.ProcessRSSDeferrals(ctx); e != nil {
		t.Fatal(e)
	}
	var state string
	var nextRun int64
	var message string
	if e := f.db.QueryRow(`SELECT state,next_run,message FROM rss_deferrals`).Scan(&state, &nextRun, &message); e != nil {
		t.Fatal(e)
	}
	if state != "deferred" || !strings.Contains(message, "2026-11-26") {
		t.Fatalf("the deferral must follow the moved air date: %s/%s", state, message)
	}
	if midnight := time.Date(2026, 11, 26, 0, 0, 0, 0, time.Local).Unix(); nextRun != midnight {
		t.Fatalf("rescheduled wake must be the new local midnight, got %d want %d", nextRun, midnight)
	}
	var downloads int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); e != nil || downloads != 0 {
		t.Fatalf("an episode whose air date moved later must not be grabbed early: %d (%v)", downloads, e)
	}
}

// A deferral whose episode disappears keeps an honest terminal record instead
// of waiting forever.
func TestRSSDeferralRecordsStaleOutcomeWhenEpisodeVanishes(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sunday := time.Date(2026, 11, 15, 20, 0, 0, 0, time.Local)
	monday := time.Date(2026, 11, 16, 0, 1, 0, 0, time.Local)
	f.s.now = func() time.Time { return sunday }
	deferTestLibrary(t, f, "2026-11-16")
	f.i.recent = []transfer.Candidate{feedItem(deferTestTitle, 5)}
	if _, e := f.s.ProcessRSS(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`DELETE FROM episodes WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	f.s.now = func() time.Time { return monday }
	if _, e := f.s.ProcessRSSDeferrals(ctx); e != nil {
		t.Fatal(e)
	}
	var state, outcome string
	if e := f.db.QueryRow(`SELECT state,outcome FROM rss_deferrals`).Scan(&state, &outcome); e != nil {
		t.Fatal(e)
	}
	if state != "stale" || outcome != "stale_target" {
		t.Fatalf("a vanished episode must resolve the deferral honestly: %s/%s", state, outcome)
	}
	var downloads int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); e != nil || downloads != 0 {
		t.Fatalf("no download may follow a stale deferral: %d (%v)", downloads, e)
	}
}

// A release more than 30 days before its air date is never parked for months:
// it gets the explicit terminal message with the manual/scheduled-search
// alternative, exactly like the announce path.
func TestRSSRecordsNotAiredWithoutDeferralBeyondHorizon(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sunday := time.Date(2026, 11, 15, 20, 0, 0, 0, time.Local)
	f.s.now = func() time.Time { return sunday }
	deferTestLibrary(t, f, "2026-12-26")
	f.i.recent = []transfer.Candidate{feedItem(deferTestTitle, 5)}
	if _, e := f.s.ProcessRSS(ctx); e != nil {
		t.Fatal(e)
	}
	var outcome, message string
	if e := f.db.QueryRow(`SELECT outcome,message FROM rss_results`).Scan(&outcome, &message); e != nil {
		t.Fatal(e)
	}
	if outcome != "not_aired" || !strings.Contains(message, "use a manual or scheduled search") {
		t.Fatalf("expected the terminal horizon message, got %s/%s", outcome, message)
	}
	var deferrals int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM rss_deferrals`).Scan(&deferrals); e != nil || deferrals != 0 {
		t.Fatalf("no deferral may be scheduled beyond the horizon: %d (%v)", deferrals, e)
	}
}

// A wake whose queue attempt fails transiently stays deferred with a bounded
// retry, never records a successful download, and keeps its reason visible.
func TestRSSDeferralRetriesTransientQueueFailure(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sunday := time.Date(2026, 11, 15, 20, 0, 0, 0, time.Local)
	monday := time.Date(2026, 11, 16, 0, 1, 0, 0, time.Local)
	f.s.now = func() time.Time { return sunday }
	deferTestLibrary(t, f, "2026-11-16")
	f.i.recent = []transfer.Candidate{feedItem(deferTestTitle, 5)}
	if _, e := f.s.ProcessRSS(ctx); e != nil {
		t.Fatal(e)
	}
	// The wake queue attempts the release's metainfo fetch through the same
	// fixed-origin path; a temporary source failure retries with backoff.
	w2 := transfer.New(f.db, f.secrets, f.cfg, &client{}, &failingFetchIndexer{indexer: f.i})
	s2 := New(f.db, w2)
	s2.now = func() time.Time { return monday }
	if _, e := s2.ProcessRSSDeferrals(ctx); e != nil {
		t.Fatal(e)
	}
	var state, message string
	var nextRun int64
	if e := f.db.QueryRow(`SELECT state,message,next_run FROM rss_deferrals`).Scan(&state, &message, &nextRun); e != nil {
		t.Fatal(e)
	}
	if state != "deferred" || !strings.Contains(message, "retried") {
		t.Fatalf("a transient wake failure must stay deferred with a retry: %s/%s", state, message)
	}
	if nextRun <= monday.Unix() {
		t.Fatalf("the retry must be bounded away from now: %d", nextRun)
	}
	var downloads int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); e != nil || downloads != 0 {
		t.Fatalf("a failed wake must not record a download: %d (%v)", downloads, e)
	}
	var results int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM rss_results WHERE outcome<>'not_aired'`).Scan(&results); e != nil || results != 0 {
		t.Fatalf("a retried wake must not record a terminal outcome: %d (%v)", results, e)
	}
}

// Announce-path parity for the bounded deferral: an exhausted claim count and
// a beyond-horizon air date each resolve terminally with the actionable
// message instead of parking the event forever.
func TestAnnounceDeferralBoundsResolveTerminally(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sunday := time.Date(2026, 11, 15, 20, 0, 0, 0, time.Local)
	f.s.now = func() time.Time { return sunday }
	deferTestLibrary(t, f, "2027-01-24")
	insertAnnounceEvent(t, f, "Show.S01E01.1080p.English.WEB", 0)
	if _, e := f.s.ProcessAnnounces(ctx); e != nil {
		t.Fatal(e)
	}
	var state, outcome, message string
	if e := f.db.QueryRow(`SELECT processing_state,outcome,message FROM announce_events`).Scan(&state, &outcome, &message); e != nil {
		t.Fatal(e)
	}
	if state != "done" || outcome != "not_aired" || !strings.Contains(message, "use a manual or scheduled search") {
		t.Fatalf("a beyond-horizon announce must resolve terminally: %s/%s/%s", state, outcome, message)
	}

	if _, e := f.db.Exec(`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(2,1,12,2,'Two','','2027-01-24',1)`); e != nil {
		t.Fatal(e)
	}
	insertAnnounceEvent(t, f, "Show.S01E02.1080p.English.WEB", 8)
	if _, e := f.s.ProcessAnnounces(ctx); e != nil {
		t.Fatal(e)
	}
	if e := f.db.QueryRow(`SELECT processing_state,outcome,message FROM announce_events ORDER BY id DESC LIMIT 1`).Scan(&state, &outcome, &message); e != nil {
		t.Fatal(e)
	}
	if state != "done" || outcome != "not_aired" || !strings.Contains(message, "use a manual or scheduled search") {
		t.Fatalf("an exhausted announce deferral must resolve terminally: %s/%s/%s", state, outcome, message)
	}
	var downloads int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); e != nil || downloads != 0 {
		t.Fatalf("terminal announce bounds must not download: %d (%v)", downloads, e)
	}
}

// An air date that moves after an announce was deferred must never trigger an
// early grab: at the originally scheduled wake the episode is still unaired,
// so the announce re-defers to the new local midnight instead of downloading.
func TestAnnounceDeferralFollowsAirDateChange(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	sunday := time.Date(2026, 11, 15, 20, 0, 0, 0, time.Local)
	monday := time.Date(2026, 11, 16, 0, 1, 0, 0, time.Local)
	f.s.now = func() time.Time { return sunday }
	deferTestLibrary(t, f, "2026-11-16")
	insertAnnounceEvent(t, f, deferTestTitle, 0)
	if _, e := f.s.ProcessAnnounces(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`UPDATE episodes SET air_date='2026-11-26' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	f.s.now = func() time.Time { return monday }
	if _, e := f.s.ProcessAnnounces(ctx); e != nil {
		t.Fatal(e)
	}
	var state, outcome, message string
	var nextRun int64
	if e := f.db.QueryRow(`SELECT processing_state,outcome,message,next_run FROM announce_events`).Scan(&state, &outcome, &message, &nextRun); e != nil {
		t.Fatal(e)
	}
	if state != "retry" || outcome != "not_aired" || !strings.Contains(message, "2026-11-26") {
		t.Fatalf("the announce deferral must follow the moved air date: %s/%s/%s", state, outcome, message)
	}
	if midnight := time.Date(2026, 11, 26, 0, 0, 0, 0, time.Local).Unix(); nextRun != midnight {
		t.Fatalf("the announce must re-defer to the new local midnight, got %d want %d", nextRun, midnight)
	}
	var downloads int
	if e := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); e != nil || downloads != 0 {
		t.Fatalf("a moved air date must not trigger an early grab: %d (%v)", downloads, e)
	}
}

func insertAnnounceEvent(t *testing.T, f fixture, releaseName string, attempts int) {
	t.Helper()
	sealed, err := f.secrets.Seal("announce:announce-fixture", []byte(`{"nick":"e2e-bot","key":"e2e-passkey"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, e := f.db.Exec(`INSERT INTO announce_sources(id,tracker,name,enabled,auto_grab,revision,settings,created_at,updated_at) VALUES('announce-fixture','iptorrents','Fixture IPT',1,1,7,?,1,1) ON CONFLICT(id) DO NOTHING`, sealed); e != nil {
		t.Fatal(e)
	}
	if _, e := f.db.Exec(`INSERT INTO announce_events(source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,info_url,dedupe,received_at,source_revision,processing_state,outcome,attempts) VALUES('announce-fixture','iptorrents',?,'',2000000000,'100',?,'https://www.iptorrents.com/details',?,unixepoch(),7,'pending','',?)`, releaseName, releaseName, releaseName, attempts); e != nil {
		t.Fatal(e)
	}
}
