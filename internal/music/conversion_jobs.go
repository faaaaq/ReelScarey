package music

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Conversion job kinds. The kind records who enqueued the work so callers can
// dedup their own requests; every job converts one registered music_files row.
// Callers enqueue export jobs at priority 3, download jobs at 5 and library
// bulk optimization at 8; the worker claims the lowest number first.
const (
	ConversionKindDownload = "download"
	ConversionKindLibrary  = "library"
	ConversionKindExport   = "export"
)

const (
	// conversionLeaseSeconds bounds one worker claim. The lease is renewed
	// while a conversion runs so a crashed or restarted process releases it.
	conversionLeaseSeconds = 300
	// conversionClaimInterval is how often the durable worker looks for work.
	conversionClaimInterval = 2 * time.Second
	// conversionMaxAttempts is how many times one job may run before it is
	// reported as failed. The count includes runs interrupted by restarts.
	conversionMaxAttempts = 3
)

// conversionRetryBackoff is the delay before a failed conversion is retried,
// scaled by the attempt count (attempts x backoff). It is a variable so tests
// can collapse the delay; production code never changes it.
var conversionRetryBackoff = 5 * time.Minute

// ConversionJob is one durable row of the unified conversion queue. Jobs
// convert a registered catalog file into another quality profile and publish
// the result as a format variant of the same track.
type ConversionJob struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	FileID      int64  `json:"file_id"`
	Profile     string `json:"profile"`
	State       string `json:"state"`
	Priority    int    `json:"priority"`
	Progress    int    `json:"progress"`
	Attempts    int    `json:"attempts"`
	Error       string `json:"error"`
	CreatedAt   int64  `json:"created_at"`
	UpdatedAt   int64  `json:"updated_at"`
	CompletedAt *int64 `json:"completed_at"`
	// SourcePath is the media-root relative path of the file being converted,
	// recorded when the job is claimed so the queue page can name the work.
	SourcePath string `json:"source_path"`
	// OutputPath is the media-root relative path of the published variant.
	OutputPath string `json:"output_path"`
}

type ConversionJobPage struct {
	Items []ConversionJob `json:"items"`
	Page  int             `json:"page"`
	Total int             `json:"total"`
	Limit int             `json:"limit"`
}

const conversionColumns = `id,kind,file_id,profile,state,priority,progress,attempts,error,created_at,updated_at,completed_at,source_path,output_path`

// ListConversions returns one bounded page of the unified queue, newest job
// first, like ListFiles bounds the catalog page.
func (s *Service) ListConversions(ctx context.Context, page int) (ConversionJobPage, error) {
	if page < 1 || page > 100000 {
		return ConversionJobPage{}, ErrInvalidInput
	}
	const limit = 100
	out := ConversionJobPage{Items: []ConversionJob{}, Page: page, Limit: limit}
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_conversion_jobs`).Scan(&out.Total); err != nil {
		return ConversionJobPage{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+conversionColumns+` FROM music_conversion_jobs ORDER BY id DESC LIMIT ? OFFSET ?`, limit, (page-1)*limit)
	if err != nil {
		return ConversionJobPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		job, err := scanConversionJob(rows)
		if err != nil {
			return ConversionJobPage{}, err
		}
		out.Items = append(out.Items, job)
	}
	return out, rows.Err()
}

func scanConversionJob(row interface{ Scan(...any) error }) (ConversionJob, error) {
	var job ConversionJob
	var completed sql.NullInt64
	if err := row.Scan(&job.ID, &job.Kind, &job.FileID, &job.Profile, &job.State, &job.Priority, &job.Progress,
		&job.Attempts, &job.Error, &job.CreatedAt, &job.UpdatedAt, &completed, &job.SourcePath, &job.OutputPath); err != nil {
		return ConversionJob{}, err
	}
	if completed.Valid {
		value := completed.Int64
		job.CompletedAt = &value
	}
	return job, nil
}

// EnqueueConversion records one conversion request. An active (queued or
// processing) job for the same (kind, file, profile) is returned instead of
// duplicated, so repeated clicks or bulk runs cannot pile up identical work.
func (s *Service) EnqueueConversion(ctx context.Context, kind string, fileID int64, profile string, priority int) (int64, error) {
	switch kind {
	case ConversionKindDownload, ConversionKindLibrary, ConversionKindExport:
	default:
		return 0, ErrInvalidInput
	}
	if fileID < 1 {
		return 0, ErrInvalidInput
	}
	if priority < 1 || priority > 9 {
		return 0, ErrInvalidInput
	}
	profile = strings.TrimSpace(profile)
	// ResolveProfileKey accepts builtin keys and custom profile names and
	// rejects everything else, so the queue never stores an unusable target.
	if _, err := s.ResolveProfileKey(ctx, profile); err != nil {
		return 0, err
	}
	// Only the primary file of a track can be converted: a variant or archive
	// file id has no original left to convert, and with retention='delete'
	// converting it would duplicate the primary or delete the only copy. The
	// row must also exist here, so a deleted file never burns a job's three
	// failed attempts on a ghost job.
	var role string
	err := s.store.db.QueryRowContext(ctx, `SELECT variant_role FROM music_files WHERE id=?`, fileID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, &Error{Code: "music_file_missing", Message: "The registered music file for this conversion is no longer in the catalog."}
	}
	if err != nil {
		return 0, err
	}
	if role != "primary" {
		return 0, &Error{Code: "music_conversion_source", Message: "Only the primary file of a track can be converted; variants and archives are already conversion outputs."}
	}
	result, err := s.store.db.ExecContext(ctx, `INSERT INTO music_conversion_jobs(kind,file_id,profile,state,priority,created_at,updated_at)
		SELECT ?,?,?, 'queued', ?, unixepoch(), unixepoch()
		WHERE NOT EXISTS(SELECT 1 FROM music_conversion_jobs WHERE kind=? AND file_id=? AND profile=? AND state IN ('queued','processing'))`,
		kind, fileID, profile, priority, kind, fileID, profile)
	if err != nil {
		return 0, err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 1 {
		return result.LastInsertId()
	}
	var existing int64
	if err := s.store.db.QueryRowContext(ctx, `SELECT id FROM music_conversion_jobs
		WHERE kind=? AND file_id=? AND profile=? AND state IN ('queued','processing') ORDER BY id LIMIT 1`,
		kind, fileID, profile).Scan(&existing); err != nil {
		return 0, err
	}
	return existing, nil
}

// CancelConversion stops a queued job outright. A processing job is cancelled
// cooperatively: the row is marked cancelled and the worker's claim-guarded
// writes stop matching, so the worker discards its staged output when
// ConvertFile returns and leaves the state cancelled. There are no orphaned
// processes: ConvertFile is bound to the worker context, the conversion
// simply runs to completion (or until shutdown) before the discard happens.
func (s *Service) CancelConversion(ctx context.Context, id int64) error {
	if id < 1 {
		return ErrInvalidInput
	}
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_conversion_jobs
		SET state='cancelled', error='cancellation requested', work_owner='', work_until=0, updated_at=unixepoch()
		WHERE id=? AND state IN ('queued','processing')`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 1 {
		return nil
	}
	var state string
	if err := s.store.db.QueryRowContext(ctx, `SELECT state FROM music_conversion_jobs WHERE id=?`, id).Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return &Error{Code: "music_conversion_state", Message: "Only a queued or processing conversion can be cancelled."}
}

// RetryConversion requeues a failed or cancelled job. The attempt count is
// preserved so a job that keeps failing still reaches its final attempt
// limit instead of looping forever.
func (s *Service) RetryConversion(ctx context.Context, id int64) error {
	if id < 1 {
		return ErrInvalidInput
	}
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_conversion_jobs
		SET state='queued', error='', work_owner='', work_until=0, updated_at=unixepoch()
		WHERE id=? AND state IN ('failed','cancelled')`, id)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 1 {
		return nil
	}
	var state string
	if err := s.store.db.QueryRowContext(ctx, `SELECT state FROM music_conversion_jobs WHERE id=?`, id).Scan(&state); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	return &Error{Code: "music_conversion_state", Message: "Only a failed or cancelled conversion can be retried."}
}

// RunConversions is the durable single-worker loop for the unified queue. It
// claims one queued job at a time from the database (lowest priority number
// first) and never relies on an in-memory queue.
func (s *Service) RunConversions(ctx context.Context) {
	ticker := time.NewTicker(conversionClaimInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		worked, err := s.processConversion(ctx)
		if err != nil && ctx.Err() == nil {
			slog.Default().Error("music conversion worker failed", "error", err)
		}
		if worked && err == nil {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// processConversion claims and runs at most one queued conversion.
func (s *Service) processConversion(ctx context.Context) (bool, error) {
	job, owner, claimed, err := s.claimConversion(ctx)
	if err != nil || !claimed {
		return false, err
	}
	return true, s.runConversion(ctx, job, owner)
}

// claimConversion atomically moves the oldest claimable queued job into the
// processing state and takes a work lease. work_until doubles as the
// not-before time of queued jobs: a failed attempt requeues the row with a
// future work_until so the backoff is durable across restarts.
//
// A 'processing' row whose lease has expired is also claimable: a worker that
// died, or whose lease-renewal or checked progress writes hit a persistent
// database error, would otherwise leave the job wedged in 'processing' until
// the next restart (claimConversion only claims 'queued', and
// RecoverConversions runs only at startup). A live worker renews its lease
// every conversionLeaseSeconds/3, so its work_until is always in the future;
// reclaiming an expired lease counts the interrupted run as an attempt, and
// the old worker's owner-guarded writes stop matching, so it can never
// publish on behalf of a claim it lost.
func (s *Service) claimConversion(ctx context.Context) (ConversionJob, string, bool, error) {
	var id int64
	err := s.store.db.QueryRowContext(ctx, `SELECT id FROM music_conversion_jobs
		WHERE (state='queued' AND work_until<=unixepoch())
		   OR (state='processing' AND work_until<=unixepoch())
		ORDER BY priority,id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ConversionJob{}, "", false, nil
	}
	if err != nil {
		return ConversionJob{}, "", false, err
	}
	owner := workflowOwner()
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_conversion_jobs
		SET state='processing', work_owner=?, work_until=unixepoch()+?, attempts=attempts+1, progress=10, error='',
			source_path=COALESCE((SELECT relative_path FROM music_files WHERE id=music_conversion_jobs.file_id),''),
			updated_at=unixepoch()
		WHERE id=? AND ((state='queued' AND work_until<=unixepoch()) OR (state='processing' AND work_until<=unixepoch()))`,
		owner, conversionLeaseSeconds, id)
	if err != nil {
		return ConversionJob{}, "", false, err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return ConversionJob{}, "", false, nil
	}
	job, err := s.loadConversion(ctx, id)
	if err != nil {
		return ConversionJob{}, "", true, err
	}
	return job, owner, true, nil
}

func (s *Service) loadConversion(ctx context.Context, id int64) (ConversionJob, error) {
	var job ConversionJob
	var completed sql.NullInt64
	err := s.store.db.QueryRowContext(ctx, `SELECT `+conversionColumns+` FROM music_conversion_jobs WHERE id=?`, id).
		Scan(&job.ID, &job.Kind, &job.FileID, &job.Profile, &job.State, &job.Priority, &job.Progress,
			&job.Attempts, &job.Error, &job.CreatedAt, &job.UpdatedAt, &completed, &job.SourcePath, &job.OutputPath)
	if err != nil {
		return ConversionJob{}, err
	}
	if completed.Valid {
		value := completed.Int64
		job.CompletedAt = &value
	}
	return job, nil
}

func (s *Service) renewConversion(ctx context.Context, id int64, owner string) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_conversion_jobs SET work_until=unixepoch()+?,updated_at=unixepoch()
		WHERE id=? AND work_owner=? AND state='processing'`, conversionLeaseSeconds, id, owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errWorkLeaseLost
	}
	return nil
}

// setConversionProgress records the stage of the running job and renews the
// lease in the same guarded statement.
func (s *Service) setConversionProgress(ctx context.Context, id int64, owner string, progress int) error {
	result, err := s.store.db.ExecContext(ctx, `UPDATE music_conversion_jobs SET progress=?,work_until=unixepoch()+?,updated_at=unixepoch()
		WHERE id=? AND work_owner=? AND state='processing'`, progress, conversionLeaseSeconds, id, owner)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		return errWorkLeaseLost
	}
	return nil
}

// conversionStillClaimed reports whether the row is still exclusively ours.
// A cancel or retry clears work_owner, so the worker aborts before publishing
// anything on behalf of a job that no longer wants the result.
func (s *Service) conversionStillClaimed(ctx context.Context, id int64, owner string) (bool, error) {
	var state, held string
	if err := s.store.db.QueryRowContext(ctx, `SELECT state,work_owner FROM music_conversion_jobs WHERE id=?`, id).Scan(&state, &held); err != nil {
		return false, err
	}
	return state == "processing" && held == owner, nil
}

// runConversion converts the claimed file and publishes the output. Every
// state-changing write is guarded by (id, work_owner, state='processing') so
// a cancel or retry decided while the conversion runs is always honored.
func (s *Service) runConversion(ctx context.Context, job ConversionJob, owner string) error {
	base := context.WithoutCancel(ctx)
	// The stage directory is cleaned when this run ends, whatever the
	// outcome: success publishes and removes it, failure cleans it, and a
	// cancelled run discards its staged bytes.
	defer s.removeConversionStage(job.ID)

	// Keep the lease alive while a long conversion runs so a healthy worker
	// is never considered dead; the checked writes below still catch a
	// genuine cancel or retry.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		ticker := time.NewTicker(conversionLeaseSeconds * time.Second / 3)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = s.renewConversion(base, job.ID, owner)
			}
		}
	}()

	profile, err := s.ResolveProfileKey(ctx, job.Profile)
	if err != nil {
		return s.finishConversionFailure(base, job, owner, err)
	}
	src, err := s.conversionSource(ctx, job.FileID)
	if err != nil {
		return s.finishConversionFailure(base, job, owner, err)
	}
	if s.downloadsDir == "" {
		return s.finishConversionFailure(base, job, owner, &Error{Code: "music_import_source", Message: "The downloads root is not configured."})
	}
	stageDir := filepath.Join(s.downloadsDir, "music-conversions", strconv.FormatInt(job.ID, 10))
	if !contained(s.downloadsDir, stageDir) {
		return s.finishConversionFailure(base, job, owner, ErrInvalidInput)
	}
	if err := os.MkdirAll(stageDir, 0o700); err != nil {
		return s.finishConversionFailure(base, job, owner, err)
	}
	ext := conversionFileExtension(profile, path.Ext(src.RelativePath))
	stagedPath := filepath.Join(stageDir, conversionVariantBase(src.RelativePath, job.Profile)+ext)
	result, err := s.ConvertFile(ctx, ConvertRequest{
		SourcePath: filepath.Join(s.mediaDir, filepath.FromSlash(src.RelativePath)),
		OutputPath: stagedPath,
		Profile:    profile,
	})
	if err != nil {
		if ctx.Err() != nil {
			return s.interruptConversion(base, job, owner)
		}
		return s.finishConversionFailure(base, job, owner, err)
	}
	if err := s.setConversionProgress(ctx, job.ID, owner, 60); err != nil {
		return s.abortIfUnclaimed(ctx, job, err)
	}
	// The claim must still be ours before anything is published: a cancel
	// (state='cancelled', owner cleared) or a retry (state='queued', owner
	// cleared) stops this run here, and the deferred cleanup discards the
	// staged output. No orphaned processes: ConvertFile is bound to ctx and
	// has already returned.
	if claimed, err := s.conversionStillClaimed(ctx, job.ID, owner); err != nil {
		return err
	} else if !claimed {
		return nil
	}
	publishedRel, err := s.publishConversion(ctx, src, profile, job.Profile, result, stagedPath)
	if err != nil {
		if ctx.Err() != nil {
			return s.interruptConversion(base, job, owner)
		}
		return s.finishConversionFailure(base, job, owner, err)
	}
	if err := s.applyConversionRetention(base, src, publishedRel, result.SHA256, profile); err != nil {
		return s.finishConversionFailure(base, job, owner, err)
	}
	result2, err := s.store.db.ExecContext(base, `UPDATE music_conversion_jobs
		SET state='completed', progress=100, output_path=?, error='', work_owner='', work_until=0, completed_at=unixepoch(), updated_at=unixepoch()
		WHERE id=? AND work_owner=? AND state='processing'`, publishedRel, job.ID, owner)
	if err != nil {
		return err
	}
	if affected, err := result2.RowsAffected(); err != nil || affected != 1 {
		// A cancel or retry won the race with the publish. The published
		// variant is a verified, valid file and is left in place; the row
		// stays in the state the caller chose.
		return nil
	}
	return nil
}

// abortIfUnclaimed translates a lost lease or a cancel/retry during a guarded
// progress write into a silent end of this run: the row now belongs to
// whoever changed it.
func (s *Service) abortIfUnclaimed(ctx context.Context, job ConversionJob, err error) error {
	if errors.Is(err, errWorkLeaseLost) {
		return nil
	}
	return err
}

// interruptConversion handles a worker shutdown mid-run: the job goes back to
// the queue immediately without counting the interrupted run against the
// retry limit. Recovery after a hard crash is RecoverConversions' job.
func (s *Service) interruptConversion(ctx context.Context, job ConversionJob, owner string) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE music_conversion_jobs
		SET state='queued', error='The conversion was interrupted by a shutdown and will retry.', progress=0, work_owner='', work_until=0, updated_at=unixepoch()
		WHERE id=? AND work_owner=? AND state='processing'`, job.ID, owner)
	return err
}

// finishConversionFailure records an honest failure. Attempts below the limit
// go back to the queue with a durable backoff written into work_until; the
// final attempt fails the job. The source file is never deleted, and the
// caller's deferred cleanup removes the staged output.
func (s *Service) finishConversionFailure(ctx context.Context, job ConversionJob, owner string, runErr error) error {
	var state string
	if err := s.store.db.QueryRowContext(ctx, `SELECT state FROM music_conversion_jobs WHERE id=?`, job.ID).Scan(&state); err == nil && state == "cancelled" {
		// Never overwrite a decided cancellation.
		return nil
	}
	next := "queued"
	until := time.Now().Unix() + int64(conversionRetryBackoff.Seconds())*int64(job.Attempts)
	if job.Attempts >= conversionMaxAttempts {
		next = "failed"
		until = 0
	}
	_, err := s.store.db.ExecContext(ctx, `UPDATE music_conversion_jobs
		SET state=?, error=?, work_owner='', work_until=?, updated_at=unixepoch()
		WHERE id=? AND work_owner=? AND state='processing'`,
		next, conversionFailureMessage(runErr), until, job.ID, owner)
	return err
}

// conversionFailureMessage keeps provider/engine messages intact; ConvertFile
// already redacts paths from ffmpeg output, and raw OS errors are scrubbed
// below so no absolute path is stored in the queue.
func conversionFailureMessage(err error) string {
	var musicErr *Error
	if errors.As(err, &musicErr) && strings.TrimSpace(musicErr.Message) != "" {
		return musicErr.Message
	}
	if err == nil {
		return "The conversion stopped unexpectedly."
	}
	// Raw OS/filesystem errors (mkdir, rename, open) can embed absolute
	// paths; scrub them so the queue page never leaks the server layout.
	return redactPaths(err.Error())
}

// RecoverConversions requeues conversions that were running when the process
// stopped. Each interrupted run counts as an attempt (like the acquisition
// recovery) so a job that cannot make progress still reaches its failure
// limit instead of retrying forever. Staged directories are left in place;
// the next run recreates its stage directory and cleans it afterwards.
func (s *Service) RecoverConversions(ctx context.Context) error {
	_, err := s.store.db.ExecContext(ctx, `UPDATE music_conversion_jobs
		SET state='queued', error='The conversion was interrupted by a restart and will retry.', attempts=attempts+1,
			progress=0, work_owner='', work_until=0, updated_at=unixepoch()
		WHERE state='processing'`)
	return err
}

func (s *Service) removeConversionStage(jobID int64) {
	if s.downloadsDir == "" {
		return
	}
	dir := filepath.Join(s.downloadsDir, "music-conversions", strconv.FormatInt(jobID, 10))
	if !contained(s.downloadsDir, dir) {
		return
	}
	_ = os.RemoveAll(dir)
}

// conversionSource is the registered file a job converts. It is the fence the
// retention policy later checks against: the original is only deleted when
// the row still describes exactly the bytes that were converted.
type conversionSource struct {
	ID, ArtistID, AlbumID, TrackID int64
	Root                           string
	RelativePath                   string
	SizeBytes                      int64
	SHA256                         string
	Format                         string
	Bitrate, SampleRate            int
	Channels, DurationMS           int
}

func (s *Service) conversionSource(ctx context.Context, fileID int64) (conversionSource, error) {
	var src conversionSource
	var track sql.NullInt64
	err := s.store.db.QueryRowContext(ctx, `SELECT id,artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms
		FROM music_files WHERE id=?`, fileID).
		Scan(&src.ID, &src.ArtistID, &src.AlbumID, &track, &src.Root, &src.RelativePath, &src.SizeBytes, &src.SHA256,
			&src.Format, &src.Bitrate, &src.SampleRate, &src.Channels, &src.DurationMS)
	if errors.Is(err, sql.ErrNoRows) {
		return conversionSource{}, &Error{Code: "music_file_missing", Message: "The registered music file for this conversion is no longer in the catalog."}
	}
	if err != nil {
		return conversionSource{}, err
	}
	if track.Valid {
		src.TrackID = track.Int64
	}
	src.RelativePath, err = cleanMediaRelative(src.RelativePath)
	if err != nil {
		return conversionSource{}, err
	}
	return src, nil
}

// cleanMediaRelative applies the same containment rules as OpenAudioFile to a
// catalog relative path before it is used to compose filesystem operations.
func cleanMediaRelative(relative string) (string, error) {
	name := path.Clean(strings.ReplaceAll(strings.TrimSpace(relative), "\\", "/"))
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
		return "", ErrInvalidInput
	}
	return name, nil
}

// conversionFileExtension maps a profile to the output file extension. An
// "original" (or unrecognized) profile keeps the source container, so a copy
// conversion never changes the file name beyond the variant label.
func conversionFileExtension(profile QualityProfile, sourceExt string) string {
	switch profile.Codec {
	case "mp3":
		return ".mp3"
	case "flac":
		return ".flac"
	case "aac":
		if profile.Container == "adts" || profile.Container == "aac" {
			return ".aac"
		}
		return ".m4a"
	case "opus":
		return ".opus"
	default:
		return strings.ToLower(sourceExt)
	}
}

// conversionVariantBase derives the converted file's base name from the
// primary file: the same base name with a " [<profile>]" suffix, e.g.
// "01 Song [opus-128]" for "01 Song". The label is sanitized so a custom
// profile name can never introduce path separators into a filename.
func conversionVariantBase(primaryRelative, label string) string {
	base := path.Base(primaryRelative)
	return strings.TrimSuffix(base, path.Ext(base)) + " [" + safeVariantLabel(label) + "]"
}

func conversionVariantPath(primaryRelative, label, ext string) (string, error) {
	clean, err := cleanMediaRelative(primaryRelative)
	if err != nil {
		return "", err
	}
	return path.Join(path.Dir(clean), conversionVariantBase(clean, label)+ext), nil
}

func safeVariantLabel(label string) string {
	out := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' {
			return '-'
		}
		if r < 32 || r == 127 {
			return '-'
		}
		return r
	}, strings.TrimSpace(label))
	if out == "" {
		return "custom"
	}
	return out
}

// publishConversion publishes a verified, staged conversion output as a
// variant of the source track, following importFile's verify-then-register
// order: probe the staged bytes, place them inside the media root, verify the
// digest at the final path, then commit the catalog row. The published
// relative path is returned.
//
// The variant lives in the same directory as the primary file with a
// " [<profile>]" filename suffix. An existing variant row for the same track
// and profile is replaced in place (journal style: stage, verify, swap,
// delete old) — there is never a second row for one (track, profile).
// optimize.go reuses this function for library bulk conversions.
func (s *Service) publishConversion(ctx context.Context, src conversionSource, profile QualityProfile, label string, result ConvertResult, stagedPath string) (string, error) {
	media, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return "", err
	}
	defer media.Close()
	ext := conversionFileExtension(profile, path.Ext(src.RelativePath))
	targetRel, err := conversionVariantPath(src.RelativePath, label, ext)
	if err != nil {
		return "", err
	}
	// Re-probe the staged bytes so the publish only registers output that is
	// decodable right now, mirroring importFile's verification order.
	// ConvertFile already validated codec and duration; this confirms the
	// staged file was not replaced between conversion and publication.
	if _, err := s.prober.ProbeAudio(ctx, stagedPath); err != nil {
		return "", &Error{Code: "music_output_invalid", Message: "The converted audio file is not decodable, so it was discarded."}
	}
	existing, err := s.variantFileRow(ctx, src, label)
	if err != nil {
		return "", err
	}
	if existing != nil {
		// Replace the existing variant of this profile in place.
		targetRel = existing.RelativePath
	}

	var asideRel string
	needCopy := true
	if info, err := media.Lstat(targetRel); err == nil {
		if !info.Mode().IsRegular() {
			return "", &Error{Code: "music_variant_conflict", Message: "The variant path is occupied by a non-regular file. Nothing was overwritten."}
		}
		sha, _, derr := musicDigest(ctx, media, targetRel)
		if derr == nil && sha == result.SHA256 {
			// The destination already holds exactly these bytes (a publish
			// that was interrupted after the rename): deduplicate instead of
			// copying again.
			needCopy = false
		} else if existing == nil {
			return "", &Error{Code: "music_variant_conflict", Message: "A different file already exists at the variant path. Nothing was overwritten; remove it or choose another profile."}
		}
	}
	if needCopy {
		var token [16]byte
		if _, err := rand.Read(token[:]); err != nil {
			return "", err
		}
		stageRel := path.Join(path.Dir(targetRel), ".reelscarey-convert-"+hex.EncodeToString(token[:])+ext)
		_ = media.Remove(stageRel) // a stale stage from an interrupted attempt
		if err := copyIntoMediaRoot(ctx, media, stagedPath, stageRel); err != nil {
			return "", err
		}
		sha, _, err := musicDigest(ctx, media, stageRel)
		if err != nil {
			_ = media.Remove(stageRel)
			return "", err
		}
		if sha != result.SHA256 {
			_ = media.Remove(stageRel)
			return "", errors.New("The staged conversion output changed before publication.")
		}
		if _, err := media.Lstat(targetRel); err == nil {
			// Replacement: move the current occupant aside only after the
			// new bytes are staged and verified, then swap.
			asideRel = path.Join(path.Dir(targetRel), ".reelscarey-variant-old-"+hex.EncodeToString(token[:])+ext)
			_ = media.Remove(asideRel)
			if err := media.Rename(targetRel, asideRel); err != nil {
				_ = media.Remove(stageRel)
				return "", err
			}
		}
		if err := media.Rename(stageRel, targetRel); err != nil {
			if asideRel != "" {
				_ = media.Rename(asideRel, targetRel)
			}
			_ = media.Remove(stageRel)
			return "", err
		}
		// fsync ordering: the published bytes were fsync'd while staged and
		// the rename is atomic within the media root; the catalog commit
		// below happens strictly after the verified bytes sit at the final
		// path, so a committed row always describes a readable file.
		if err := expectMusicDigest(ctx, media, targetRel, result.SHA256); err != nil {
			if asideRel != "" {
				_ = media.Rename(asideRel, targetRel)
			}
			return "", err
		}
	}

	tagJSON, err := json.Marshal(result.Tags)
	if err != nil {
		return "", err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if existing != nil {
		result2, err := tx.ExecContext(ctx, `UPDATE music_files
			SET size_bytes=?, sha256=?, format=?, bitrate=?, sample_rate=?, channels=?, duration_ms=?, tag_json=?, imported_at=unixepoch()
			WHERE id=? AND variant_role='variant'`,
			result.SizeBytes, result.SHA256, result.Tags.Format, result.Tags.Bitrate, result.Tags.SampleRate,
			result.Tags.Channels, result.Tags.DurationMS, string(tagJSON), existing.ID)
		if err != nil {
			return "", err
		}
		if affected, err := result2.RowsAffected(); err != nil || affected != 1 {
			return "", &Error{Code: "music_variant_conflict", Message: "The variant changed while the conversion was publishing it. Nothing was overwritten."}
		}
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_files(artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,bitrate,sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name,variant_role,variant_profile)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,unixepoch(),'conversion',?,'variant',?)`,
			src.ArtistID, src.AlbumID, nullableID(src.TrackID), src.Root, targetRel, result.SizeBytes, result.SHA256,
			result.Tags.Format, result.Tags.Bitrate, result.Tags.SampleRate, result.Tags.Channels, result.Tags.DurationMS,
			string(tagJSON), path.Base(src.RelativePath), label); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	// Cleanup happens only after the catalog commit.
	if asideRel != "" {
		_ = media.Remove(asideRel)
	}
	return targetRel, nil
}

type conversionVariantRow struct {
	ID           int64
	RelativePath string
}

// variantFileRow finds the existing variant row for the same track and
// profile. music_files has no parent-file column, so track identity (falling
// back to artist+album for files without a catalog track) is the dedup key:
// one variant row per profile, never duplicates.
func (s *Service) variantFileRow(ctx context.Context, src conversionSource, label string) (*conversionVariantRow, error) {
	var row conversionVariantRow
	var err error
	if src.TrackID > 0 {
		err = s.store.db.QueryRowContext(ctx, `SELECT id,relative_path FROM music_files
			WHERE variant_role='variant' AND variant_profile=? AND track_id=? ORDER BY id LIMIT 1`, label, src.TrackID).
			Scan(&row.ID, &row.RelativePath)
	} else {
		err = s.store.db.QueryRowContext(ctx, `SELECT id,relative_path FROM music_files
			WHERE variant_role='variant' AND variant_profile=? AND track_id IS NULL AND artist_id=? AND album_id=? ORDER BY id LIMIT 1`,
			label, src.ArtistID, src.AlbumID).
			Scan(&row.ID, &row.RelativePath)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if clean, cleanErr := cleanMediaRelative(row.RelativePath); cleanErr == nil {
		row.RelativePath = clean
	}
	return &row, nil
}

// copyIntoMediaRoot copies a staged file from outside the media root into an
// exclusive path inside it and fsyncs the bytes before the caller digests
// them.
func copyIntoMediaRoot(ctx context.Context, media *os.Root, sourcePath, targetRel string) error {
	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := media.OpenFile(targetRel, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, musicReader{ctx, in})
	return errors.Join(copyErr, out.Sync(), out.Close())
}

// applyConversionRetention enforces the original-retention policy after a
// verified variant publication. The original row and file are deleted only
// when the profile drops originals, the saved preference is 'delete', the
// variant row is committed and the converted file is verified on disk, and
// the original row still describes exactly the bytes that were converted.
// Any other case keeps both files. Deletion order: catalog rows first (the
// variant is promoted to the track's primary file in the same transaction),
// then the file on disk — a crash in between leaves an unregistered copy
// that a rescan re-registers, never a catalog row pointing at a missing file.
func (s *Service) applyConversionRetention(ctx context.Context, src conversionSource, variantRel, expectedSHA string, profile QualityProfile) error {
	if profile.KeepOriginal || profile.Codec == "original" {
		return nil
	}
	prefs, err := s.GetPreferences(ctx)
	if err != nil {
		// An unreadable preference row never authorizes a deletion: keep
		// both files and let the next conversion try again.
		return nil
	}
	if prefs.OriginalRetention != "delete" {
		return nil
	}
	media, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return err
	}
	defer media.Close()
	// The published variant must still be exactly the converted bytes.
	if err := expectMusicDigest(ctx, media, variantRel, expectedSHA); err != nil {
		return errors.New("The published conversion could not be verified on disk, so the original file was kept.")
	}
	// Fence the original row against concurrent edits.
	var rel, sha string
	err = s.store.db.QueryRowContext(ctx, `SELECT relative_path,sha256 FROM music_files WHERE id=?`, src.ID).Scan(&rel, &sha)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // already gone; nothing to delete
	}
	if err != nil {
		return err
	}
	if rel != src.RelativePath || sha != src.SHA256 {
		// The original changed after the conversion claimed it. Both files
		// are kept; the variant stays a variant.
		return nil
	}
	if err := expectMusicDigest(ctx, media, rel, sha); err != nil {
		// The bytes on disk no longer match the catalog row; deleting them
		// could destroy data the catalog does not describe. Keep both.
		return nil
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `DELETE FROM music_files WHERE id=? AND relative_path=? AND sha256=?`, src.ID, rel, sha)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		return &Error{Code: "music_variant_conflict", Message: "The original file changed while the conversion finished. The original was kept."}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_files SET variant_role='primary',variant_profile='' WHERE relative_path=? AND variant_role='variant'`, variantRel); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := media.Remove(rel); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Default().Warn("music conversion could not delete the replaced original file", "error", err)
	}
	return nil
}
