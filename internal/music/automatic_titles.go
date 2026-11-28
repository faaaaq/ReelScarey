package music

import (
	"context"
	"database/sql"
	"errors"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

// The automatic job writes canonical tags as a group. Preserve manual choices
// about those tags, including the option to save only to the catalog.
const automaticMusicTagsAllowed = `ar.name_overridden=0 AND a.title_overridden=0 AND a.year_overridden=0
	AND json_type(a.editor_fields,'$.album_artist') IS NULL
	AND json_remove(COALESCE(t.editor_fields,'{}'),'$.genre','$.comment')='{}'`

func (s *Service) processTitleUpdate(ctx context.Context) (bool, error) {
	var id, revision int64
	var allowed bool
	err := s.store.db.QueryRowContext(ctx, `SELECT u.music_file_id,u.revision,`+automaticMusicTagsAllowed+`
		FROM music_title_updates u JOIN music_files f ON f.id=u.music_file_id
		JOIN music_albums a ON a.id=f.album_id JOIN music_artists ar ON ar.id=f.artist_id
		LEFT JOIN music_tracks t ON t.id=f.track_id
		WHERE u.next_attempt<=unixepoch()
		  AND NOT EXISTS(SELECT 1 FROM music_edit_files f JOIN music_edit_jobs j ON j.id=f.job_id WHERE f.music_file_id=u.music_file_id AND j.state='running')
		  AND NOT EXISTS(SELECT 1 FROM music_conversion_jobs c WHERE c.file_id=u.music_file_id AND c.kind<>'download' AND c.state IN ('queued','processing'))
		ORDER BY u.next_attempt,u.music_file_id LIMIT 1`).Scan(&id, &revision, &allowed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !allowed {
		_, err = s.store.db.ExecContext(ctx, `DELETE FROM music_title_updates WHERE music_file_id=? AND revision=?`, id, revision)
		return true, err
	}
	plan, err := s.PreviewEdits(ctx, []int64{id})
	var typed *Error
	if errors.As(err, &typed) && typed.Code == "music_no_track" {
		_, err = s.store.db.ExecContext(ctx, `DELETE FROM music_title_updates WHERE music_file_id=? AND revision=?`, id, revision)
		return true, err
	}
	if err != nil {
		if ctx.Err() == nil {
			_, _ = s.store.db.ExecContext(ctx, `UPDATE music_title_updates SET next_attempt=unixepoch()+300 WHERE music_file_id=? AND revision=?`, id, revision)
		}
		return false, err
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if err = organization.EnsureMediaAvailableTx(ctx, tx); err != nil {
		return false, nil
	}
	if len(plan.Files) == 0 {
		_, err = tx.ExecContext(ctx, `DELETE FROM music_title_updates WHERE music_file_id=? AND revision=?`, id, revision)
		if err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	file := plan.Files[0]
	if !needsRetag(file.PreviousTagJSON, file.Tags) {
		_, err = tx.ExecContext(ctx, `DELETE FROM music_title_updates WHERE music_file_id=? AND revision=?`, id, revision)
		if err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	var current, path, tagJSON string
	var active int
	err = tx.QueryRowContext(ctx, `SELECT `+trackDisplayTitle+`,f.relative_path,f.tag_json,`+automaticMusicTagsAllowed+`,
		(SELECT COUNT(*) FROM music_edit_files e JOIN music_edit_jobs j ON j.id=e.job_id WHERE e.music_file_id=f.id AND j.state='running')
		FROM music_files f JOIN music_tracks t ON t.id=f.track_id
		JOIN music_albums a ON a.id=f.album_id JOIN music_artists ar ON ar.id=f.artist_id
		WHERE f.id=?`, id).Scan(&current, &path, &tagJSON, &allowed, &active)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (!allowed || current != file.Tags.Title || path != file.SourcePath || tagJSON != file.PreviousTagJSON || active > 0) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	file.Action, file.DestinationPath = "retag", file.SourcePath
	job, err := persistEditJob(ctx, tx, []EditFile{file}, true)
	if err != nil {
		return false, err
	}
	// Delay a retry while the durable edit runs. A new match/title queues
	// another revision, which survives completion of this one.
	if _, err = tx.ExecContext(ctx, `UPDATE music_title_updates SET next_attempt=unixepoch()+300 WHERE music_file_id=? AND revision=?`, id, revision); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	s.runEditJob(ctx, job)
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	var state string
	if err = s.store.db.QueryRowContext(ctx, `SELECT state FROM music_edit_jobs WHERE id=?`, job).Scan(&state); err != nil {
		return true, err
	}
	if state != "completed" {
		return true, nil
	}
	_, err = s.store.db.ExecContext(ctx, `DELETE FROM music_title_updates WHERE music_file_id=? AND revision=?`, id, revision)
	return true, err
}
