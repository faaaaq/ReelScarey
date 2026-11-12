package automation

import (
	"context"
	"testing"
)

const announceTestTracker = "iptorrents"

func addAnnounceSource(t *testing.T, f fixture, enabled, autoGrab, revision int) {
	t.Helper()
	if _, err := f.db.Exec(`INSERT INTO announce_sources(id,tracker,name,enabled,auto_grab,revision,settings,created_at,updated_at) VALUES('announce-fixture',?,?,?,?,?,'{}',1,1)`,
		announceTestTracker, "Fixture IPTorrents", enabled, autoGrab, revision); err != nil {
		t.Fatal(err)
	}
}

// addAnnounceEvent records one captured announce. state is the processing_state
// the capture path would have written: 'pending' for an auto-grab candidate,
// 'done' for capture-only history that must never be replayed.
func addAnnounceEvent(t *testing.T, f fixture, releaseName, torrentID string, sourceRevision int, state string) int64 {
	t.Helper()
	res, err := f.db.Exec(`INSERT INTO announce_events(source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,info_url,dedupe,received_at,source_revision,processing_state,outcome) VALUES('announce-fixture',?,?,'',2000000000,'100',?,'https://www.iptorrents.com/details',?,unixepoch(),?,?,'')`,
		announceTestTracker, releaseName, torrentID, releaseName, sourceRevision, state)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func announceEventState(t *testing.T, f fixture, id int64) (string, string) {
	t.Helper()
	var state, outcome string
	if err := f.db.QueryRow(`SELECT processing_state,outcome FROM announce_events WHERE id=?`, id).Scan(&state, &outcome); err != nil {
		t.Fatal(err)
	}
	return state, outcome
}

func announceDownloads(t *testing.T, f fixture) int {
	t.Helper()
	var downloads int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); err != nil {
		t.Fatal(err)
	}
	return downloads
}

// A release name that matches no monitored, missing movie is recorded honestly
// as unmatched and never queued. The scheduled search is paused to prove the
// announce processor is independent of it.
func TestAnnounceRecordsUnmatchedAndIgnoresPausedScheduledSearch(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, false, "search", 10)
	addAnnounceSource(t, f, 1, 1, 7)
	id := addAnnounceEvent(t, f, "Completely.Unrelated.Movie.2019.1080p.WEB", "111", 7, "pending")

	worked, err := f.s.ProcessAnnounces(ctx)
	if err != nil || !worked {
		t.Fatalf("the announce processor did not run: worked=%v err=%v", worked, err)
	}
	state, outcome := announceEventState(t, f, id)
	if state != "done" || outcome != "unmatched" {
		t.Fatalf("an unrelated release must be recorded as unmatched: state=%s outcome=%s", state, outcome)
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("an unmatched release was queued: %d", n)
	}
}

// Two distinct canonical movies sharing a title/year are ambiguous even when
// their quality scores would differ; the processor must refuse to guess.
func TestAnnounceRejectsAmbiguousTitlesAcrossDistinctMedia(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, true, "search", 10)
	if _, err := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,release_date) VALUES(2,'movie',2,'A Test Movie',2020,1,'2020-01-01')`); err != nil {
		t.Fatal(err)
	}
	addAnnounceSource(t, f, 1, 1, 7)
	id := addAnnounceEvent(t, f, "A.Test.Movie.2020.1080p.English.WEB", "222", 7, "pending")

	if _, err := f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome := announceEventState(t, f, id)
	if state != "done" || outcome != "ambiguous" {
		t.Fatalf("ambiguous titles across distinct media must not be grabbed: state=%s outcome=%s", state, outcome)
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("an ambiguous release was queued: %d", n)
	}
}

// A registered movie file must never be replaced by an announce grab.
func TestAnnounceNeverReplacesARegisteredMovie(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, true, "search", 10)
	if _, err := f.db.Exec(`INSERT INTO media_files(movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,'Movies/existing.mkv',10,'existing','{"width":1920,"height":1080}',1,'existing')`); err != nil {
		t.Fatal(err)
	}
	addAnnounceSource(t, f, 1, 1, 7)
	id := addAnnounceEvent(t, f, "A.Test.Movie.2020.1080p.English.WEB", "333", 7, "pending")

	if _, err := f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	_, outcome := announceEventState(t, f, id)
	if outcome == "queued" || outcome == "already_tracked" {
		t.Fatalf("a registered movie was replaced by an announce grab: %s", outcome)
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("a registered-file announce was queued: %d", n)
	}
}

// The captured source revision is fenced again inside the transfer reservation.
// An event whose captured revision no longer matches the saved source must be
// abandoned as stale rather than queued.
func TestAnnounceStaleSourceRevisionIsRefused(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, true, "search", 10)
	addAnnounceSource(t, f, 1, 1, 8) // saved revision is 8
	// The event was captured at revision 7 and is now stale.
	id := addAnnounceEvent(t, f, "A.Test.Movie.2020.1080p.English.WEB", "444", 7, "pending")

	if _, err := f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome := announceEventState(t, f, id)
	if state != "done" {
		t.Fatalf("a stale event must be completed, not left pending: state=%s outcome=%s", state, outcome)
	}
	if outcome == "queued" || outcome == "already_tracked" {
		t.Fatalf("a stale announce was queued: %s", outcome)
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("a stale announce was queued: %d", n)
	}
}

// Capture-only history (recorded while auto grab was off or the tracker did not
// match) must never be replayed by the processor.
func TestAnnounceDoesNotReplayCaptureOnlyHistory(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, true, "search", 10)
	addAnnounceSource(t, f, 1, 1, 7)
	if _, err := f.db.Exec(`UPDATE announce_events SET outcome='capture_only'`); err != nil {
		t.Fatal(err)
	}
	id := addAnnounceEvent(t, f, "A.Test.Movie.2020.1080p.English.WEB", "555", 7, "done")
	if _, err := f.db.Exec(`UPDATE announce_events SET outcome='capture_only',processed_at=unixepoch() WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}

	worked, err := f.s.ProcessAnnounces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if worked {
		t.Fatal("capture-only history must not be claimed for processing")
	}
	state, outcome := announceEventState(t, f, id)
	if state != "done" || outcome != "capture_only" {
		t.Fatalf("capture-only history changed: state=%s outcome=%s", state, outcome)
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("capture-only history was queued: %d", n)
	}
}

// With no pending or retry events the processor must do nothing rather than
// spin; a fresh database has none.
func TestAnnounceIdleWithoutPendingEvents(t *testing.T) {
	f := setup(t)
	worked, err := f.s.ProcessAnnounces(context.Background())
	if err != nil || worked {
		t.Fatalf("an idle announce processor must not claim work: worked=%v err=%v", worked, err)
	}
}
