package music

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

type mapCountingProber struct {
	calls    atomic.Int32
	tags     map[string]AudioTags
	fallback AudioTags
}

func (p *mapCountingProber) ProbeAudio(_ context.Context, path string) (AudioTags, error) {
	p.calls.Add(1)
	if tags, ok := p.tags[filepath.Base(path)]; ok {
		return tags, nil
	}
	return p.fallback, nil
}

func (p *mapCountingProber) count() int { return int(p.calls.Load()) }

type blockingProber struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingProber) ProbeAudio(context.Context, string) (AudioTags, error) {
	p.once.Do(func() { close(p.entered) })
	<-p.release
	return AudioTags{Artist: "Artist", AlbumArtist: "Artist", Album: "Album", Title: "Song", Track: intPtr(1), Disc: intPtr(1), Format: "flac"}, nil
}

func writeScanFixture(t *testing.T, media, relative string) string {
	t.Helper()
	absolute := filepath.Join(media, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte("fixture audio bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(absolute, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	return absolute
}

func matchingScanTags() AudioTags {
	return AudioTags{
		Artist: "Artist", AlbumArtist: "Artist", Album: "Album", Title: "Song",
		Track: intPtr(1), Disc: intPtr(1), Format: "flac", DurationMS: 1000,
		MusicBrainzArtistID: testMBID, MusicBrainzReleaseGroupID: testRGID,
		MusicBrainzTrackID: testTrackID, MusicBrainzRecordingID: testRecordingID,
	}
}

func TestScanPreCountsProgressAndReportsLiveScanning(t *testing.T) {
	ctx := context.Background()
	s, db, media := editFixture(t)
	_ = db
	prober := &blockingProber{entered: make(chan struct{}), release: make(chan struct{})}
	s.SetProber(prober)
	for _, name := range []string{"01 Song.flac", "02 Song.flac", "03 Song.flac"} {
		writeScanFixture(t, media, "Music/Artist/Album/"+name)
	}
	scan, err := s.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-prober.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("scan never started probing")
	}
	live, err := s.Scan(ctx, scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if live.Phase != "scanning" || live.ProgressTotal != 3 || live.ProgressDone != 0 {
		t.Fatalf("live progress %+v", live)
	}
	close(prober.release)
	scan = waitForScan(t, ctx, s, scan.ID)
	if scan.State != "review" || scan.Phase != "ready" {
		t.Fatal("finished scan", scan)
	}
	if scan.ProgressTotal != 3 || scan.ProgressDone != 3 || scan.FileCount != 3 || scan.SkippedCount != 0 {
		t.Fatal("progress counters", scan)
	}
	if !strings.Contains(scan.Message, "Scanned 3 audio files") {
		t.Fatal("message", scan.Message)
	}
}

func TestScanSkipsRegisteredCarriesMatchedAndFullRescanReprobes(t *testing.T) {
	ctx := context.Background()
	s, db, media := editFixture(t)
	prober := &mapCountingProber{tags: map[string]AudioTags{"01 Song.flac": matchingScanTags()}}
	s.SetProber(prober)
	writeScanFixture(t, media, "Music/Artist/Album/01 Song.flac")

	first, err := s.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	first = waitForScan(t, ctx, s, first.ID)
	if first.State != "review" || first.MatchedCount != 1 || first.SkippedCount != 0 || prober.count() != 1 {
		t.Fatalf("first scan %+v probes=%d", first, prober.count())
	}

	// A rescan without registering must carry the matched file forward
	// without probing or matching it again, and keep it finalizable.
	second, err := s.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	second = waitForScan(t, ctx, s, second.ID)
	if second.SkippedCount != 1 || second.FileCount != 1 || second.MatchedCount != 1 || prober.count() != 1 {
		t.Fatalf("normal rescan did not carry the matched file: %+v probes=%d", second, prober.count())
	}
	page, err := s.ScanPage(ctx, second.ID, 1)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("carried page", page, err)
	}
	carried := page.Items[0]
	if carried.State != "matched" || carried.ArtistID == nil || carried.AlbumID == nil || carried.TrackID == nil || carried.RelativePath != "Music/Artist/Album/01 Song.flac" {
		t.Fatalf("carried file %+v", carried)
	}
	if _, err := s.RegisterScan(ctx, second.ID, []int64{carried.ID}); err != nil {
		t.Fatal(err)
	}

	// Once registered, a normal rescan skips the unchanged path entirely.
	third, err := s.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	third = waitForScan(t, ctx, s, third.ID)
	if third.FileCount != 0 || third.SkippedCount != 1 || prober.count() != 1 {
		t.Fatalf("registered file was reprocessed: %+v probes=%d", third, prober.count())
	}
	if !strings.Contains(third.Message, "already processed") {
		t.Fatal("skip message", third.Message)
	}

	// A full rescan explicitly re-examines even a registered file: it is
	// walked, observed and matched again, but the saved digest/tag observation
	// is reused while the bytes are unchanged.
	full, err := s.StartScanRootsFull(ctx, []string{"Music"}, true)
	if err != nil {
		t.Fatal(err)
	}
	full = waitForScan(t, ctx, s, full.ID)
	if !full.FullScan || full.SkippedCount != 0 || full.FileCount != 1 || full.MatchedCount != 1 {
		t.Fatalf("full rescan %+v probes=%d", full, prober.count())
	}
	page, err = s.ScanPage(ctx, full.ID, 1)
	if err != nil || len(page.Items) != 1 {
		t.Fatal("full page", page, err)
	}
	if _, err := s.RegisterScan(ctx, full.ID, []int64{page.Items[0].ID}); err != nil {
		t.Fatal(err)
	}
	var files int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files`).Scan(&files); err != nil || files != 1 {
		t.Fatalf("registered music_files rows %d %v", files, err)
	}
}

func TestScanReconsidersUnresolvedFilesAndChangedFiles(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	media := t.TempDir()
	prober := &mapCountingProber{fallback: AudioTags{
		Artist: "Artist", AlbumArtist: "Artist", Album: "Album", Title: "Song",
		Track: intPtr(1), Disc: intPtr(1), Format: "flac",
	}}
	service := NewService(db, media)
	service.SetProber(prober)
	writeScanFixture(t, media, "Music/Artist/Album/01 Song.flac")

	first, err := service.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	first = waitForScan(t, ctx, service, first.ID)
	if first.UnresolvedCount != 1 || first.MatchedCount != 0 {
		t.Fatalf("first unresolved scan %+v", first)
	}
	// Saving the catalog entry after the first scan must be enough for a normal
	// rescan to reconsider the unresolved path instead of skipping it.
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?,'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?,'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,created_at,updated_at) VALUES(3,2,?,?,1,1,'Song',1,1)`, testTrackID, testRecordingID); err != nil {
		t.Fatal(err)
	}
	second, err := service.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	second = waitForScan(t, ctx, service, second.ID)
	if second.MatchedCount != 1 || second.UnresolvedCount != 0 || second.SkippedCount != 0 {
		t.Fatalf("unresolved file was not reconsidered: %+v", second)
	}
}

func TestRecoverScansRequeuesInterruptedWorkflow(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	if _, err := db.Exec(`INSERT INTO music_scans(id,root,state,phase,request,work_owner,work_until,message,created_at,updated_at)
		VALUES(1,'Music','review','matching','match','stale-owner',9999999999,'Matching.',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_scans(id,root,state,phase,created_at,updated_at) VALUES(2,'Music','scanning','scanning',1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := service.RecoverScans(ctx); err != nil {
		t.Fatal(err)
	}
	scan, err := service.Scan(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Request != "match" || scan.Phase != "queued" || scan.WorkUntil != 0 || !strings.Contains(scan.Message, "interrupted by a restart") {
		t.Fatalf("recovered request %+v", scan)
	}
	var state, phase, owner string
	if err := db.QueryRow(`SELECT state,phase,work_owner FROM music_scans WHERE id=1`).Scan(&state, &phase, &owner); err != nil {
		t.Fatal(err)
	}
	if state != "review" || phase != "queued" || owner != "" {
		t.Fatalf("recovered row %s %s %s", state, phase, owner)
	}
	interrupted, err := service.Scan(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if interrupted.State != "failed" || interrupted.Phase != "failed" || !strings.Contains(interrupted.Error, "interrupted by a restart") {
		t.Fatalf("interrupted scan %+v", interrupted)
	}
	// The re-queued request is claimable and no longer has a live lease.
	claimed, owner, ok, err := service.claimWorkflow(ctx)
	if err != nil || !ok || claimed.ID != 1 || owner == "" {
		t.Fatalf("claim %+v %v %v", claimed, ok, err)
	}
	if _, _, again, err := service.claimWorkflow(ctx); err != nil || again {
		t.Fatalf("second claim of one scan succeeded: %v %v", again, err)
	}
}

func TestWorkflowRequestsValidateStateAndConsent(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := NewService(db, t.TempDir())
	if _, err := db.Exec(`INSERT INTO music_scans(id,root,state,phase,created_at,updated_at) VALUES(1,'Music','review','ready',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_scan_files(scan_id,relative_path,size_bytes,modified_at,state,error,created_at,updated_at)
		VALUES(1,'Music/a.flac',10,1,'unresolved','',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestMatchAll(ctx, 1, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("match-all accepted missing consent", err)
	}
	scan, err := service.RequestMatchAll(ctx, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Request != "match" || scan.Phase != "queued" || scan.RequestedAt == 0 || scan.ProgressTotal != 1 {
		t.Fatalf("queued match request %+v", scan)
	}
	var provider *Error
	if _, err := service.RequestMatchAll(ctx, 1, true); !errors.As(err, &provider) || provider.Code != "music_scan_state" {
		t.Fatal("second request was not rejected", err)
	}
	if _, err := service.RequestFinalize(ctx, 1, true); !errors.As(err, &provider) || provider.Code != "music_scan_state" {
		t.Fatal("finalize during a match request was not rejected", err)
	}
	if _, err := service.RequestFinalize(ctx, 1, false); !errors.Is(err, ErrInvalidInput) {
		t.Fatal("finalize accepted missing consent", err)
	}
	// Cancel clears the request so the worker cannot pick it up.
	if err := service.CancelScan(ctx, 1); err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.Scan(ctx, 1)
	if err != nil || cancelled.Request != "" || cancelled.State != "cancelled" {
		t.Fatal("cancel", cancelled, err)
	}
	if _, err := service.RequestMatchAll(ctx, 1, true); err == nil {
		t.Fatal("cancelled scan accepted a request")
	}

	if _, err := db.Exec(`UPDATE music_scans SET state='review',phase='ready' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestFinalize(ctx, 1, true); !errors.As(err, &provider) || provider.Code != "music_nothing_to_do" {
		t.Fatal("finalize without matched files was accepted", err)
	}
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?,'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?,'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,disc,number,title,created_at,updated_at) VALUES(3,2,?,1,1,'Song',1,1)`, testTrackID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_scan_files SET state='matched',artist_id=1,album_id=2,track_id=3 WHERE scan_id=1`); err != nil {
		t.Fatal(err)
	}
	finalize, err := service.RequestFinalize(ctx, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if finalize.Request != "finalize" || finalize.Phase != "queued" {
		t.Fatalf("queued finalize %+v", finalize)
	}
}

func workflowMatchFixture(t *testing.T, fingerprinter *fakeFingerprinter, saveKey bool) (*Service, *sql.DB, int64) {
	t.Helper()
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		writeDiscoveryJSON(w, acoustidResultBody(0.97, testRecordingID, "Airbag", testRGID, "OK Computer"))
	}
	service, db, scanID, _ := acoustidFixture(t, acoustidMusicBrainzHandler(t, testRecordingID), acoustid, fingerprinter, saveKey)
	return service, db, scanID
}

func TestMatchWorkflowMatchesTagsThenFingerprintsRemaining(t *testing.T) {
	ctx := context.Background()
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 284, Value: "AQADtE"}}
	service, db, scanID := workflowMatchFixture(t, fingerprinter, true)
	taggedID := insertDiscoveryFile(t, db, scanID, discoveryFile{
		path: "Music/Radiohead/OK Computer/01 Airbag.flac", artist: "Radiohead", albumArtist: "Radiohead",
		album: "OK Computer", title: "Airbag", track: intPtr(1), disc: intPtr(1), rg: testRGID,
	})
	untaggedSize, untaggedModified := writeFingerprintMedia(t, service.mediaDir, "Music/Unknown/track.flac")
	untaggedID := insertFingerprintFile(t, db, scanID, "Music/Unknown/track.flac", untaggedSize, untaggedModified)
	if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
		t.Fatal(err)
	}
	worked, err := service.ProcessWorkflow(ctx)
	if err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	scan, err := service.Scan(ctx, scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.State != "review" || scan.Phase != "ready" || scan.Request != "" || scan.WorkUntil != 0 {
		t.Fatalf("finished match scan %+v", scan)
	}
	if scan.MatchedCount != 2 || scan.UnresolvedCount != 0 {
		t.Fatalf("match counts %+v", scan)
	}
	if !strings.Contains(scan.Message, "Matched 1 files") || !strings.Contains(scan.Message, "1 more by audio fingerprint") {
		t.Fatal("match message", scan.Message)
	}
	for _, id := range []int64{taggedID, untaggedID} {
		var state string
		if err := db.QueryRow(`SELECT state FROM music_scan_files WHERE id=?`, id).Scan(&state); err != nil || state != "matched" {
			t.Fatalf("file %d state %q %v", id, state, err)
		}
	}
	var files int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files`).Scan(&files); err != nil || files != 0 {
		t.Fatalf("match-all registered files: %d %v", files, err)
	}
}

func TestMatchWorkflowWarnsWhenAcoustIDIsUnavailable(t *testing.T) {
	ctx := context.Background()
	service, db, scanID, _ := acoustidFixture(t, nil, nil, nil, false)
	relative := "Music/Unknown/track.flac"
	size, modified := writeFingerprintMedia(t, service.mediaDir, relative)
	insertFingerprintFile(t, db, scanID, relative, size, modified)
	scan, err := service.RequestMatchAll(ctx, scanID, true)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Phase != "queued" {
		t.Fatal("request not queued", scan)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	scan, err = service.Scan(ctx, scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.Phase != "ready" || scan.Request != "" || scan.UnresolvedCount != 1 {
		t.Fatalf("unavailable AcoustID scan %+v", scan)
	}
	if !strings.Contains(scan.Message, "Audio fingerprint matching was skipped") {
		t.Fatal("warning", scan.Message)
	}
}

func TestMatchWorkflowStopsSafelyOnProviderRateLimit(t *testing.T) {
	ctx := context.Background()
	fingerprinter := &fakeFingerprinter{available: true, fingerprint: Fingerprint{DurationSeconds: 284, Value: "AQADtE"}}
	acoustid := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}
	service, db, scanID, _ := acoustidFixture(t, nil, acoustid, fingerprinter, true)
	relative := "Music/Unknown/track.flac"
	size, modified := writeFingerprintMedia(t, service.mediaDir, relative)
	insertFingerprintFile(t, db, scanID, relative, size, modified)
	if _, err := service.RequestMatchAll(ctx, scanID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	scan, err := service.Scan(ctx, scanID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.State != "review" || scan.Phase != "ready" || scan.Request != "" {
		t.Fatalf("rate limit was not a safe stop: %+v", scan)
	}
	if scan.UnresolvedCount != 1 || scan.MatchedCount != 0 {
		t.Fatalf("rate limit changed counts: %+v", scan)
	}
	if !strings.Contains(scan.Message, "AcoustID is limiting requests") {
		t.Fatal("rate limit warning", scan.Message)
	}
}

func TestFinalizeWorkflowRegistersAndOrganizesInPlace(t *testing.T) {
	ctx := context.Background()
	service, db, media := editFixture(t)
	service.SetProber(&mapCountingProber{tags: map[string]AudioTags{"Song.flac": matchingScanTags()}})
	source := writeScanFixture(t, media, "Music/Artist/Album/Song.flac")
	scan, err := service.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	scan = waitForScan(t, ctx, service, scan.ID)
	if scan.MatchedCount != 1 {
		t.Fatalf("scan %+v", scan)
	}
	if _, err := service.RequestFinalize(ctx, scan.ID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	scan, err = service.Scan(ctx, scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.State != "completed" || scan.Phase != "completed" || scan.Request != "" || scan.RegisteredCount != 1 {
		t.Fatalf("finalized scan %+v", scan)
	}
	if !strings.Contains(scan.Message, "Registered 1 files") || !strings.Contains(scan.Message, "organized 1 files") {
		t.Fatal("finalize message", scan.Message)
	}
	var relative, origin string
	var downloads int
	if err := db.QueryRow(`SELECT relative_path,origin FROM music_files`).Scan(&relative, &origin); err != nil {
		t.Fatal(err)
	}
	if relative != "Music/Artist/Album/01 Song.flac" || origin != "existing" {
		t.Fatalf("registered file %q %q", relative, origin)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_downloads`).Scan(&downloads); err != nil || downloads != 0 {
		t.Fatalf("finalize fabricated downloads %d %v", downloads, err)
	}
	destination := filepath.Join(media, filepath.FromSlash(relative))
	if _, err := os.Stat(destination); err != nil {
		t.Fatal("organized destination missing", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatal("source was not moved", err)
	}
	var jobs, completed int
	if err := db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(state='completed'),0) FROM music_edit_jobs`).Scan(&jobs, &completed); err != nil || jobs != 1 || completed != 1 {
		t.Fatalf("edit jobs %d %d %v", jobs, completed, err)
	}
}

func TestFinalizeWorkflowReportsPartialFailure(t *testing.T) {
	ctx := context.Background()
	service, db, media := editFixture(t)
	service.SetProber(&mapCountingProber{tags: map[string]AudioTags{
		"Song.flac":  matchingScanTags(),
		"Song2.flac": matchingScanTags(),
	}})
	writeScanFixture(t, media, "Music/Artist/Album/Song.flac")
	missing := writeScanFixture(t, media, "Music/Artist/Album/Song2.flac")
	scan, err := service.StartScan(ctx, "Music")
	if err != nil {
		t.Fatal(err)
	}
	scan = waitForScan(t, ctx, service, scan.ID)
	if scan.MatchedCount != 2 {
		t.Fatalf("scan %+v", scan)
	}
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequestFinalize(ctx, scan.ID, true); err != nil {
		t.Fatal(err)
	}
	if worked, err := service.ProcessWorkflow(ctx); err != nil || !worked {
		t.Fatalf("workflow %v %v", worked, err)
	}
	scan, err = service.Scan(ctx, scan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if scan.State == "completed" || scan.Phase != "ready" {
		t.Fatalf("partial failure was reported as success: %+v", scan)
	}
	if !strings.Contains(scan.Message, "1 failed") || !strings.Contains(scan.Message, "manual review") {
		t.Fatal("partial failure message", scan.Message)
	}
	if scan.UnresolvedCount != 1 {
		t.Fatalf("missing file was not left unresolved: %+v", scan)
	}
	var files int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files`).Scan(&files); err != nil || files != 1 {
		t.Fatalf("registered files %d %v", files, err)
	}
}
