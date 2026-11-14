package automation

// End-to-end announce acceptance: an announcement for monitored media travels
// the real pipeline — IRC parsing happens in internal/announce (covered there
// and by the fixture events this file inserts at the recorded boundary), the
// real automation worker claims and matches the event, the real transfer
// service fetches the metainfo from the tracker's fixed origin (served by a
// local transport) and reserves the download, and the real transfer worker
// submits it to a recording download-client double. Nothing inside the
// pipeline is replaced: only the two network boundaries (tracker HTTP and
// client protocol) are served by test doubles, which is the same injection
// style the transfer constructor itself uses.

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

const announceE2ESource = "announce-fixture"

// announceE2ETorrent is a minimal valid private-style metainfo whose parsed
// info hash is stable, so reservation identity checks see a real torrent.
const announceE2ETorrent = "d10:created by4:test4:infod6:lengthi3e4:name9:movie.mkv12:piece lengthi16384e6:pieces20:01234567890123456789ee"

type announceE2ETransport struct {
	mu       sync.Mutex
	body     []byte
	requests int
	lastURL  string
}

func (t *announceE2ETransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.requests++
	t.lastURL = r.URL.String()
	return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(t.body))}, nil
}

// announceE2EClient records the exact submissions the real transfer worker
// makes. Find starts empty, so the first pass must Add; afterwards it reports
// the recorded torrent so later passes reconcile without resubmitting.
type announceE2EClient struct {
	mu          sync.Mutex
	adds        int
	failAdd     error
	lastTag     string
	lastSave    string
	lastPayload transfer.Payload
	torrent     *transfer.Torrent
}

func (c *announceE2EClient) Test(context.Context, transfer.ClientConfig) (string, error) {
	return "e2e", nil
}

func (c *announceE2EClient) Find(_ context.Context, _ transfer.ClientConfig, hash string) (*transfer.Torrent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.torrent, nil
}

func (c *announceE2EClient) Add(_ context.Context, _ transfer.ClientConfig, tag, save string, p transfer.Payload) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.adds++
	c.lastTag, c.lastSave, c.lastPayload = tag, save, p
	if c.failAdd != nil {
		return c.failAdd
	}
	hash, e := transfer.TorrentHash(p.Torrent)
	if e != nil {
		return e
	}
	c.torrent = &transfer.Torrent{Hash: hash, Tags: tag, SavePath: save, State: "stalledUP", Progress: 1, Size: 3, Completed: 3, AmountLeft: 0}
	return nil
}

func (*announceE2EClient) Files(context.Context, transfer.ClientConfig, string) ([]transfer.TorrentFile, error) {
	return []transfer.TorrentFile{{Index: 0, Name: "movie.mkv", Size: 3, Progress: 1, Priority: 1}}, nil
}

type announceE2E struct {
	f         fixture
	cl        *announceE2EClient
	transport *announceE2ETransport
}

func newAnnounceE2E(t *testing.T) announceE2E {
	t.Helper()
	cl := &announceE2EClient{}
	f := setupFixture(t, cl)
	tr := &announceE2ETransport{body: []byte(announceE2ETorrent)}
	if !f.w.SetDirectHTTPClient("iptorrents", &http.Client{Transport: tr, Timeout: 5 * time.Second}) {
		t.Fatal("the announce HTTP transport could not be isolated")
	}
	sealed, err := f.secrets.Seal("announce:"+announceE2ESource, []byte(`{"nick":"e2e-bot","key":"e2e-passkey"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO announce_sources(id,tracker,name,enabled,auto_grab,revision,settings,created_at,updated_at) VALUES(?,'iptorrents','E2E IPT',1,1,7,?,1,1)`, announceE2ESource, sealed); err != nil {
		t.Fatal(err)
	}
	return announceE2E{f: f, cl: cl, transport: tr}
}

func (e announceE2E) announce(t *testing.T, releaseName, torrentID string) int64 {
	t.Helper()
	res, err := e.f.db.Exec(`INSERT INTO announce_events(source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,info_url,dedupe,received_at,source_revision,processing_state,outcome) VALUES(?,'iptorrents',?,'',2000000000,'100',?,'https://www.iptorrents.com/details',?,unixepoch(),7,'pending','')`,
		announceE2ESource, releaseName, torrentID, releaseName)
	if err != nil {
		t.Fatal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e announceE2E) outcome(t *testing.T, id int64) (state, outcome, message string) {
	t.Helper()
	if err := e.f.db.QueryRow(`SELECT processing_state,outcome,message FROM announce_events WHERE id=?`, id).Scan(&state, &outcome, &message); err != nil {
		t.Fatal(err)
	}
	return state, outcome, message
}

func (e announceE2E) adds(t *testing.T) int {
	t.Helper()
	e.cl.mu.Lock()
	defer e.cl.mu.Unlock()
	return e.cl.adds
}

func (e announceE2E) dispatch(t *testing.T) {
	t.Helper()
	if _, err := e.f.w.ProcessNext(context.Background()); err != nil {
		t.Fatalf("the transfer worker could not dispatch the queued announce: %v", err)
	}
}

// addE2EShow replaces the default movie with a monitored series whose episodes
// carry explicit air dates relative to the fixed automation clock.
func addE2EShow(t *testing.T, f fixture, yesterday, today, tomorrow string) {
	t.Helper()
	if _, err := f.db.Exec(`DELETE FROM media_items;
INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,release_date) VALUES(5,'series',5,'Show',2020,1,'2020-01-01');
INSERT INTO series_details(media_id) VALUES(5);
INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',3,1,'2020-01-01');
INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES
 (1,1,11,1,'One','','` + yesterday + `',1),
 (2,1,12,2,'Two','','` + today + `',1),
 (3,1,13,3,'Three','','` + tomorrow + `',1)`); err != nil {
		t.Fatal(err)
	}
}

// A monitored, missing movie announced on IRC must end as one submission to
// the configured download client, carrying the tracker-fetched torrent, the
// job tag and the reserved save path, and the durable job must report the
// downloading state honestly.
func TestAnnounceE2EMovieReachesDownloadClient(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	id := e.announce(t, "A.Test.Movie.2020.1080p.English.WEB", "700")

	if worked, err := e.f.s.ProcessAnnounces(ctx); err != nil || !worked {
		t.Fatalf("the announce worker did not run: worked=%v err=%v", worked, err)
	}
	state, outcome, message := e.outcome(t, id)
	if state != "done" || outcome != "queued" {
		t.Fatalf("the movie announce was not queued: state=%s outcome=%s message=%s", state, outcome, message)
	}
	if e.adds(t) != 0 {
		t.Fatal("the queue step must not submit anything itself")
	}
	e.dispatch(t)
	if got := e.adds(t); got != 1 {
		t.Fatalf("expected exactly one client submission, got %d", got)
	}
	e.cl.mu.Lock()
	payload, tag, save := e.cl.lastPayload, e.cl.lastTag, e.cl.lastSave
	e.cl.mu.Unlock()
	if len(payload.Torrent) == 0 || string(payload.Torrent) != announceE2ETorrent {
		t.Fatalf("the submitted payload is not the tracker torrent: %q", payload.Torrent)
	}
	if !strings.HasPrefix(tag, "reelscarey-") || !strings.Contains(save, "ReelScarey") {
		t.Fatalf("wrong submission tag/save: tag=%q save=%q", tag, save)
	}
	if !strings.Contains(e.transport.lastURL, "https://www.iptorrents.com/download.php/700/") {
		t.Fatalf("the fetch did not use the fixed tracker origin: %q", e.transport.lastURL)
	}
	if !strings.Contains(e.transport.lastURL, "torrent_pass=e2e-passkey") {
		t.Fatal("the saved passkey must travel only as the torrent_pass query parameter")
	}
	var jobState, releaseID string
	if err := e.f.db.QueryRow(`SELECT state,release_id FROM downloads`).Scan(&jobState, &releaseID); err != nil {
		t.Fatal(err)
	}
	if jobState != "downloading" {
		t.Fatalf("the durable job did not report the submission: state=%s", jobState)
	}
	if releaseID != "announce:iptorrents:announce-fixture:"+strconv.FormatInt(id, 10) {
		t.Fatalf("wrong durable release identity: %q", releaseID)
	}
	// The queued job must reconcile, not resubmit: a second worker pass finds
	// the torrent in the client and never calls Add again.
	if _, err := e.f.w.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.adds(t); got != 1 {
		t.Fatalf("a second dispatch resubmitted the torrent: %d adds", got)
	}
}

// The owner's Dexter scenario: a monitored episode scheduled to air tomorrow
// is announced today. The documented policy grabs TV episodes on or after the
// listed air date, so the announce must be deferred to that date with an
// explicit not_aired outcome — not silently consumed — and the exact same
// release must then reach the download client once the air date arrives.
func TestAnnounceE2EEarlyEpisodeReleaseIsDeferredToAirDateThenDispatched(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	base := time.Date(2026, 11, 15, 20, 30, 0, 0, time.Local) // Sunday evening
	today, tomorrow := base.Format("2006-01-02"), base.AddDate(0, 0, 1).Format("2006-01-02")
	addE2EShow(t, e.f, base.AddDate(0, 0, -1).Format("2006-01-02"), today, tomorrow)
	e.f.s.now = func() time.Time { return base }
	id := e.announce(t, "Show.S01E03.1080p.English.WEB", "703")

	if worked, err := e.f.s.ProcessAnnounces(ctx); err != nil || !worked {
		t.Fatalf("the announce worker did not run: %v %v", worked, err)
	}
	state, outcome, message := e.outcome(t, id)
	if state != "retry" || outcome != "not_aired" {
		t.Fatalf("an early release must be deferred with an explicit outcome: state=%s outcome=%s message=%s", state, outcome, message)
	}
	if !strings.Contains(message, tomorrow) {
		t.Fatalf("the deferral must name the listed air date: %q", message)
	}
	var nextRun int64
	if err := e.f.db.QueryRow(`SELECT next_run FROM announce_events WHERE id=?`, id).Scan(&nextRun); err != nil {
		t.Fatal(err)
	}
	airMidnight, err := time.ParseInLocation("2006-01-02", tomorrow, base.Location())
	if err != nil {
		t.Fatal(err)
	}
	if nextRun != airMidnight.Unix() {
		t.Fatalf("the deferral must land at the air-date local midnight: next_run=%d want=%d", nextRun, airMidnight.Unix())
	}
	if _, ok := announceCandidate(t, e.f, id); ok {
		t.Fatal("an unaired episode was reserved for download")
	}
	if e.adds(t) != 0 {
		t.Fatal("an unaired episode was submitted to the download client")
	}

	// The air date arrives: the same event is reclaimed and the ordinary match
	// now queues and submits the exact release.
	e.f.s.now = func() time.Time { return airMidnight.Add(time.Minute) }
	if worked, err := e.f.s.ProcessAnnounces(ctx); err != nil || !worked {
		t.Fatalf("the deferred announce was not reclaimed: %v %v", worked, err)
	}
	state, outcome, _ = e.outcome(t, id)
	if state != "done" || outcome != "queued" {
		t.Fatalf("the deferred announce was not queued on its air date: state=%s outcome=%s", state, outcome)
	}
	e.dispatch(t)
	if got := e.adds(t); got != 1 {
		t.Fatalf("expected exactly one submission after the air date, got %d", got)
	}
	e.cl.mu.Lock()
	payload := e.cl.lastPayload
	e.cl.mu.Unlock()
	if string(payload.Torrent) != announceE2ETorrent {
		t.Fatal("the deferred release submitted the wrong payload")
	}
	var kind string
	var episodeID int64
	if err := e.f.db.QueryRow(`SELECT kind FROM downloads`).Scan(&kind); err != nil || kind != "series" {
		t.Fatalf("the deferred grab is not tracked as a series download: %q %v", kind, err)
	}
	if err := e.f.db.QueryRow(`SELECT episode_id FROM episode_downloads`).Scan(&episodeID); err != nil || episodeID != 3 {
		t.Fatalf("the deferred grab did not reserve the announced episode: %v %v", episodeID, err)
	}
}

// Repeating the same announcement after the air date must not create a second
// download: the captured event identity is its acknowledgement.
func TestAnnounceE2EDuplicateAnnouncementAfterAirDateDoesNotResubmit(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	base := time.Date(2026, 11, 15, 20, 30, 0, 0, time.Local)
	addE2EShow(t, e.f, base.AddDate(0, 0, -1).Format("2006-01-02"), base.Format("2006-01-02"), base.AddDate(0, 0, 1).Format("2006-01-02"))
	e.f.s.now = func() time.Time { return base.AddDate(0, 0, 1) }
	e.announce(t, "Show.S01E01.1080p.English.WEB", "701")

	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	e.dispatch(t)
	if got := e.adds(t); got != 1 {
		t.Fatalf("expected one submission, got %d", got)
	}
	// A duplicate delivery of the same announce is deduplicated at capture and
	// the resolved event is never claimed again; a second pass must be a no-op.
	if worked, err := e.f.s.ProcessAnnounces(ctx); err != nil || worked {
		t.Fatalf("a second pass claimed resolved work: worked=%v err=%v", worked, err)
	}
	if _, err := e.f.w.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	if got := e.adds(t); got != 1 {
		t.Fatalf("a duplicate announcement resubmitted the release: %d adds", got)
	}
}

// The C3-0008 codec-token rule must hold for IRC-announced releases: an x265
// token after the episode label never hides the episode from the matcher.
func TestAnnounceE2Ex265ReleaseNameParsesAndDispatches(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	base := time.Date(2026, 11, 15, 20, 30, 0, 0, time.Local)
	addE2EShow(t, e.f, base.AddDate(0, 0, -1).Format("2006-01-02"), base.Format("2006-01-02"), base.AddDate(0, 0, 1).Format("2006-01-02"))
	e.f.s.now = func() time.Time { return base.AddDate(0, 0, 1) }
	id := e.announce(t, "Show.S01E02.x265.1080p.WEB-DL.DDP5.1.H.265-GROUP", "702")

	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome, _ := e.outcome(t, id)
	if state != "done" || outcome != "queued" {
		t.Fatalf("an x265 release was not parsed as its episode: state=%s outcome=%s", state, outcome)
	}
	e.dispatch(t)
	if got := e.adds(t); got != 1 {
		t.Fatalf("expected one submission, got %d", got)
	}
	var episodeID int64
	if err := e.f.db.QueryRow(`SELECT episode_id FROM episode_downloads`).Scan(&episodeID); err != nil || episodeID != 2 {
		t.Fatalf("the x265 release did not reserve episode 2: %v %v", episodeID, err)
	}
}

// A genuine mismatched episode label stays rejected: the parser must not be
// made permissive by the codec fix.
func TestAnnounceE2EWrongEpisodeIsNotGrabbed(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	base := time.Date(2026, 11, 15, 20, 30, 0, 0, time.Local)
	addE2EShow(t, e.f, base.AddDate(0, 0, -1).Format("2006-01-02"), base.Format("2006-01-02"), base.AddDate(0, 0, 1).Format("2006-01-02"))
	e.f.s.now = func() time.Time { return base.AddDate(0, 0, 1) }
	id := e.announce(t, "Show.S01E09.1080p.English.WEB", "709")

	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome, message := e.outcome(t, id)
	if state != "done" || outcome != "unmatched" {
		t.Fatalf("an unmonitored episode must stay unmatched: state=%s outcome=%s message=%s", state, outcome, message)
	}
	if e.adds(t) != 0 {
		t.Fatal("an unmonitored episode was submitted")
	}
}

// A release the saved profile rejects is recorded as rejected with no
// reservation and no submission.
func TestAnnounceE2EQualityRejectedReleaseDoesNotDispatch(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	if _, err := e.f.db.Exec(`UPDATE quality_profiles SET rejected_terms='CAM' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	id := e.announce(t, "A.Test.Movie.2020.1080p.CAM.English", "710")

	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome, message := e.outcome(t, id)
	if state != "done" || outcome != "rejected" {
		t.Fatalf("a profile-rejected announce must be recorded as rejected: state=%s outcome=%s message=%s", state, outcome, message)
	}
	if _, ok := announceCandidate(t, e.f, id); ok {
		t.Fatal("a rejected announce reached the reservation")
	}
	if e.adds(t) != 0 {
		t.Fatal("a rejected announce was submitted")
	}
}

// A client that refuses the submission must never be reported as a download:
// the durable job blocks with an error the owner can act on and no success
// outcome exists anywhere.
func TestAnnounceE2EClientRejectionIsReportedHonestly(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	e.cl.failAdd = context.DeadlineExceeded
	id := e.announce(t, "A.Test.Movie.2020.1080p.English.WEB", "711")

	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome, _ := e.outcome(t, id)
	if state != "done" || outcome != "queued" {
		t.Fatalf("the announce queued normally: state=%s outcome=%s", state, outcome)
	}
	e.dispatch(t)
	if got := e.adds(t); got != 1 {
		t.Fatalf("expected exactly one refused submission attempt, got %d", got)
	}
	var jobState, errorCode, errorMessage string
	if err := e.f.db.QueryRow(`SELECT state,error_code,error_message FROM downloads`).Scan(&jobState, &errorCode, &errorMessage); err != nil {
		t.Fatal(err)
	}
	if jobState != "blocked" || errorCode == "" || errorMessage == "" {
		t.Fatalf("a refused submission must block with a visible error: state=%s code=%q message=%q", jobState, errorCode, errorMessage)
	}
}

// Air-date boundaries use the date-only server-local comparison documented in
// docs/AUTOMATION.md: an episode whose listed air date is today is eligible
// all day, and undated or invalid episode dates authorize no provider work.
func TestAnnounceE2EAirDateBoundaryAndInvalidDates(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	base := time.Date(2026, 11, 15, 23, 59, 0, 0, time.Local)
	today := base.Format("2006-01-02")
	addE2EShow(t, e.f, base.AddDate(0, 0, -1).Format("2006-01-02"), today, base.AddDate(0, 0, 1).Format("2006-01-02"))
	if _, err := e.f.db.Exec(`UPDATE episodes SET air_date='' WHERE id=2`); err != nil {
		t.Fatal(err)
	}
	e.f.s.now = func() time.Time { return base.Add(-time.Minute) } // one minute before local midnight

	// One minute before the air date begins, tomorrow's episode is deferred…
	id := e.announce(t, "Show.S01E03.1080p.English.WEB", "730")
	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	_, outcome, _ := e.outcome(t, id)
	if outcome != "not_aired" {
		t.Fatalf("an episode airing tomorrow must be deferred before local midnight: %s", outcome)
	}
	// …an episode listed for today whose date is missing is never eligible…
	id2 := e.announce(t, "Show.S01E02.1080p.English.WEB", "731")
	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	_, outcome2, _ := e.outcome(t, id2)
	if outcome2 != "unmatched" {
		t.Fatalf("an undated episode must authorize no work: %s", outcome2)
	}
	// …and the same tomorrow-listed release is grabbed from local midnight on.
	e.f.s.now = func() time.Time { return base.Add(time.Minute) }
	if _, err := e.f.db.Exec(`UPDATE announce_events SET processing_state='pending',outcome='',processed_at=NULL,next_run=NULL WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome3, _ := e.outcome(t, id)
	if state != "done" || outcome3 != "queued" {
		t.Fatalf("the same release must queue after local midnight: state=%s outcome=%s", state, outcome3)
	}
}

// With no usable download client the announce is recorded with an honest
// attention outcome instead of a false success, and no job is created.
func TestAnnounceE2EClientUnavailableIsRecordedHonestly(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	if err := e.f.w.Remove(ctx, "qbittorrent"); err != nil {
		t.Fatal(err)
	}
	id := e.announce(t, "A.Test.Movie.2020.1080p.English.WEB", "712")

	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome, message := e.outcome(t, id)
	if outcome == "queued" || outcome == "already_tracked" {
		t.Fatalf("an announce without a client cannot queue: state=%s outcome=%s", state, outcome)
	}
	if state == "retry" {
		// A temporary missing client is retried with backoff and its reason is
		// visible; that is an honest waiting state.
		if message == "" {
			t.Fatal("a waiting announce must carry its reason")
		}
	} else if state == "done" && outcome != "needs_attention" {
		t.Fatalf("an unavailable client must wait or need attention, not finish otherwise: state=%s outcome=%s message=%s", state, outcome, message)
	}
	var downloads int
	if err := e.f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); err != nil {
		t.Fatal(err)
	}
	if downloads != 0 {
		t.Fatal("a download was created without a usable client")
	}
	if e.adds(t) != 0 {
		t.Fatal("a submission was attempted without a usable client")
	}
}

// Manual search parity: the identical release the automatic path defers stays
// visible and queueable through the manual episode search, whose owner choice
// is deliberately not blocked by the automatic air-date policy.
func TestAnnounceE2EManualSearchRemainsAvailableForUnairedEpisode(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	base := time.Date(2026, 11, 15, 20, 30, 0, 0, time.Local)
	addE2EShow(t, e.f, base.AddDate(0, 0, -1).Format("2006-01-02"), base.Format("2006-01-02"), base.AddDate(0, 0, 1).Format("2006-01-02"))
	e.f.s.now = func() time.Time { return base }
	id := e.announce(t, "Show.S01E03.1080p.English.WEB", "714")
	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	if _, outcome, _ := e.outcome(t, id); outcome != "not_aired" {
		t.Fatalf("automatic grabbing must defer the unaired episode: %s", outcome)
	}

	releases, err := e.f.w.SearchTV(ctx, 5, 1, 3, "")
	if err != nil || len(releases) == 0 {
		t.Fatalf("manual search must still surface candidates: %v %v", releases, err)
	}
	job, created, err := e.f.w.Queue(ctx, releases[0].ID)
	if err != nil || !created {
		t.Fatalf("the owner's manual choice must remain queueable: %v %v", created, err)
	}
	if job.Kind != "series" {
		t.Fatalf("the manual queue is not a series job: %+v", job)
	}
	var downloads, automatic int
	if err := e.f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&downloads); err != nil {
		t.Fatal(err)
	}
	if err := e.f.db.QueryRow(`SELECT COUNT(*) FROM downloads WHERE release_id LIKE 'announce:%'`).Scan(&automatic); err != nil {
		t.Fatal(err)
	}
	if downloads != 1 || automatic != 0 {
		t.Fatalf("only the manual choice may download here: total=%d automatic=%d", downloads, automatic)
	}
}

// A monitored item added after the worker is already running is recognized by
// the next announce pass without a restart.
func TestAnnounceE2EMonitoredItemAddedLaterIsRecognized(t *testing.T) {
	e := newAnnounceE2E(t)
	ctx := context.Background()
	configure(t, e.f.s, false, "search", 10)
	base := time.Date(2026, 11, 15, 20, 30, 0, 0, time.Local)
	addE2EShow(t, e.f, base.AddDate(0, 0, -1).Format("2006-01-02"), base.Format("2006-01-02"), base.AddDate(0, 0, 1).Format("2006-01-02"))
	e.f.s.now = func() time.Time { return base.AddDate(0, 0, 1) }

	// The episode starts unmonitored, like a title the owner has not turned on.
	if _, err := e.f.db.Exec(`UPDATE episodes SET monitored=0 WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	// First pass: nothing monitored matches this title yet.
	id := e.announce(t, "Show.S01E03.1080p.English.WEB", "713")
	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome, _ := e.outcome(t, id)
	if outcome != "unmatched" {
		t.Fatalf("an unmonitored title must be unmatched: state=%s outcome=%s", state, outcome)
	}

	// The owner monitors the episode afterwards.
	if _, err := e.f.db.Exec(`UPDATE media_items SET monitored=1 WHERE id=5`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.f.db.Exec(`UPDATE episodes SET monitored=1 WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.f.db.Exec(`UPDATE announce_events SET processing_state='pending',outcome='',processed_at=NULL,next_run=NULL WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.f.s.ProcessAnnounces(ctx); err != nil {
		t.Fatal(err)
	}
	state, outcome, _ = e.outcome(t, id)
	if state != "done" || outcome != "queued" {
		t.Fatalf("a newly monitored episode was not recognized: state=%s outcome=%s", state, outcome)
	}
	e.dispatch(t)
	if got := e.adds(t); got != 1 {
		t.Fatalf("expected one submission, got %d", got)
	}
}
