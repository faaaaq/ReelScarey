package music

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

// The conversion queue tests reuse the hermetic fake ffmpeg (the test binary
// itself, see transcode_cap_test.go TestMain) and the fake prober helpers
// from convert_test.go: a conversion streams the source bytes to the staged
// output, so published variant files are verifiable byte-for-byte.

var conversionSourceBytes = []byte("audio-bytes-for-the-conversion-queue")

// conversionFixture registers one artist/album/track with a primary music file
// (wma, so conversion is always needed) in a disposable media root, and wires
// a disposable downloads root for the queue's stage directories.
func conversionFixture(t *testing.T) (*Service, *sql.DB, string, string, int64) {
	t.Helper()
	ctx := context.Background()
	media := t.TempDir()
	downloads := t.TempDir()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,?,'Artist',1,'missing',1,1)`, testMBID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,?,'Album',1,'missing',1,1)`, testRGID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_tracks(id,album_id,musicbrainz_id,disc,number,title,created_at,updated_at) VALUES(3,2,?,1,1,'Song',1,1)`, testTrackID); err != nil {
		t.Fatal(err)
	}
	relative := "Music/Artist/Album/01 Song.wma"
	path := filepath.Join(media, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, conversionSourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(1,2,3,'Music',?,?,?,'wma',192,44100,2,1000,'{}',1,'existing','01 Song.wma')`,
		relative, len(conversionSourceBytes), sha256Hex(conversionSourceBytes)); err != nil {
		t.Fatal(err)
	}
	var fileID int64
	if err := db.QueryRow(`SELECT id FROM music_files`).Scan(&fileID); err != nil {
		t.Fatal(err)
	}
	s := NewService(db, media)
	s.SetDownloadsDir(downloads)
	return s, db, media, downloads, fileID
}

// conversionProber answers source probes with wma tags and staged ".part"
// probes with opus tags, so the opus profiles validate their output.
func conversionProber() *fakeConvertProber {
	return &fakeConvertProber{
		source: sourceTags("wma", 1000, 192),
		output: sourceTags("opus", 1000, 128),
	}
}

// shortConversionBackoff shrinks the retry backoff so failed attempts can be
// retried quickly by the test loop while still proving the durable
// work_until backoff is written into the row.
func shortConversionBackoff(t *testing.T) {
	t.Helper()
	previous := conversionRetryBackoff
	conversionRetryBackoff = time.Second
	t.Cleanup(func() { conversionRetryBackoff = previous })
}

// runConversionUntil processes the queue until the job reaches the wanted
// state, failing the test on worker errors or timeout.
func runConversionUntil(t *testing.T, s *Service, id int64, want string) ConversionJob {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(20 * time.Second)
	for {
		worked, err := s.processConversion(ctx)
		if err != nil {
			t.Fatal(err)
		}
		job, err := s.loadConversion(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if job.State == want {
			return job
		}
		if !worked && job.State != "queued" {
			t.Fatalf("worker stalled with job in state %q, want %q", job.State, want)
		}
		if time.Now().After(deadline) {
			t.Fatalf("conversion never reached %q: %+v", want, job)
		}
		if !worked {
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func conversionStageDir(downloads string, jobID int64) string {
	return filepath.Join(downloads, "music-conversions", strconv.FormatInt(jobID, 10))
}

func variantRow(t *testing.T, db *sql.DB, label string) (int64, string, string, string) {
	t.Helper()
	var id int64
	var relative, role, profile string
	err := db.QueryRow(`SELECT id,relative_path,variant_role,variant_profile FROM music_files WHERE variant_profile=?`, label).
		Scan(&id, &relative, &role, &profile)
	if err != nil {
		t.Fatal(err)
	}
	return id, relative, role, profile
}

// --- Enqueue validation and dedup -------------------------------------------

func TestEnqueueConversionValidatesInput(t *testing.T) {
	s, _, _, _, fileID := conversionFixture(t)
	ctx := context.Background()
	if _, err := s.EnqueueConversion(ctx, "bogus", fileID, "opus-high", 5); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid kind: %v", err)
	}
	if _, err := s.EnqueueConversion(ctx, "export", 0, "opus-high", 5); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid file id: %v", err)
	}
	if _, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid priority: %v", err)
	}
	if _, err := s.EnqueueConversion(ctx, "export", fileID, "no-such-profile", 5); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid profile: %v", err)
	}
}

func TestEnqueueConversionDedupsActiveJobs(t *testing.T) {
	s, db, _, _, fileID := conversionFixture(t)
	ctx := context.Background()
	first, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil || again != first {
		t.Fatalf("duplicate enqueue returned %d, want the existing job %d (err %v)", again, first, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_conversion_jobs`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("active jobs duplicated: %d rows (err %v)", count, err)
	}
	// A different profile is different work.
	other, err := s.EnqueueConversion(ctx, "export", fileID, "mp3-compatible", 3)
	if err != nil || other == first {
		t.Fatalf("different profile must enqueue new work: %d vs %d (err %v)", other, first, err)
	}
	// A cancelled job is no longer active, so the same request queues again.
	if err := s.CancelConversion(ctx, first); err != nil {
		t.Fatal(err)
	}
	retry, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil || retry == first {
		t.Fatalf("cancelled job must not dedup: %d vs %d (err %v)", retry, first, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_conversion_jobs`).Scan(&count); err != nil || count != 3 {
		t.Fatalf("unexpected queue size: %d rows (err %v)", count, err)
	}
}

// --- Claim ordering and lease exclusivity -----------------------------------

func TestClaimConversionOrdersByPriorityAndExcludesClaimants(t *testing.T) {
	s, db, _, _, fileID := conversionFixture(t)
	ctx := context.Background()
	library, _ := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 8)
	download, _ := s.EnqueueConversion(ctx, "download", fileID, "opus-high", 5)
	export, _ := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)

	job, owner, claimed, err := s.claimConversion(ctx)
	if err != nil || !claimed {
		t.Fatal("first claim failed", err)
	}
	if job.ID != export {
		t.Fatalf("priority 3 export must be claimed first, got job %d", job.ID)
	}
	if owner == "" || job.State != "processing" || job.Attempts != 1 || job.SourcePath != "Music/Artist/Album/01 Song.wma" {
		t.Fatalf("claimed job not leased honestly: %+v", job)
	}
	var heldOwner string
	if err := db.QueryRow(`SELECT work_owner FROM music_conversion_jobs WHERE id=?`, export).Scan(&heldOwner); err != nil || heldOwner != owner {
		t.Fatalf("lease owner not persisted: %q (err %v)", heldOwner, err)
	}
	var until int64
	if err := db.QueryRow(`SELECT work_until FROM music_conversion_jobs WHERE id=?`, export).Scan(&until); err != nil || until <= time.Now().Unix() {
		t.Fatalf("lease must extend into the future: %d (err %v)", until, err)
	}

	job, _, claimed, err = s.claimConversion(ctx)
	if err != nil || !claimed || job.ID != download {
		t.Fatalf("second claim must take the priority 5 download job: %d %v (err %v)", job.ID, claimed, err)
	}
	job, _, claimed, err = s.claimConversion(ctx)
	if err != nil || !claimed || job.ID != library {
		t.Fatalf("third claim must take the priority 8 library job: %d %v (err %v)", job.ID, claimed, err)
	}
	if _, _, claimed, err = s.claimConversion(ctx); err != nil || claimed {
		t.Fatalf("empty queue must not claim: %v (err %v)", claimed, err)
	}

	// A queued job whose backoff (work_until) is still in the future is not
	// claimable yet.
	if _, err := s.EnqueueConversion(ctx, "export", fileID, "mp3-compatible", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE music_conversion_jobs SET work_until=unixepoch()+3600 WHERE state='queued'`); err != nil {
		t.Fatal(err)
	}
	if _, _, claimed, err = s.claimConversion(ctx); err != nil || claimed {
		t.Fatalf("backed-off job must not be claimed: %v (err %v)", claimed, err)
	}
}

// --- Success: variant publication -------------------------------------------

func TestConversionWorkerPublishesVariant(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, downloads, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()

	id, err := s.EnqueueConversion(ctx, "download", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	job := runConversionUntil(t, s, id, "completed")
	if job.Progress != 100 || job.OutputPath != "Music/Artist/Album/01 Song [opus-high].opus" || job.CompletedAt == nil {
		t.Fatalf("completed job not recorded honestly: %+v", job)
	}
	if job.Error != "" {
		t.Fatalf("completed job carries an error: %q", job.Error)
	}

	variantID, relative, role, profile := variantRow(t, db, "opus-high")
	if role != "variant" || profile != "opus-high" {
		t.Fatalf("variant row wrong: role=%q profile=%q", role, profile)
	}
	var trackID int64
	var formatText, originText, sourceNameText string
	if err := db.QueryRow(`SELECT track_id,format,origin,source_name FROM music_files WHERE id=?`, variantID).
		Scan(&trackID, &formatText, &originText, &sourceNameText); err != nil {
		t.Fatal(err)
	}
	if trackID != 3 || formatText != "opus" || originText != "conversion" || sourceNameText != "01 Song.wma" {
		t.Fatalf("variant row metadata wrong: track=%d format=%q origin=%q source=%q", trackID, formatText, originText, sourceNameText)
	}
	variantPath := filepath.Join(media, filepath.FromSlash(relative))
	published, err := os.ReadFile(variantPath)
	if err != nil {
		t.Fatalf("published variant missing: %v", err)
	}
	if string(published) != string(conversionSourceBytes) {
		t.Fatal("published variant is not the fake ffmpeg output")
	}
	// The catalog digest describes the published bytes.
	var storedSHA string
	if err := db.QueryRow(`SELECT sha256 FROM music_files WHERE id=?`, variantID).Scan(&storedSHA); err != nil || storedSHA != sha256Hex(conversionSourceBytes) {
		t.Fatalf("variant digest not recorded: %q (err %v)", storedSHA, err)
	}
	// Keep-both defaults: the primary row and file are untouched.
	primaryBytes, err := os.ReadFile(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma"))
	if err != nil || string(primaryBytes) != string(conversionSourceBytes) {
		t.Fatal("primary file was modified by the conversion")
	}
	var primaryRole string
	if err := db.QueryRow(`SELECT variant_role FROM music_files WHERE id=?`, fileID).Scan(&primaryRole); err != nil || primaryRole != "primary" {
		t.Fatalf("primary role changed: %q (err %v)", primaryRole, err)
	}
	// The stage directory is cleaned after a successful publish.
	if _, err := os.Stat(conversionStageDir(downloads, id)); !os.IsNotExist(err) {
		t.Fatal("stage directory was left behind after a successful publish")
	}
}

// --- Failure, backoff and retry ---------------------------------------------

func TestConversionRetriesThenFailsAndNeverTouchesSource(t *testing.T) {
	helperFFmpeg(t)
	shortConversionBackoff(t)
	s, db, media, downloads, fileID := conversionFixture(t)
	s.SetProber(&fakeConvertProber{
		source: sourceTags("wma", 1000, 192),
		outErr: &Error{Code: "audio_unreadable", Message: "probe failed"},
	})
	ctx := context.Background()

	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 8)
	if err != nil {
		t.Fatal(err)
	}
	// Attempt 1 fails and is requeued with a durable backoff.
	if _, err := s.processConversion(ctx); err != nil {
		t.Fatal(err)
	}
	var state, jobErr string
	var attempts int
	var workUntil int64
	if err := db.QueryRow(`SELECT state,attempts,work_until,error FROM music_conversion_jobs WHERE id=?`, id).
		Scan(&state, &attempts, &workUntil, &jobErr); err != nil {
		t.Fatal(err)
	}
	if state != "queued" || attempts != 1 || jobErr == "" {
		t.Fatalf("first failure must requeue with an honest error: state=%q attempts=%d error=%q", state, attempts, jobErr)
	}
	if workUntil <= time.Now().Unix() {
		t.Fatalf("first failure must set a future backoff: %d", workUntil)
	}

	// Attempts 2 and 3 run; the third fails the job for good.
	job := runConversionUntil(t, s, id, "failed")
	if job.Attempts != 3 {
		t.Fatalf("job must fail on attempt 3, got %d", job.Attempts)
	}
	if job.Error == "" {
		t.Fatal("failed job must record an honest error")
	}
	// The source row and file are untouched, and no variant exists.
	var relative, sha string
	if err := db.QueryRow(`SELECT relative_path,sha256 FROM music_files WHERE id=?`, fileID).Scan(&relative, &sha); err != nil {
		t.Fatal(err)
	}
	if relative != "Music/Artist/Album/01 Song.wma" || sha != sha256Hex(conversionSourceBytes) {
		t.Fatalf("source row changed: %q %q", relative, sha)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil {
		t.Fatal("source file was removed by a failed conversion")
	}
	var variants int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE variant_role='variant'`).Scan(&variants); err != nil || variants != 0 {
		t.Fatalf("a failed conversion published a variant: %d rows (err %v)", variants, err)
	}
	if _, err := os.Stat(conversionStageDir(downloads, id)); !os.IsNotExist(err) {
		t.Fatal("failed conversion left its stage directory behind")
	}

	// RetryConversion requeues with attempts preserved.
	if err := s.RetryConversion(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT state,attempts FROM music_conversion_jobs WHERE id=?`, id).Scan(&state, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "queued" || attempts != 3 {
		t.Fatalf("retry must preserve attempts: state=%q attempts=%d", state, attempts)
	}
}

func TestRetryAndCancelRejectWrongStates(t *testing.T) {
	s, _, _, _, fileID := conversionFixture(t)
	ctx := context.Background()
	id, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RetryConversion(ctx, id); err == nil {
		t.Fatal("retrying a queued job must be rejected")
	}
	if err := s.CancelConversion(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := s.CancelConversion(ctx, id); err == nil {
		t.Fatal("cancelling a cancelled job must be rejected")
	}
	if err := s.RetryConversion(ctx, id); err != nil {
		t.Fatalf("retrying a cancelled job must work: %v", err)
	}
	if err := s.RetryConversion(ctx, id); err == nil {
		t.Fatal("retrying an already queued job must be rejected")
	}
	if err := s.CancelConversion(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancelling a missing job must be ErrNotFound: %v", err)
	}
}

// --- Cancellation ------------------------------------------------------------

func TestCancelQueuedConversion(t *testing.T) {
	s, db, _, _, fileID := conversionFixture(t)
	ctx := context.Background()
	id, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CancelConversion(ctx, id); err != nil {
		t.Fatal(err)
	}
	var state, jobErr string
	var completed sql.NullInt64
	if err := db.QueryRow(`SELECT state,error,completed_at FROM music_conversion_jobs WHERE id=?`, id).Scan(&state, &jobErr, &completed); err != nil {
		t.Fatal(err)
	}
	if state != "cancelled" || jobErr != "cancellation requested" {
		t.Fatalf("cancelled job state wrong: %q %q", state, jobErr)
	}
	if completed.Valid {
		t.Fatal("cancelled job must not record completed_at")
	}
	// A cancelled queued job is never claimed.
	if _, _, claimed, err := s.claimConversion(ctx); err != nil || claimed {
		t.Fatalf("cancelled job must not be claimable: %v (err %v)", claimed, err)
	}
}

// gatedConvertProber blocks its first probe until the gate closes, so the
// test can cancel a processing job while the worker is mid-conversion.
type gatedConvertProber struct {
	gate   chan struct{}
	source AudioTags
	output AudioTags
}

func (p *gatedConvertProber) ProbeAudio(_ context.Context, path string) (AudioTags, error) {
	if p.gate != nil {
		gate := p.gate
		p.gate = nil
		<-gate
	}
	if strings.HasSuffix(path, ".part") {
		return p.output, nil
	}
	return p.source, nil
}

func TestConversionCooperativeCancelWhileProcessing(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, downloads, fileID := conversionFixture(t)
	gate := make(chan struct{})
	prober := &gatedConvertProber{gate: gate, source: sourceTags("wma", 1000, 192), output: sourceTags("opus", 1000, 128)}
	s.SetProber(prober)
	ctx := context.Background()

	id, err := s.EnqueueConversion(ctx, "download", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := s.processConversion(ctx)
		done <- err
	}()
	// Wait until the claim happened, then cancel while the conversion is
	// blocked inside ConvertFile.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var state string
		if err := db.QueryRow(`SELECT state FROM music_conversion_jobs WHERE id=?`, id).Scan(&state); err != nil {
			t.Fatal(err)
		}
		if state == "processing" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job was never claimed")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := s.CancelConversion(ctx, id); err != nil {
		t.Fatal(err)
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatalf("worker run after cancel returned an error: %v", err)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM music_conversion_jobs WHERE id=?`, id).Scan(&state); err != nil || state != "cancelled" {
		t.Fatalf("cancelled processing job must stay cancelled: %q (err %v)", state, err)
	}
	var variants int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE variant_role='variant'`).Scan(&variants); err != nil || variants != 0 {
		t.Fatalf("a cancelled conversion published a variant: %d rows (err %v)", variants, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil {
		t.Fatal("source file was modified by a cancelled conversion")
	}
	if _, err := os.Stat(conversionStageDir(downloads, id)); !os.IsNotExist(err) {
		t.Fatal("cancelled conversion left its staged output behind")
	}
}

// --- Restart recovery --------------------------------------------------------

func TestRecoverConversionsRequeuesProcessing(t *testing.T) {
	s, db, _, _, fileID := conversionFixture(t)
	ctx := context.Background()
	enqueued, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil {
		t.Fatal(err)
	}
	result, err := db.Exec(`INSERT INTO music_conversion_jobs(kind,file_id,profile,state,priority,attempts,work_owner,work_until,created_at,updated_at)
		VALUES('library',?,'opus-high','processing',8,1,'interrupted-owner',unixepoch()+300,1,1)`, fileID)
	if err != nil {
		t.Fatal(err)
	}
	interrupted, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverConversions(ctx); err != nil {
		t.Fatal(err)
	}
	var state, owner, jobErr string
	var attempts int
	var workUntil int64
	if err := db.QueryRow(`SELECT state,work_owner,error,attempts,work_until FROM music_conversion_jobs WHERE id=?`, interrupted).
		Scan(&state, &owner, &jobErr, &attempts, &workUntil); err != nil {
		t.Fatal(err)
	}
	if state != "queued" || owner != "" {
		t.Fatalf("processing row must be requeued by recovery: state=%q owner=%q", state, owner)
	}
	if !strings.Contains(jobErr, "interrupted by a restart") {
		t.Fatalf("recovered job must explain the interruption: %q", jobErr)
	}
	if attempts != 2 {
		t.Fatalf("recovery must count the interrupted run as an attempt: %d", attempts)
	}
	if workUntil != 0 {
		t.Fatalf("recovered job must be immediately claimable: %d", workUntil)
	}
	// The recovery never touches jobs that were not processing: the untouched
	// queued row still holds its original state, and it is claimed first
	// (lower priority number) before the recovered row.
	if err := db.QueryRow(`SELECT state,work_owner,attempts FROM music_conversion_jobs WHERE id=?`, enqueued).Scan(&state, &owner, &attempts); err != nil {
		t.Fatal(err)
	}
	if state != "queued" || owner != "" || attempts != 0 {
		t.Fatalf("queued row must be untouched by recovery: %q %q %d", state, owner, attempts)
	}
	job, _, claimed, err := s.claimConversion(ctx)
	if err != nil || !claimed {
		t.Fatal("recovered job must be claimable", err)
	}
	if job.ID != enqueued || job.Attempts != 1 {
		t.Fatalf("claim order wrong: job %d attempts %d", job.ID, job.Attempts)
	}
	if _, _, claimed, err = s.claimConversion(ctx); err != nil || !claimed {
		t.Fatal("recovered job must be claimable", err)
	}
}

// --- Retention ----------------------------------------------------------------

func TestConversionRetentionDeletesOriginalAfterVerifiedPublish(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, _, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()
	if _, err := db.Exec(`UPDATE music_preferences SET original_retention='delete' WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 8)
	if err != nil {
		t.Fatal(err)
	}
	if job := runConversionUntil(t, s, id, "completed"); job.Error != "" {
		t.Fatalf("completed job with retention reported an error: %q", job.Error)
	}
	// The original row and file are gone.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE id=?`, fileID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("original row was not deleted: %d rows (err %v)", count, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); !os.IsNotExist(err) {
		t.Fatal("original file was not deleted after the verified publish")
	}
	// The variant was promoted to the track's primary file.
	variantID, relative, role, profile := variantRow(t, db, "")
	if role != "primary" || profile != "" {
		t.Fatalf("promoted variant wrong: role=%q profile=%q", role, profile)
	}
	published, err := os.ReadFile(filepath.Join(media, filepath.FromSlash(relative)))
	if err != nil || string(published) != string(conversionSourceBytes) {
		t.Fatal("promoted variant file is not the verified conversion output")
	}
	_ = variantID
}

func TestConversionRetentionKeepsOriginalWhenPublishFails(t *testing.T) {
	helperFFmpeg(t)
	shortConversionBackoff(t)
	s, db, media, _, fileID := conversionFixture(t)
	s.SetProber(&fakeConvertProber{
		source: sourceTags("wma", 1000, 192),
		outErr: &Error{Code: "audio_unreadable", Message: "probe failed"},
	})
	ctx := context.Background()
	if _, err := db.Exec(`UPDATE music_preferences SET original_retention='delete' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil {
		t.Fatal(err)
	}
	runConversionUntil(t, s, id, "failed")
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE id=?`, fileID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed conversion must keep the original row: %d rows (err %v)", count, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil {
		t.Fatal("failed conversion must keep the original file on disk")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE variant_role='variant'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed conversion published a variant: %d rows (err %v)", count, err)
	}
}

func TestConversionRetentionKeepsBothWhenPolicySaysKeep(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, _, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()
	// Default preference is original_retention='keep' even for a
	// KeepOriginal=false profile.
	id, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil {
		t.Fatal(err)
	}
	runConversionUntil(t, s, id, "completed")
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files`).Scan(&count); err != nil || count != 2 {
		t.Fatalf("keep policy must leave both rows: %d rows (err %v)", count, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil {
		t.Fatal("keep policy must leave the original file on disk")
	}
}

// --- Duplicate variant replacement ------------------------------------------

func TestConversionReplacesExistingVariantWithoutDuplicatingRows(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, downloads, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()

	id, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil {
		t.Fatal(err)
	}
	runConversionUntil(t, s, id, "completed")
	variantID, relative, _, _ := variantRow(t, db, "opus-high")

	// Change the source bytes, then convert again: the same (file, profile)
	// variant must be replaced in place, never duplicated.
	newBytes := []byte("second-generation-audio-bytes-for-the-queue")
	if err := os.WriteFile(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma"), newBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := s.EnqueueConversion(ctx, "export", fileID, "opus-high", 3)
	if err != nil {
		t.Fatal(err)
	}
	if second == id {
		t.Fatal("a completed job must not dedup a new conversion")
	}
	runConversionUntil(t, s, second, "completed")

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE variant_role='variant' AND variant_profile='opus-high'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("variant replacement duplicated rows: %d rows (err %v)", count, err)
	}
	variantIDAfter, relativeAfter, _, _ := variantRow(t, db, "opus-high")
	if variantIDAfter != variantID || relativeAfter != relative {
		t.Fatalf("variant row was not updated in place: %d/%q vs %d/%q", variantIDAfter, relativeAfter, variantID, relative)
	}
	published, err := os.ReadFile(filepath.Join(media, filepath.FromSlash(relativeAfter)))
	if err != nil || string(published) != string(newBytes) {
		t.Fatal("the variant file was not replaced with the new conversion output")
	}
	var storedSHA string
	if err := db.QueryRow(`SELECT sha256 FROM music_files WHERE id=?`, variantID).Scan(&storedSHA); err != nil || storedSHA != sha256Hex(newBytes) {
		t.Fatalf("replaced variant digest not updated: %q (err %v)", storedSHA, err)
	}
	if _, err := os.Stat(conversionStageDir(downloads, second)); !os.IsNotExist(err) {
		t.Fatal("stage directory left behind after a variant replacement")
	}
	// The old variant file's directory holds no leftover swap files.
	entries, err := os.ReadDir(filepath.Dir(filepath.Join(media, filepath.FromSlash(relativeAfter))))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".reelscarey-") {
			t.Fatalf("swap file left behind: %s", entry.Name())
		}
	}
}

// --- Listing ------------------------------------------------------------------

func TestListConversionsBoundedPage(t *testing.T) {
	s, _, _, _, fileID := conversionFixture(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 8); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ListConversions(ctx, 0); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid page must be rejected: %v", err)
	}
	page, err := s.ListConversions(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Limit != 100 {
		t.Fatalf("deduped queue page wrong: %+v", page)
	}
	if page.Items[0].Kind != "library" || page.Items[0].State != "queued" || page.Items[0].Priority != 8 {
		t.Fatalf("listed job wrong: %+v", page.Items[0])
	}
}
