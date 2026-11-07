package transfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// RemoveEpisodeFile is the reviewed per-episode removal. With deleteFile it
// removes the library registration and the media copy, staging the file beside
// itself first so a failed commit can be undone; without it the file stays on
// disk and only the episode association is dropped. A file shared by several
// episodes is never deleted from under the others, and a file that a tracked
// replacement still depends on is refused. The download folder (seeding
// source) is never touched.
func (s *Service) RemoveEpisodeFile(ctx context.Context, seriesID, revision, episodeID int64, deleteFile bool) error {
	if revision < 1 || episodeID < 1 {
		return problem("invalid_selection", "Reload this show and try again.")
	}
	var (
		fileID           int64
		relative, origin string
		shared           int
		title            string
	)
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = tx.QueryRowContext(ctx, `SELECT title FROM media_items WHERE id=? AND kind='series'`, seriesID).Scan(&title); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return problem("not_found", "This show is not in the saved library.")
		}
		return e
	}
	e = tx.QueryRowContext(ctx, `SELECT mf.id,mf.relative_path,mf.origin,
		(SELECT COUNT(*) FROM episode_files x WHERE x.media_file_id=mf.id)
		FROM episodes e JOIN seasons s ON s.id=e.season_id
		JOIN episode_files ef ON ef.episode_id=e.id JOIN media_files mf ON mf.id=ef.media_file_id
		WHERE e.id=? AND s.series_id=?`, episodeID, seriesID).Scan(&fileID, &relative, &origin, &shared)
	if errors.Is(e, sql.ErrNoRows) {
		return problem("no_episode_file", "This episode has no registered file to remove.")
	}
	if e != nil {
		return e
	}
	if deleteFile && shared > 1 {
		return problem("shared_file", fmt.Sprintf("This file also serves %d other episodes. Remove those episodes from the library first.", shared-1))
	}
	var replacements int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements WHERE media_file_id=?`, fileID).Scan(&replacements); e != nil {
		return e
	}
	if deleteFile && replacements > 0 {
		return problem("replacement_pending", "A tracked replacement still depends on this file. Finish or cancel it before deleting the file.")
	}
	r, e := tx.ExecContext(ctx, `UPDATE series_details SET revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE media_id=? AND revision=?`, seriesID, revision)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n != 1 {
		return problem("revision_conflict", "This show changed. Reload and review it again.")
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM episode_files WHERE episode_id=?`, episodeID); e != nil {
		return e
	}
	staged := ""
	if deleteFile {
		if _, e = tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); e != nil {
			return e
		}
		for _, table := range []string{"subtitle_jobs", "subtitle_candidates", "subtitle_inspections", "subtitles", "subtitle_imports", "subtitle_edits"} {
			if _, e = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE media_file_id=?`, fileID); e != nil {
				return e
			}
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM youtube_files WHERE media_file_id=?`, fileID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE library_scan_files SET media_file_id=NULL WHERE media_file_id=?`, fileID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM media_files WHERE id=?`, fileID); e != nil {
			return e
		}
		if staged, _, e = s.stageMediaRemoval(relative); e != nil {
			return e
		}
	}
	state := "episode removed from the library; file kept on disk"
	if deleteFile {
		state = "episode and its file deleted"
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',?,?,?)`, seriesID, title, state); e != nil {
		if staged != "" {
			_ = os.Rename(staged, filepath.Join(s.cfg.MediaDir, filepath.FromSlash(relative)))
		}
		return e
	}
	if e = tx.Commit(); e != nil {
		if staged != "" {
			_ = os.Rename(staged, filepath.Join(s.cfg.MediaDir, filepath.FromSlash(relative)))
		}
		return e
	}
	if staged != "" {
		// The registration is gone; a staging file that cannot be deleted is
		// harmless and stays out of the library.
		_ = os.Remove(staged)
	}
	return nil
}

// stageMediaRemoval renames the library copy to a neighbouring temporary name
// under the media root. The caller restores it if the transaction fails and
// removes it after the commit, so neither a failed commit nor a failed delete
// can leave the database referencing a file that is already gone.
func (s *Service) stageMediaRemoval(relative string) (string, string, error) {
	root, e := os.OpenRoot(s.cfg.MediaDir)
	if e != nil {
		return "", "", problem("file_removal", "The media folder is not available right now.")
	}
	defer root.Close()
	rel := path.Clean(strings.ReplaceAll(relative, "\\", "/"))
	if rel == "." || rel == "" || strings.HasPrefix(rel, "../") || path.IsAbs(rel) || !fs.ValidPath(rel) {
		return "", "", problem("invalid_path", "This registered path is not safe to delete.")
	}
	if s.cfg.DownloadsDir != "" {
		if download, e := os.OpenRoot(s.cfg.DownloadsDir); e == nil {
			if _, err := download.Stat(rel); err == nil {
				download.Close()
				return "", "", problem("seeding_source", "This file lives in the download folder and is kept for seeding.")
			}
			download.Close()
		}
	}
	stagedRel := rel + ".reelscarey-removing-" + fmt.Sprint(time.Now().UnixNano())
	if e = root.Rename(filepath.FromSlash(rel), filepath.FromSlash(stagedRel)); e != nil {
		if errors.Is(e, os.ErrNotExist) {
			return "", "", problem("file_missing", "The registered file is already missing from disk. Remove only the library entry instead.")
		}
		return "", "", problem("file_removal", "The file could not be moved aside for deletion: "+e.Error())
	}
	return filepath.Join(s.cfg.MediaDir, filepath.FromSlash(stagedRel)), stagedRel, nil
}
