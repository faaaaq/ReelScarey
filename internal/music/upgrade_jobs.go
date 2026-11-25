package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

const (
	upgradeRatePerHour = 200
	upgradePageLimit   = 100
	upgradeMaxAttempts = 3
)

// UpgradeJobInput selects canonical primary files for a source-quality
// upgrade. RatePerHour is deliberately not an input: the worker enforces the
// global 200-attempts/hour limit and the browser cannot relax it.
type UpgradeJobInput struct {
	Scope          string `json:"scope"`
	ScopeID        int64  `json:"scope_id"`
	TargetQuality  string `json:"target_quality"`
	MaxBitrateKbps int    `json:"max_bitrate_kbps"`
}

// UpgradeJob is the public queue projection. Lease identities, selected
// provider IDs/URLs, local paths and publication journals remain private.
type UpgradeJob struct {
	ID             int64  `json:"id"`
	Scope          string `json:"scope"`
	ScopeID        int64  `json:"scope_id"`
	ScopeName      string `json:"scope_name"`
	TargetQuality  string `json:"target_quality"`
	MaxBitrateKbps int    `json:"max_bitrate_kbps"`
	RatePerHour    int    `json:"rate_per_hour"`
	State          string `json:"state"`
	TotalCount     int    `json:"total_count"`
	ProcessedCount int    `json:"processed_count"`
	UpgradedCount  int    `json:"upgraded_count"`
	SkippedCount   int    `json:"skipped_count"`
	FailedCount    int    `json:"failed_count"`
	CurrentTrack   string `json:"current_track"`
	Error          string `json:"error"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
}

type UpgradeJobPage struct {
	Items []UpgradeJob `json:"items"`
	Page  int          `json:"page"`
	Total int          `json:"total"`
	Limit int          `json:"limit"`
}

const upgradeJobColumns = `id,scope,scope_id,scope_name,target_quality,max_bitrate_kbps,rate_per_hour,state,
	total_count,processed_count,upgraded_count,skipped_count,failed_count,current_track,error,created_at,updated_at`

type upgradeRowScanner interface{ Scan(...any) error }

func scanUpgradeJob(row upgradeRowScanner) (UpgradeJob, error) {
	var job UpgradeJob
	err := row.Scan(&job.ID, &job.Scope, &job.ScopeID, &job.ScopeName, &job.TargetQuality, &job.MaxBitrateKbps,
		&job.RatePerHour, &job.State, &job.TotalCount, &job.ProcessedCount, &job.UpgradedCount, &job.SkippedCount,
		&job.FailedCount, &job.CurrentTrack, &job.Error, &job.CreatedAt, &job.UpdatedAt)
	return job, err
}

// UpgradeJob returns one public job without its private lease or item source
// evidence. Missing rows use the music package's normal ErrNotFound contract.
func (s *Service) UpgradeJob(ctx context.Context, id int64) (UpgradeJob, error) {
	if id < 1 {
		return UpgradeJob{}, ErrInvalidInput
	}
	job, err := scanUpgradeJob(s.store.db.QueryRowContext(ctx, `SELECT `+upgradeJobColumns+` FROM music_upgrade_jobs WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return UpgradeJob{}, ErrNotFound
	}
	return job, err
}

func (s *Service) ListUpgradeJobs(ctx context.Context, page int) (UpgradeJobPage, error) {
	if page < 1 || page > 100000 {
		return UpgradeJobPage{}, ErrInvalidInput
	}
	out := UpgradeJobPage{Items: []UpgradeJob{}, Page: page, Limit: upgradePageLimit}
	if err := s.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM music_upgrade_jobs`).Scan(&out.Total); err != nil {
		return UpgradeJobPage{}, err
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT `+upgradeJobColumns+` FROM music_upgrade_jobs ORDER BY id DESC LIMIT ? OFFSET ?`, upgradePageLimit, (page-1)*upgradePageLimit)
	if err != nil {
		return UpgradeJobPage{}, err
	}
	defer rows.Close()
	for rows.Next() {
		job, err := scanUpgradeJob(rows)
		if err != nil {
			return UpgradeJobPage{}, err
		}
		out.Items = append(out.Items, job)
	}
	return out, rows.Err()
}

type upgradeItem struct {
	ID, JobID, TrackID, MusicFileID int64
	ArtistID, AlbumID               int64
	Root, RelativePath, SHA256      string
	Title, Artist, Album            string
	DurationMS                      int
	OldBitrate                      int
	OldFormat                       string
	OldSampleRate                   int
	State, Detail                   string
	Attempts                        int
	NotBefore                       int64
	Publication                     string
}

const upgradeItemColumns = `id,job_id,track_id,music_file_id,artist_id,album_id,old_root,old_relative_path,old_sha256,
	title,artist,album,duration_ms,old_bitrate,old_format,old_sample_rate,state,detail,attempts,not_before,publication`

func scanUpgradeItem(row upgradeRowScanner) (upgradeItem, error) {
	var item upgradeItem
	err := row.Scan(&item.ID, &item.JobID, &item.TrackID, &item.MusicFileID, &item.ArtistID, &item.AlbumID,
		&item.Root, &item.RelativePath, &item.SHA256, &item.Title, &item.Artist, &item.Album, &item.DurationMS,
		&item.OldBitrate, &item.OldFormat, &item.OldSampleRate, &item.State, &item.Detail, &item.Attempts,
		&item.NotBefore, &item.Publication)
	return item, err
}

func upgradeStateError(action string) error {
	return &Error{Code: "music_upgrade_state", Message: "Only a queued or running upgrade can be cancelled; only a failed or cancelled upgrade can be retried (" + action + ")."}
}

func validateUpgradeInput(ctx context.Context, db *sql.DB, input UpgradeJobInput) (UpgradeJobInput, string, error) {
	input.Scope = strings.ToLower(strings.TrimSpace(input.Scope))
	input.TargetQuality = strings.ToLower(strings.TrimSpace(input.TargetQuality))
	if input.TargetQuality != "high" && input.TargetQuality != "lossless" && input.TargetQuality != "hires" || input.MaxBitrateKbps < 0 {
		return input, "", ErrInvalidInput
	}
	// Keep max_bitrate_kbps*1000 representable when the candidate cap is
	// evaluated. A zero cap means unlimited; a positive cap applies only when
	// the downloaded stream has a known bitrate.
	maxInt := int(^uint(0) >> 1)
	if input.MaxBitrateKbps > maxInt/1000 {
		return input, "", ErrInvalidInput
	}
	var name string
	var err error
	switch input.Scope {
	case "library":
		if input.ScopeID != 0 {
			return input, "", ErrInvalidInput
		}
		name = "Library"
	case "artist":
		if input.ScopeID < 1 {
			return input, "", ErrInvalidInput
		}
		err = db.QueryRowContext(ctx, `SELECT name FROM music_artists WHERE id=?`, input.ScopeID).Scan(&name)
	case "album":
		if input.ScopeID < 1 {
			return input, "", ErrInvalidInput
		}
		err = db.QueryRowContext(ctx, `SELECT title FROM music_albums WHERE id=?`, input.ScopeID).Scan(&name)
	case "track":
		if input.ScopeID < 1 {
			return input, "", ErrInvalidInput
		}
		err = db.QueryRowContext(ctx, `SELECT title FROM music_tracks WHERE id=?`, input.ScopeID).Scan(&name)
	default:
		return input, "", ErrInvalidInput
	}
	if errors.Is(err, sql.ErrNoRows) {
		return input, "", ErrNotFound
	}
	if err != nil {
		return input, "", err
	}
	return input, name, nil
}

// EnqueueUpgradeJob expands the scope and atomically records the job, all
// immutable item snapshots and active-overlap guards. Multiple primaries for
// one canonical track are rejected instead of resolved by search order.
func (s *Service) EnqueueUpgradeJob(ctx context.Context, input UpgradeJobInput) (UpgradeJob, error) {
	input, scopeName, err := validateUpgradeInput(ctx, s.store.db, input)
	if err != nil {
		return UpgradeJob{}, err
	}
	defer organization.ReadMedia()()
	s.fileMu.Lock()
	defer s.fileMu.Unlock()

	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return UpgradeJob{}, err
	}
	defer tx.Rollback()
	where := `f.variant_role='primary' AND f.track_id IS NOT NULL`
	args := []any{}
	switch input.Scope {
	case "artist":
		where += ` AND ar.id=?`
		args = append(args, input.ScopeID)
	case "album":
		where += ` AND a.id=?`
		args = append(args, input.ScopeID)
	case "track":
		where += ` AND t.id=?`
		args = append(args, input.ScopeID)
	}
	rows, err := tx.QueryContext(ctx, `SELECT f.id,t.id,a.artist_id,a.id,f.root,f.relative_path,f.sha256,t.title,ar.name,a.title,
		f.duration_ms,f.bitrate,f.format,f.sample_rate
		FROM music_files f JOIN music_tracks t ON t.id=f.track_id
		JOIN music_albums a ON a.id=t.album_id JOIN music_artists ar ON ar.id=a.artist_id
		WHERE `+where+` ORDER BY t.id,f.id`, args...)
	if err != nil {
		return UpgradeJob{}, err
	}
	items := []upgradeItem{}
	perTrack := map[int64]int{}
	for rows.Next() {
		var item upgradeItem
		if err := rows.Scan(&item.MusicFileID, &item.TrackID, &item.ArtistID, &item.AlbumID, &item.Root,
			&item.RelativePath, &item.SHA256, &item.Title, &item.Artist, &item.Album, &item.DurationMS,
			&item.OldBitrate, &item.OldFormat, &item.OldSampleRate); err != nil {
			rows.Close()
			return UpgradeJob{}, err
		}
		item.RelativePath, err = cleanMediaRelative(item.RelativePath)
		if err != nil {
			rows.Close()
			return UpgradeJob{}, err
		}
		item.SHA256 = strings.ToLower(strings.TrimSpace(item.SHA256))
		item.State = "pending"
		perTrack[item.TrackID]++
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return UpgradeJob{}, err
	}
	rows.Close()
	for trackID, count := range perTrack {
		if count != 1 {
			return UpgradeJob{}, &Error{Code: "music_upgrade_ambiguous", Message: fmt.Sprintf("Track %d has multiple primary files. Choose exactly one primary before upgrading it.", trackID)}
		}
	}
	if len(items) == 0 {
		return UpgradeJob{}, &Error{Code: "music_upgrade_empty", Message: "The selected scope has no canonical primary music files to upgrade."}
	}

	// Older registered rows may not have a digest. Fill only the immutable
	// snapshot from current bytes; never rewrite the catalog row here.
	var media *os.Root
	for i := range items {
		if items[i].SHA256 != "" {
			continue
		}
		if _, _, err := s.safeMediaPath(items[i].RelativePath); err != nil {
			if media != nil {
				media.Close()
			}
			return UpgradeJob{}, err
		}
		if media == nil {
			media, err = os.OpenRoot(s.mediaDir)
			if err != nil {
				return UpgradeJob{}, err
			}
		}
		sha, _, digestErr := musicDigest(ctx, media, items[i].RelativePath)
		if digestErr != nil {
			media.Close()
			return UpgradeJob{}, &Error{Code: "music_upgrade_changed", Message: "An original music file could not be verified while creating the upgrade."}
		}
		items[i].SHA256 = sha
	}
	if media != nil {
		if err := media.Close(); err != nil {
			return UpgradeJob{}, err
		}
	}
	for _, item := range items {
		var overlap int64
		err := tx.QueryRowContext(ctx, `SELECT j.id FROM music_upgrade_items i JOIN music_upgrade_jobs j ON j.id=i.job_id
			WHERE i.music_file_id=? AND j.state IN ('queued','running') ORDER BY j.id LIMIT 1`, item.MusicFileID).Scan(&overlap)
		if err == nil {
			return UpgradeJob{}, &Error{Code: "music_upgrade_overlap", Message: fmt.Sprintf("Upgrade job %d already contains one of these original files.", overlap)}
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return UpgradeJob{}, err
		}
	}

	now := time.Now().Unix()
	result, err := tx.ExecContext(ctx, `INSERT INTO music_upgrade_jobs(scope,scope_id,scope_name,target_quality,max_bitrate_kbps,rate_per_hour,state,total_count,created_at,updated_at)
		VALUES(?,?,?,?,?,200,'queued',?,?,?)`, input.Scope, input.ScopeID, scopeName, input.TargetQuality, input.MaxBitrateKbps, len(items), now, now)
	if err != nil {
		return UpgradeJob{}, err
	}
	jobID, err := result.LastInsertId()
	if err != nil {
		return UpgradeJob{}, err
	}
	for _, item := range items {
		result, err := tx.ExecContext(ctx, `INSERT INTO music_upgrade_items(job_id,track_id,music_file_id,artist_id,album_id,old_root,old_relative_path,old_sha256,
			title,artist,album,duration_ms,old_bitrate,old_format,old_sample_rate,state,created_at,updated_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'pending',?,?)`, jobID, item.TrackID, item.MusicFileID, item.ArtistID, item.AlbumID,
			item.Root, item.RelativePath, item.SHA256, item.Title, item.Artist, item.Album, item.DurationMS, item.OldBitrate,
			item.OldFormat, item.OldSampleRate, now, now)
		if err != nil {
			return UpgradeJob{}, err
		}
		itemID, err := result.LastInsertId()
		if err != nil {
			return UpgradeJob{}, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_upgrade_active(music_file_id,job_id,item_id) VALUES(?,?,?)`, item.MusicFileID, jobID, itemID); err != nil {
			if strings.Contains(err.Error(), "music_upgrade_active") {
				return UpgradeJob{}, &Error{Code: "music_upgrade_overlap", Message: "Another active upgrade already contains one of these original files."}
			}
			return UpgradeJob{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return UpgradeJob{}, err
	}
	return s.UpgradeJob(ctx, jobID)
}

func (s *Service) CancelUpgradeJob(ctx context.Context, id int64) (UpgradeJob, error) {
	if id < 1 {
		return UpgradeJob{}, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return UpgradeJob{}, err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE music_upgrade_jobs SET state='cancelled',current_track='',error='Cancellation requested.',lease_owner='',lease_until=0,updated_at=unixepoch()
		WHERE id=? AND state IN ('queued','running')`, id)
	if err != nil {
		return UpgradeJob{}, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		var state string
		if scanErr := tx.QueryRowContext(ctx, `SELECT state FROM music_upgrade_jobs WHERE id=?`, id).Scan(&state); errors.Is(scanErr, sql.ErrNoRows) {
			return UpgradeJob{}, ErrNotFound
		} else if scanErr != nil {
			return UpgradeJob{}, scanErr
		}
		return UpgradeJob{}, upgradeStateError("cancel")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_items SET state='pending',not_before=0,updated_at=unixepoch()
		WHERE job_id=? AND state IN ('searching','downloading')`, id); err != nil {
		return UpgradeJob{}, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_upgrade_active WHERE job_id=?`, id); err != nil {
		return UpgradeJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return UpgradeJob{}, err
	}
	return s.UpgradeJob(ctx, id)
}

// RetryUpgradeJob retries only unfinished and failed items. Upgraded/skipped
// items, selected source IDs/URLs, candidate evidence and crash journals are
// retained. A fresh three-attempt budget makes a manual retry useful after an
// exhausted automatic retry sequence.
func (s *Service) RetryUpgradeJob(ctx context.Context, id int64) (UpgradeJob, error) {
	if id < 1 {
		return UpgradeJob{}, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return UpgradeJob{}, err
	}
	defer tx.Rollback()
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM music_upgrade_jobs WHERE id=?`, id).Scan(&state); errors.Is(err, sql.ErrNoRows) {
		return UpgradeJob{}, ErrNotFound
	} else if err != nil {
		return UpgradeJob{}, err
	}
	if state != "failed" && state != "cancelled" {
		return UpgradeJob{}, upgradeStateError("retry")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,music_file_id FROM music_upgrade_items WHERE job_id=? AND state IN ('pending','searching','downloading','failed') ORDER BY id`, id)
	if err != nil {
		return UpgradeJob{}, err
	}
	type retryItem struct{ id, fileID int64 }
	items := []retryItem{}
	for rows.Next() {
		var item retryItem
		if err := rows.Scan(&item.id, &item.fileID); err != nil {
			rows.Close()
			return UpgradeJob{}, err
		}
		items = append(items, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return UpgradeJob{}, err
	}
	for _, item := range items {
		var overlap int64
		err := tx.QueryRowContext(ctx, `SELECT j.id FROM music_upgrade_items i JOIN music_upgrade_jobs j ON j.id=i.job_id
			WHERE i.music_file_id=? AND i.job_id<>? AND j.state IN ('queued','running') LIMIT 1`, item.fileID, id).Scan(&overlap)
		if err == nil {
			return UpgradeJob{}, &Error{Code: "music_upgrade_overlap", Message: fmt.Sprintf("Upgrade job %d already contains one of these original files.", overlap)}
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return UpgradeJob{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_upgrade_active WHERE job_id=?`, id); err != nil {
		return UpgradeJob{}, err
	}
	for _, item := range items {
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_upgrade_active(music_file_id,job_id,item_id) VALUES(?,?,?)`, item.fileID, id, item.id); err != nil {
			if strings.Contains(err.Error(), "music_upgrade_active") {
				return UpgradeJob{}, &Error{Code: "music_upgrade_overlap", Message: "Another active upgrade already contains one of these original files."}
			}
			return UpgradeJob{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_items SET state='pending',detail='',attempts=0,not_before=0,last_attempt_at=0,updated_at=unixepoch()
		WHERE job_id=? AND state IN ('pending','searching','downloading','failed')`, id); err != nil {
		return UpgradeJob{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE music_upgrade_jobs SET state='queued',current_track='',error='',lease_owner='',lease_until=0,
		failed_count=0,processed_count=upgraded_count+skipped_count,updated_at=unixepoch() WHERE id=?`, id); err != nil {
		return UpgradeJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return UpgradeJob{}, err
	}
	return s.UpgradeJob(ctx, id)
}
