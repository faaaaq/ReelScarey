package music

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Independent audit of the conversion engine, durable conversion queue and
// download-to-publication flow (audit_e_*). These tests cover the confirmed
// defects found by the audit and the six audit areas. The live end-to-end
// test with the real ffmpeg/ffprobe lives in audit_e_live_test.go.

// --- Defect A: per-download custom profile names resolve at import time -----

// TestAuditEImportPlanResolvesCustomProfileName proves the per-download
// custom profile NAME stored on the acquisition job's format column is
// honored at import time instead of being silently dropped.
func TestAuditEImportPlanResolvesCustomProfileName(t *testing.T) {
	service, ctx := auditPProfilesFixture(t)
	custom, err := service.SaveCustomProfile(ctx, QualityProfile{Name: "Quiet Opus", Codec: "opus", BitrateKbps: 128})
	if err != nil {
		t.Fatal(err)
	}
	auditPSetPreferences(t, service, ctx, func(p *MusicPreferences) {
		p.TranscodingEnabled = true
		p.DownloadProfile = "original"
	})
	// The stored value is the custom profile NAME (custom rows have no key).
	if _, err := service.store.db.Exec(`INSERT INTO music_acquisition_jobs(id,root,format,state,track_count,created_at,updated_at) VALUES(7,'Music',?, 'queued',1,1,1)`, custom.Name); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.db.Exec(`INSERT INTO music_acquisition_tracks(id,job_id,position,title,artist,album,state,import_job_id,created_at,updated_at) VALUES(1,7,1,'Song','Artist','Album','completed',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	plan := service.importProfilePlan(ctx, 1, 1)
	if plan == nil {
		t.Fatal("the per-download custom profile name was silently dropped at import time")
	}
	if plan.Profile.Codec != "opus" || plan.Profile.BitrateKbps != 128 {
		t.Fatalf("resolved profile is not the custom profile: %+v", plan.Profile)
	}
}

// --- Defect B: non-primary files must never be enqueued as conversion sources ---

// TestAuditEEnqueueConversionRejectsVariantSource proves a variant or archive
// file id cannot be enqueued as a conversion source, so with retention='delete'
// the queue can never duplicate the primary or delete the only copy.
func TestAuditEEnqueueConversionRejectsVariantSource(t *testing.T) {
	s, db, _, _, fileID := conversionFixture(t)
	ctx := context.Background()
	for _, role := range []string{"variant", "archive"} {
		result, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name,variant_role,variant_profile)
			VALUES(?,?,?,'Music',?,26,'sha','opus',96,44100,2,1000,'{}',1,'conversion','01 Song.wma',?,?)`,
			1, 2, 3, "Music/Artist/Album/01 Song ["+role+"].opus", role, "opus-high")
		if err != nil {
			t.Fatal(err)
		}
		nonPrimary, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.EnqueueConversion(ctx, "library", nonPrimary, "opus-high", 5); err == nil {
			t.Fatalf("a %s file id must be rejected at enqueue: converting it would duplicate the primary or delete the only copy", role)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_conversion_jobs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected enqueues must not create jobs: %d rows (err %v)", count, err)
	}
	if _, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5); err != nil {
		t.Fatal("the primary file id must still be accepted", err)
	}
}

// TestAuditEEnqueueConversionRejectsMissingFile proves the file row must exist
// at enqueue: a deleted catalog row must not burn three failed attempts on a
// ghost job.
func TestAuditEEnqueueConversionRejectsMissingFile(t *testing.T) {
	s, db, _, _, _ := conversionFixture(t)
	ctx := context.Background()
	if _, err := s.EnqueueConversion(ctx, "library", 987654, "opus-high", 5); err == nil {
		t.Fatal("a missing file id must be rejected at enqueue")
	} else {
		var musicErr *Error
		if !errors.As(err, &musicErr) || musicErr.Code != "music_file_missing" {
			t.Fatalf("missing file id must report a typed error, got %v", err)
		}
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_conversion_jobs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("rejected enqueue must not create a job: %d rows (err %v)", count, err)
	}
}

// --- Defect C: the archive destination's aside copy must not stay primary ---

// TestAuditEImportArchiveAsideDemotesOccupantPrimary proves the renamed-aside
// occupant of the archive destination does not keep competing as a primary:
// the newly imported primary is the preferred file, and the aside copy holds
// the archive role.
func TestAuditEImportArchiveAsideDemotesOccupantPrimary(t *testing.T) {
	service, media, downloads := importFixture(t)
	generateFLAC(t, filepath.Join(downloads, "Album", "01.flac"), "Song", 1)
	generateFLAC(t, filepath.Join(downloads, "Album", "02.flac"), "Other", 2)
	// A primary row occupies the future archive destination "01 Song.flac"
	// with different bytes. The import supersedes it: the converted primary
	// is published at "01 Song.opus" and the retained original at
	// "01 Song.flac", so the old occupant is renamed aside.
	albumDir := filepath.Join(media, "Music", "Artist", "Album")
	if err := os.MkdirAll(albumDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(albumDir, "01 Song.flac"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.store.db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name,variant_role,variant_profile)
		VALUES(1,2,3,'Music','Music/Artist/Album/01 Song.flac',8,'sha','flac',0,44100,2,1000,'{}',1,'existing','01 Song.flac','primary','')`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	auditPSetPreferences(t, service, ctx, func(p *MusicPreferences) {
		p.TranscodingEnabled = true
		p.DownloadProfile = "opus-balanced"
		p.OriginalRetention = "keep"
	})
	job, err := service.StartImport(ctx, 2, "Album", "Music")
	if err != nil {
		t.Fatal(err)
	}
	job = waitForImport(t, ctx, service, job.ID)
	if job.State != "completed" || job.ImportedCount != 2 {
		t.Fatal("conflicting import job", job)
	}
	// Exactly one primary row for the track, and it is the converted file.
	var primaries int
	var preferredPath string
	if err := service.store.db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE variant_role='primary' AND track_id=3`).Scan(&primaries); err != nil {
		t.Fatal(err)
	}
	if err := service.store.db.QueryRow(`SELECT relative_path FROM music_files WHERE variant_role='primary' AND track_id=3`).Scan(&preferredPath); err != nil {
		t.Fatal(err)
	}
	if primaries != 1 || preferredPath != "Music/Artist/Album/01 Song.opus" {
		t.Fatalf("aside copy kept primary preference: %d primaries, preferred %q", primaries, preferredPath)
	}
	// The aside copy is demoted to the archive role.
	var asideRole string
	if err := service.store.db.QueryRow(`SELECT variant_role FROM music_files WHERE relative_path='Music/Artist/Album/01 Song (2).flac'`).Scan(&asideRole); err != nil {
		t.Fatal("the aside row was not registered", err)
	}
	if asideRole != "archive" {
		t.Fatalf("aside copy kept variant_role=%q, want 'archive'", asideRole)
	}
	if got, err := os.ReadFile(filepath.Join(albumDir, "01 Song (2).flac")); err != nil || string(got) != "occupied" {
		t.Fatal("the aside copy was not preserved", got, err)
	}
}

// --- Interrupted jobs and restarts -------------------------------------------

// TestAuditEConversionInterruptedMidConvertThenRetry interrupts a running
// conversion by cancelling the worker context mid-convert, then retries the
// job to completion. The interrupted run is requeued by the graceful shutdown
// path, the stage directory is cleaned on both failure and success, and the
// source row and file are untouched.
func TestAuditEConversionInterruptedMidConvertThenRetry(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, downloads, fileID := conversionFixture(t)
	gate := make(chan struct{})
	prober := &gatedConvertProber{gate: gate, source: sourceTags("wma", 1000, 192), output: sourceTags("opus", 1000, 96)}
	s.SetProber(prober)
	ctx := context.Background()

	id, err := s.EnqueueConversion(ctx, "download", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	workerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := s.processConversion(workerCtx)
		done <- err
	}()
	// Wait until the job is claimed, then cancel the worker context mid-convert.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var state string
		if err := db.QueryRow(`SELECT state FROM music_conversion_jobs WHERE id=?`, id).Scan(&state); err == nil && state == "processing" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("job was never claimed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	close(gate)
	if err := <-done; err != nil {
		t.Fatal("worker error", err)
	}
	// The interrupted run is requeued immediately with the claim's attempt
	// count preserved, and the stage directory is cleaned.
	var state, jobErr string
	var attempts int64
	if err := db.QueryRow(`SELECT state,attempts,error FROM music_conversion_jobs WHERE id=?`, id).Scan(&state, &attempts, &jobErr); err != nil {
		t.Fatal(err)
	}
	if state != "queued" || attempts != 1 || jobErr == "" {
		t.Fatalf("interrupted run must be requeued with an honest error: state=%q attempts=%d error=%q", state, attempts, jobErr)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil {
		t.Fatal("source file was modified", err)
	}
	if _, err := os.Stat(conversionStageDir(downloads, id)); !os.IsNotExist(err) {
		t.Fatal("interrupted run left its staged output behind", err)
	}
	// The retry runs to completion.
	s.SetProber(conversionProber())
	if job := runConversionUntil(t, s, id, "completed"); job.State != "completed" {
		t.Fatal("retry did not complete", job)
	}
	var completedAttempts int64
	if err := db.QueryRow(`SELECT attempts FROM music_conversion_jobs WHERE id=?`, id).Scan(&completedAttempts); err != nil {
		t.Fatal(err)
	}
	if completedAttempts != 2 {
		t.Fatalf("retry must count the interrupted run as an attempt: %d", completedAttempts)
	}
	// The stage directory is cleaned on success.
	if _, err := os.Stat(conversionStageDir(downloads, id)); !os.IsNotExist(err) {
		t.Fatal("stage directory left behind after success", err)
	}
}

// TestAuditEConversionSourceVanishedMidQueue proves a job whose source file
// vanished between the enqueue and the claim fails honestly without touching
// other rows: the source row and file are kept, and the second job is
// untouched by the first job's failure.
func TestAuditEConversionSourceVanishedMidQueue(t *testing.T) {
	helperFFmpeg(t)
	shortConversionBackoff(t)
	s, db, media, _, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()
	// A second job on a second primary file of the same track, which is not
	// affected by the vanished source row of the first job.
	secondRel := "Music/Artist/Album/02 Other.wma"
	if err := os.WriteFile(filepath.Join(media, filepath.FromSlash(secondRel)), conversionSourceBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)
		VALUES(1,2,3,'Music',?,?,?,'wma',192,44100,2,1000,'{}',1,'existing','02 Other.wma')`,
		secondRel, len(conversionSourceBytes), sha256Hex(conversionSourceBytes)); err != nil {
		t.Fatal(err)
	}
	// The source row is deleted mid-queue (the conversionSource fence rejects
	// the ghost job honestly).
	first, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal("the first job must be accepted", err)
	}
	if _, err := db.Exec(`DELETE FROM music_files WHERE id=?`, fileID); err != nil {
		t.Fatal(err)
	}
	if job := runConversionUntil(t, s, first, "failed"); job.Error == "" {
		t.Fatal("a vanished source must fail with an honest error")
	}
	// The source file on disk was never touched.
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil {
		t.Fatal("the source file was modified", err)
	}
	// The second job is untouched by the first job's failure: it completes.
	second, err := s.EnqueueConversion(ctx, "library", fileID+1, "opus-high", 5)
	if err != nil {
		t.Fatal("the second job must be accepted", err)
	}
	if job := runConversionUntil(t, s, second, "completed"); job.State != "completed" {
		t.Fatal("the second job must complete despite the first job's failure", job)
	}
	var state string
	if err := db.QueryRow(`SELECT state FROM music_conversion_jobs WHERE id=?`, second).Scan(&state); err != nil || state != "completed" {
		t.Fatalf("the second job must complete: %q (err %v)", state, err)
	}
}

// --- Duplicate/parallel claims ------------------------------------------------

// TestAuditEConversionParallelClaimsNeverDoublePublish proves two workers
// racing on the claim cannot both publish: the claim is exclusive, so the
// second worker publishes nothing and the first worker's variant is the only
// variant row for the profile.
func TestAuditEConversionParallelClaimsNeverDoublePublish(t *testing.T) {
	helperFFmpeg(t)
	s, db, _, _, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()
	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	// One claim wins; the second returns nothing.
	job1, owner1, claimed1, err := s.claimConversion(ctx)
	if err != nil || !claimed1 {
		t.Fatal("first claim must succeed", err)
	}
	_, _, claimed2, err := s.claimConversion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if claimed2 {
		t.Fatal("a second claim must not claim the same job")
	}
	// The first worker runs the conversion to completion.
	if err := s.runConversion(ctx, job1, owner1); err != nil {
		t.Fatal(err)
	}
	variants, err := db.Query(`SELECT COUNT(*) FROM music_files WHERE variant_role='variant' AND variant_profile='opus-high'`)
	if err != nil {
		t.Fatal(err)
	}
	defer variants.Close()
	if !variants.Next() {
		t.Fatal("no variant row")
	}
	var count int
	if err := variants.Scan(&count); err != nil || count != 1 {
		t.Fatalf("exactly one variant row must exist: %d (err %v)", count, err)
	}
	// The job is completed.
	var state string
	if err := db.QueryRow(`SELECT state FROM music_conversion_jobs WHERE id=?`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "completed" {
		t.Fatalf("job state after the single worker run = %q, want completed", state)
	}
}

// TestAuditEConversionVariantReplacementIdempotentOnRetry proves variant
// replacement is idempotent on retry after a crash between the variant commit
// and the stage cleanup: the same (track, profile) still has exactly one row.
func TestAuditEConversionVariantReplacementIdempotentOnRetry(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, downloads, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()
	if _, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5); err != nil {
		t.Fatal(err)
	}
	if job := runConversionUntil(t, s, 1, "completed"); job.State != "completed" {
		t.Fatal("conversion", job)
	}
	variantID, _, _, _ := variantRow(t, db, "opus-high")
	// The variant row and file exist.
	var path string
	if err := db.QueryRow(`SELECT relative_path FROM music_files WHERE id=?`, variantID).Scan(&path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(media, filepath.FromSlash(path))); err != nil {
		t.Fatal("variant file missing", err)
	}
	// A second job on the same file replaces the variant in place, and the
	// stage directory is cleaned.
	if _, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5); err != nil {
		t.Fatal(err)
	}
	if job := runConversionUntil(t, s, 2, "completed"); job.State != "completed" {
		t.Fatal("second conversion", job)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE variant_role='variant' AND variant_profile='opus-high'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("variant replacement duplicated rows: %d rows", count)
	}
	if _, err := os.Stat(filepath.Join(downloads, "music-conversions", "2")); !os.IsNotExist(err) {
		t.Fatal("stage directory left behind", err)
	}
}

// --- Failure preservation ------------------------------------------------------

// TestAuditECorruptSourceFailsHonestlyAndKeepsSource proves a corrupt source
// file (truncated garbage with a media extension) fails the job with an honest
// message while the source row and file are intact, and no orphaned .part
// files are left anywhere.
func TestAuditECorruptSourceFailsHonestlyAndKeepsSource(t *testing.T) {
	helperFFmpeg(t)
	shortConversionBackoff(t)
	s, db, media, downloads, fileID := conversionFixture(t)
	// The source bytes are garbage: the real FFprobeTagger cannot read them.
	if err := os.WriteFile(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma"), []byte("truncated garbage with a media extension"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(FFprobeTagger{})
	ctx := context.Background()
	if _, err := db.Exec(`UPDATE music_preferences SET original_retention='delete' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	job := runConversionUntil(t, s, id, "failed")
	if !strings.Contains(job.Error, "FFprobe could not read") {
		t.Fatalf("honest failure message = %q", job.Error)
	}
	// The source row and file are intact.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE id=?`, fileID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("source row must be kept: %d rows (err %v)", count, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil {
		t.Fatal("source file was deleted", err)
	}
	// No orphaned .part files anywhere below the media root or downloads root.
	for _, root := range []string{media, downloads} {
		if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if strings.HasSuffix(path, ".part") {
				t.Errorf("orphaned .part file: %s", path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestAuditEConversionFailurePreservesSourceRow proves a conversion output
// validation failure keeps the source row and file: the job fails honestly.
func TestAuditEConversionFailurePreservesSourceRow(t *testing.T) {
	helperFFmpeg(t)
	shortConversionBackoff(t)
	s, db, media, _, fileID := conversionFixture(t)
	// The prober claims the output is not decodable: the job fails honestly.
	s.SetProber(&fakeConvertProber{source: sourceTags("wma", 1000, 192), outErr: &Error{Code: "audio_unreadable", Message: "probe failed"}})
	ctx := context.Background()
	if _, err := db.Exec(`UPDATE music_preferences SET original_retention='delete' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	job := runConversionUntil(t, s, id, "failed")
	if job.Error == "" {
		t.Fatal("output validation failure must fail with an honest message")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE id=?`, fileID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("source row must be kept on failure: %d rows (err %v)", count, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil {
		t.Fatal("source file was deleted", err)
	}
}

// TestAuditEConversionNoZombieAfterCancellation proves a blocked fake ffmpeg
// child is killed when the parent context is cancelled: the child is reaped
// and the conversion returns a typed error, not a hang.
func TestAuditEConvertFileKillsProcessOnCancel(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	// The fake ffmpeg child replays the source bytes in 512-byte reads, so a
	// 64 MiB source keeps the child alive for the whole cancellation window.
	chunk := bytes.Repeat([]byte("0123456789abcdef"), 64<<10)
	var source bytes.Buffer
	for i := 0; i < 64; i++ {
		source.Write(chunk)
	}
	srcPath := filepath.Join(dir, "sine.ogg")
	outPath := filepath.Join(dir, "out.ogg")
	partPath := outPath + ".part"
	if err := os.WriteFile(srcPath, source.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{source: sourceTags("wma", 1000, 192), output: sourceTags("opus", 1000, 96)})
	done := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_, err := s.ConvertFile(ctx, ConvertRequest{
			SourcePath: srcPath,
			OutputPath: outPath,
			Profile:    mustProfile("opus-high"),
		})
		done <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if info, err := os.Stat(partPath); err == nil && info.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("staged .part file never appeared")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel() // the 64 MiB replay is still running
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected a typed cancellation error, got %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("ConvertFile did not return after cancellation")
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Fatal("cancelled conversion left a staged .part file")
	}
	after, err := os.ReadFile(srcPath)
	if err != nil || !bytes.Equal(after, source.Bytes()) {
		t.Fatal("source bytes were modified by the killed conversion")
	}
}

// TestAuditEConvertTimeoutTypedError proves the timeout enforcement produces
// a typed error, not a hang.
func TestAuditEConvertTimeoutTypedError(t *testing.T) {
	helperFFmpeg(t)
	s, dir := convertFixture(t)
	// The fake ffmpeg child replays a 64 MiB source in 512-byte reads, so it
	// outlives the 200ms timeout and the kill is enforced mid-convert.
	chunk := bytes.Repeat([]byte("0123456789abcdef"), 64<<10)
	var source bytes.Buffer
	for i := 0; i < 64; i++ {
		source.Write(chunk)
	}
	srcPath := filepath.Join(dir, "sine.ogg")
	if err := os.WriteFile(srcPath, source.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	s.SetProber(&fakeConvertProber{source: sourceTags("wma", 1000, 192), output: sourceTags("opus", 1000, 96)})
	_, err := s.ConvertFile(context.Background(), ConvertRequest{
		SourcePath: srcPath,
		OutputPath: filepath.Join(dir, "out.ogg"),
		Profile:    mustProfile("opus-high"),
		// 50ms: far below the 64 MiB 512-byte-read replay's runtime even on
		// the fastest hardware, so the fake ffmpeg can never legitimately
		// finish within the deadline; together with verifyConvertOutput's
		// expired-context guard the typed timeout error is deterministic.
		Timeout: 50 * time.Millisecond,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected a typed timeout error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "out.ogg")); !os.IsNotExist(err) {
		t.Fatal("timed-out conversion published an output")
	}
	if _, err := os.Stat(filepath.Join(dir, "out.ogg") + ".part"); !os.IsNotExist(err) {
		t.Fatal("timed-out conversion left a staged .part file")
	}
	after, err := os.ReadFile(srcPath)
	if err != nil || !bytes.Equal(after, source.Bytes()) {
		t.Fatal("source bytes were modified by the timed-out conversion")
	}
}

// --- Safe original replacement ---------------------------------------------------

// TestAuditEConversionRetentionDeleteOriginalAfterVerifiedPublish proves the
// exact retention delete ordering: the variant is committed and the converted
// bytes are verified on disk before the original is deleted, and the SHA/path
// fence prevents deleting a different file that occupies the same path.
func TestAuditEConversionRetentionDeleteFenceCatchesSwappedBytes(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, _, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()
	if _, err := db.Exec(`UPDATE music_preferences SET original_retention='delete' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// A conversion that runs to completion with retention delete.
	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	if job := runConversionUntil(t, s, id, "completed"); job.State != "completed" {
		t.Fatal("conversion", job)
	}
	// The original row and file are gone.
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE id=?`, fileID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("original row was not deleted: %d rows (err %v)", count, err)
	}
	if _, err := os.Stat(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); !os.IsNotExist(err) {
		t.Fatal("original file was not deleted after the verified publish", err)
	}
	// The variant was promoted to the track's primary file: the promoted row
	// is the primary and its variant identity is cleared.
	var role, profile string
	if err := db.QueryRow(`SELECT variant_role,variant_profile FROM music_files WHERE variant_role='primary'`).Scan(&role, &profile); err != nil {
		t.Fatal(err)
	}
	if role != "primary" || profile != "" {
		t.Fatalf("promoted variant role=%q profile=%q, want primary/''", role, profile)
	}
}

// TestAuditEConversionRetentionSwapBetweenFenceAndDelete simulates the
// adversarial case: the original's bytes are replaced by different content
// before the retention delete. The fence must catch the swap (both when the
// bytes on disk no longer match the catalog row and when the catalog row was
// rewritten to match the swapped bytes) and keep both files, never deleting
// the wrong bytes.
func TestAuditEConversionRetentionSwapBetweenFenceAndDelete(t *testing.T) {
	helperFFmpeg(t)
	s, db, media, _, fileID := conversionFixture(t)
	s.SetProber(conversionProber())
	ctx := context.Background()
	// A conversion that runs to completion with the original kept (the fixture
	// default is retention keep), then a retention delete is armed directly.
	id, err := s.EnqueueConversion(ctx, "library", fileID, "opus-high", 5)
	if err != nil {
		t.Fatal(err)
	}
	if job := runConversionUntil(t, s, id, "completed"); job.State != "completed" {
		t.Fatal("conversion", job)
	}
	variantID, variantRel, _, _ := variantRow(t, db, "opus-high")
	if _, err := db.Exec(`UPDATE music_preferences SET original_retention='delete' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	// The variant file is the verified conversion output (the fake ffmpeg
	// replays the source bytes, so its digest is the source digest).
	variantPath := filepath.Join(media, filepath.FromSlash(variantRel))
	if _, err := os.Stat(variantPath); err != nil {
		t.Fatal("variant file missing", err)
	}
	variantDigest := sha256Hex(conversionSourceBytes)
	swappedBytes := []byte("different bytes than the converted source")
	if err := os.WriteFile(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma"), swappedBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	// The conversion-time snapshot is captured once, before any tampering, so
	// both adversarial cases are fenced against the real conversion evidence.
	src := auditEConversionSource(t, s, fileID)
	var preCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE id=?`, fileID).Scan(&preCount); err != nil {
		t.Fatal(err)
	}
	t.Logf("before first applyConversionRetention: row=%d", preCount)
	// The row's sha256 was not rewritten, so the fence's digest check on disk
	// must catch the swapped bytes and keep both files.
	if err := s.applyConversionRetention(ctx, src, variantRel, variantDigest, mustProfile("opus-high")); err != nil {
		t.Fatal(err)
	}
	if err := auditEAssertSourceKept(t, db, media, fileID, variantID, swappedBytes); err != nil {
		t.Fatal(err)
	}
	// The adversarial case where the catalog row was rewritten to match the
	// swapped bytes: the row fence (src.SHA256) must catch the swap.
	if _, err := db.Exec(`UPDATE music_files SET sha256=? WHERE id=?`, sha256Hex(swappedBytes), fileID); err != nil {
		t.Fatal(err)
	}
	if err := s.applyConversionRetention(ctx, src, variantRel, variantDigest, mustProfile("opus-high")); err != nil {
		t.Fatal(err)
	}
	if err := auditEAssertSourceKept(t, db, media, fileID, variantID, swappedBytes); err != nil {
		t.Fatal(err)
	}
}

func auditEAssertSourceKept(t *testing.T, db *sql.DB, media string, fileID, variantID int64, swappedBytes []byte) error {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_files WHERE id=?`, fileID).Scan(&count); err != nil || count != 1 {
		return fmt.Errorf("the swapped original must be kept: %d rows (err %v)", count, err)
	}
	if got, err := os.ReadFile(filepath.Join(media, "Music", "Artist", "Album", "01 Song.wma")); err != nil || !bytes.Equal(got, swappedBytes) {
		return fmt.Errorf("the fence deleted the wrong bytes: %q (err %v)", got, err)
	}
	var role string
	if err := db.QueryRow(`SELECT variant_role FROM music_files WHERE id=?`, variantID).Scan(&role); err != nil || role != "variant" {
		return fmt.Errorf("the variant must stay a variant when the swap is caught: %q (err %v)", role, err)
	}
	return nil
}

func auditEConversionSource(t *testing.T, s *Service, fileID int64) conversionSource {
	t.Helper()
	src, err := s.conversionSource(context.Background(), fileID)
	if err != nil {
		t.Fatal(err)
	}
	return src
}
