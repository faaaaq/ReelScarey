package automation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

// This file adds durable announce end-to-end coverage on top of the unit-style
// cases in announce_test.go. Every case drives the real Service.ProcessAnnounces
// worker (claim -> match -> quality -> QueueAnnounceAfter reservation) and
// asserts the durable transfer artifacts it leaves behind, not just the recorded
// outcome string:
//
//   - announce_events.processing_state/outcome/message
//   - the release_candidates row QueueAnnounceAfter persists before any metainfo
//     fetch, including its sealed payload that records the exact selected target
//   - the absence of downloads/episode_downloads (nothing was grabbed)
//
// A note on the successful-grab seam: for an announce candidate transfer builds
// its metainfo with fetchAnnounce, which uses the unexported
// s.adapters["iptorrents"].(*Direct) HTTP client and a fixed
// https://www.iptorrents.com origin. That client cannot be replaced from the
// automation package (only internal/transfer tests can reach Direct.HTTP), so a
// fully queued download is not reachable from here without editing production
// code. These tests therefore prove the exact selection and reservation are
// reached, and that the failure is bounded and honest, rather than fabricating a
// download. See the task report for the testability defect this exposes.

// announceCandidate returns the durable transfer reservation the announce
// processor created for one event, if it reached transfer at all. The candidate
// is the crash-recovery evidence QueueAnnounceAfter persists before it fetches
// any metainfo, and its sealed payload records the exact target selected.
func announceCandidate(t *testing.T, f fixture, eventID int64) (transfer.Candidate, bool) {
	t.Helper()
	id := "announce:iptorrents:announce-fixture:" + strconv.FormatInt(eventID, 10)
	var sealed []byte
	err := f.db.QueryRow(`SELECT payload FROM release_candidates WHERE id=?`, id).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return transfer.Candidate{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.secrets.Open("release", sealed)
	if err != nil {
		t.Fatal(err)
	}
	var v transfer.Candidate
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v, true
}

// addAnnounceTVFixture replaces the default movie with a single monitored
// missing series/season/episode, mirroring the RSS TV fixture.
func addAnnounceTVFixture(t *testing.T, f fixture) {
	t.Helper()
	if _, err := f.db.Exec(`DELETE FROM media_items;INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored) VALUES(5,'series',5,'Show',2020,1);INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',1,1,'2020-01-01');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,11,1,'One','','2020-01-01',1)`); err != nil {
		t.Fatal(err)
	}
}

func setAnnounceMinSeeders(t *testing.T, f fixture, n int) {
	t.Helper()
	if _, err := f.db.Exec(`UPDATE quality_profiles SET min_seeders=? WHERE id=1`, n); err != nil {
		t.Fatal(err)
	}
}

// A monitored, missing movie whose release is announced while the scheduled
// search is paused must still be selected and handed to transfer with the exact
// movie target. No other download may be created.
func TestAnnounceIntegrationSelectsExactMissingMovieTarget(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, false, "search", 10) // scheduled searches are paused
	setAnnounceMinSeeders(t, f, 0)
	addAnnounceSource(t, f, 1, 1, 7)
	id := addAnnounceEvent(t, f, "A.Test.Movie.2020.1080p.English.WEB", "900", 7, "pending")

	worked, err := f.s.ProcessAnnounces(ctx)
	if err != nil || !worked {
		t.Fatalf("the announce processor did not run: worked=%v err=%v", worked, err)
	}
	state, outcome := announceEventState(t, f, id)
	if state != "done" {
		t.Fatalf("a processed event must be resolved: state=%s outcome=%s", state, outcome)
	}
	switch outcome {
	case "unmatched", "ambiguous", "rejected", "stale_source", "stale_policy":
		t.Fatalf("the exact missing movie was not selected for transfer: %s", outcome)
	}
	v, ok := announceCandidate(t, f, id)
	if !ok {
		t.Fatalf("the announce never reached the transfer reservation: outcome=%s", outcome)
	}
	if v.Announce == nil || v.Announce.EventID != id || v.Announce.TorrentID != "900" || v.Announce.SourceRevision != 7 || v.Announce.SourceID != "announce-fixture" {
		t.Fatalf("wrong announce identity carried to transfer: %+v", v.Announce)
	}
	if v.MovieID != 1 || v.Kind == "series" || len(v.Targets) != 0 {
		t.Fatalf("wrong movie target reserved: %+v", v)
	}
	if v.Title != "A.Test.Movie.2020.1080p.English.WEB" {
		t.Fatalf("wrong release title reserved: %q", v.Title)
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("a download was created without a reachable announce source: %d", n)
	}
}

// A monitored, missing ordinary episode must be selected with its exact episode
// target and reserved as a series candidate, never a movie.
func TestAnnounceIntegrationSelectsExactMissingTVEpisode(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	addAnnounceTVFixture(t, f)
	configure(t, f.s, false, "search", 10)
	setAnnounceMinSeeders(t, f, 0)
	addAnnounceSource(t, f, 1, 1, 7)
	id := addAnnounceEvent(t, f, "Show.S01E01.1080p.English.WEB", "901", 7, "pending")

	worked, err := f.s.ProcessAnnounces(ctx)
	if err != nil || !worked {
		t.Fatalf("the announce processor did not run: worked=%v err=%v", worked, err)
	}
	state, outcome := announceEventState(t, f, id)
	if state != "done" {
		t.Fatalf("a processed event must be resolved: state=%s outcome=%s", state, outcome)
	}
	switch outcome {
	case "unmatched", "ambiguous", "rejected", "stale_source", "stale_policy":
		t.Fatalf("the exact missing episode was not selected for transfer: %s", outcome)
	}
	v, ok := announceCandidate(t, f, id)
	if !ok {
		t.Fatalf("the TV announce never reached the transfer reservation: outcome=%s", outcome)
	}
	if v.Kind != "series" || v.MovieID != 5 {
		t.Fatalf("wrong series reserved: %+v", v)
	}
	if len(v.Targets) != 1 || v.Targets[0].EpisodeID != 1 || v.Targets[0].Season != 1 || v.Targets[0].Number != 1 {
		t.Fatalf("wrong episode target reserved: %+v", v.Targets)
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("a download was created without a reachable announce source: %d", n)
	}
	var episodes int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM episode_downloads`).Scan(&episodes); err != nil {
		t.Fatal(err)
	}
	if episodes != 0 {
		t.Fatalf("an episode download was reserved without a reachable announce source: %d", episodes)
	}
}

// A registered episode file must never be replaced by an announce grab.
func TestAnnounceIntegrationNeverReplacesRegisteredEpisode(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	addAnnounceTVFixture(t, f)
	configure(t, f.s, true, "search", 10)
	setAnnounceMinSeeders(t, f, 0)
	// A registered episode is a media_files row (series-scoped) linked through
	// episode_files, which is exactly what the guard and hasFile check.
	res, err := f.db.Exec(`INSERT INTO media_files(movie_id,series_id,download_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(NULL,5,NULL,'Shows/existing.mkv',10,'existing','{"width":1920,"height":1080}',1,'existing')`)
	if err != nil {
		t.Fatal(err)
	}
	fileID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,?)`, fileID); err != nil {
		t.Fatal(err)
	}
	addAnnounceSource(t, f, 1, 1, 7)
	id := addAnnounceEvent(t, f, "Show.S01E01.1080p.English.WEB", "902", 7, "pending")

	if _, err := f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	_, outcome := announceEventState(t, f, id)
	if outcome == "queued" || outcome == "already_tracked" {
		t.Fatalf("a registered episode was replaced by an announce grab: %s", outcome)
	}
	if _, ok := announceCandidate(t, f, id); ok {
		t.Fatal("a registered episode reached the transfer reservation")
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("a registered-file announce was queued: %d", n)
	}
	var episodes int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM episode_downloads`).Scan(&episodes); err != nil {
		t.Fatal(err)
	}
	if episodes != 0 {
		t.Fatalf("a registered episode was reserved: %d", episodes)
	}
}

// A duplicate delivery of one event must be processed exactly once: a second
// pass finds nothing pending and leaves the resolved event and its single
// reservation untouched.
func TestAnnounceIntegrationProcessesEachEventExactlyOnce(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, false, "search", 10)
	setAnnounceMinSeeders(t, f, 0)
	addAnnounceSource(t, f, 1, 1, 7)
	id := addAnnounceEvent(t, f, "A.Test.Movie.2020.1080p.English.WEB", "903", 7, "pending")

	if worked, err := f.s.ProcessAnnounces(ctx); err != nil || !worked {
		t.Fatalf("first pass did not run: %v %v", worked, err)
	}
	state, outcome := announceEventState(t, f, id)
	if state != "done" {
		t.Fatalf("event not resolved by the first pass: %s %s", state, outcome)
	}
	var candidates int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM release_candidates`).Scan(&candidates); err != nil {
		t.Fatal(err)
	}
	if candidates != 1 {
		t.Fatalf("expected exactly one reservation after the first pass: %d", candidates)
	}

	worked, err := f.s.ProcessAnnounces(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if worked {
		t.Fatal("a resolved event was claimed again on a duplicate pass")
	}
	state2, outcome2 := announceEventState(t, f, id)
	if state2 != "done" || outcome2 != outcome {
		t.Fatalf("a duplicate pass changed the resolved event: %s/%s -> %s/%s", state, outcome, state2, outcome2)
	}
	var candidates2 int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM release_candidates`).Scan(&candidates2); err != nil {
		t.Fatal(err)
	}
	if candidates2 != 1 {
		t.Fatalf("a duplicate pass created an extra reservation: %d", candidates2)
	}
}

// An event left 'processing' by a crashed worker whose lease has expired must be
// reclaimed and resolved by the next pass, not stuck forever.
func TestAnnounceIntegrationReclaimsExpiredProcessingLease(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, false, "search", 10)
	setAnnounceMinSeeders(t, f, 0)
	addAnnounceSource(t, f, 1, 1, 7)
	id := addAnnounceEvent(t, f, "A.Test.Movie.2020.1080p.English.WEB", "904", 7, "pending")
	// Simulate a worker that claimed the event and died before completing it.
	if _, err := f.db.Exec(`UPDATE announce_events SET processing_state='processing',lease_owner='dead-worker',lease_until=1,attempts=1,outcome='' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}

	worked, err := f.s.ProcessAnnounces(ctx)
	if err != nil || !worked {
		t.Fatalf("the recovery pass did not run: worked=%v err=%v", worked, err)
	}
	state, outcome := announceEventState(t, f, id)
	if state != "done" {
		t.Fatalf("a crashed claim was not recovered: state=%s outcome=%s", state, outcome)
	}
	switch outcome {
	case "unmatched", "ambiguous", "rejected", "stale_source", "stale_policy":
		t.Fatalf("the recovered event was not selected for transfer: %s", outcome)
	}
	v, ok := announceCandidate(t, f, id)
	if !ok || v.MovieID != 1 {
		t.Fatalf("the recovered event did not reserve the exact movie target: %+v (%v)", v, ok)
	}
	var attempts int
	if err := f.db.QueryRow(`SELECT attempts FROM announce_events WHERE id=?`, id).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("the reclaimed event must be re-claimed exactly once: attempts=%d", attempts)
	}
}

// Unknown seeder counts are the norm for an announce, so an announce satisfies
// the profile's minimum seeder count instead of being rejected by it. The rule
// still applies to scheduled searches, which always report a count; that
// known-low rejection is exercised at the quality layer. Here we prove that a
// profile with a positive minimum still lets an announce reach transfer, just
// like a zero minimum does.
func TestAnnounceIntegrationUnknownSeedersSatisfyMinimum(t *testing.T) {
	ctx := context.Background()

	positive := setup(t)
	configure(t, positive.s, false, "search", 10)
	setAnnounceMinSeeders(t, positive, 5)
	addAnnounceSource(t, positive, 1, 1, 7)
	id := addAnnounceEvent(t, positive, "A.Test.Movie.2020.1080p.English.WEB", "905", 7, "pending")
	if _, err := positive.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome := announceEventState(t, positive, id)
	if state != "done" || outcome == "rejected" {
		t.Fatalf("an announce must satisfy a positive minimum seeder count: %s/%s", state, outcome)
	}
	v, ok := announceCandidate(t, positive, id)
	if !ok || v.MovieID != 1 {
		t.Fatalf("an accepted announce did not reach the transfer reservation: %s (%+v)", outcome, v)
	}

	zeroMinimum := setup(t)
	configure(t, zeroMinimum.s, false, "search", 10)
	setAnnounceMinSeeders(t, zeroMinimum, 0)
	addAnnounceSource(t, zeroMinimum, 1, 1, 7)
	id2 := addAnnounceEvent(t, zeroMinimum, "A.Test.Movie.2020.1080p.English.WEB", "906", 7, "pending")
	if _, err := zeroMinimum.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	_, outcome2 := announceEventState(t, zeroMinimum, id2)
	if outcome2 == "rejected" {
		t.Fatalf("a zero minimum must accept an unknown seed count: %s", outcome2)
	}
	if _, ok := announceCandidate(t, zeroMinimum, id2); !ok {
		t.Fatalf("an accepted release did not reach the transfer reservation: %s", outcome2)
	}
}

// Season packs, episode ranges, specials (season zero) and unrelated releases
// must never be grabbed, even when a monitored missing episode exists.
func TestAnnounceIntegrationIgnoresPacksRangesSpecialsAndUnrelatedReleases(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	addAnnounceTVFixture(t, f)
	configure(t, f.s, false, "search", 10)
	setAnnounceMinSeeders(t, f, 0)
	addAnnounceSource(t, f, 1, 1, 7)

	cases := []string{
		"Show.S01.1080p.English.WEB",        // season pack
		"Show.S01E01-E03.1080p.English.WEB", // episode range
		"Show.S00E01.1080p.English.WEB",     // special (season zero)
		"Completely.Unrelated.Movie.2019.1080p.WEB",
	}
	for i, name := range cases {
		id := addAnnounceEvent(t, f, name, strconv.Itoa(910+i), 7, "pending")
		worked, err := f.s.ProcessAnnounces(ctx)
		if err != nil || !worked {
			t.Fatalf("%q: processor did not run: %v %v", name, worked, err)
		}
		state, outcome := announceEventState(t, f, id)
		if outcome != "unmatched" {
			t.Fatalf("%q must not be grabbed: state=%s outcome=%s", name, state, outcome)
		}
		if _, ok := announceCandidate(t, f, id); ok {
			t.Fatalf("%q reached the transfer reservation", name)
		}
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("a pack/range/special/unrelated release was queued: %d", n)
	}
	var episodes int
	if err := f.db.QueryRow(`SELECT COUNT(*) FROM episode_downloads`).Scan(&episodes); err != nil {
		t.Fatal(err)
	}
	if episodes != 0 {
		t.Fatalf("a pack/range/special/unrelated release reserved an episode: %d", episodes)
	}
}

// A source whose auto grab was turned off (or whose revision moved) must not be
// queued; the processor records the staleness honestly and leaves no reservation.
func TestAnnounceIntegrationStaleSourceIsNotQueued(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	configure(t, f.s, false, "search", 10)
	setAnnounceMinSeeders(t, f, 0)
	addAnnounceSource(t, f, 1, 0, 7) // enabled but auto grab off
	id := addAnnounceEvent(t, f, "A.Test.Movie.2020.1080p.English.WEB", "920", 7, "pending")

	if _, err := f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome := announceEventState(t, f, id)
	if state != "done" {
		t.Fatalf("a stale-source event must be completed: state=%s outcome=%s", state, outcome)
	}
	if outcome == "queued" || outcome == "already_tracked" {
		t.Fatalf("a stale announce source was queued: %s", outcome)
	}
	if _, ok := announceCandidate(t, f, id); ok {
		t.Fatal("a stale announce source reached the transfer reservation")
	}
	if n := announceDownloads(t, f); n != 0 {
		t.Fatalf("a stale announce source was queued: %d", n)
	}
}
