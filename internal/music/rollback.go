package music

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/filecheck"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

func musicDigest(ctx context.Context, root *os.Root, name string) (string, int64, error) {
	f, err := filecheck.Open(root, name)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	before, err := filecheck.Capture(ctx, f)
	if err != nil {
		return "", 0, err
	}
	sha, err := filecheck.Digest(ctx, f)
	if err != nil {
		return "", 0, err
	}
	after, err := filecheck.Capture(ctx, f)
	if err != nil || after != before {
		return "", 0, filecheck.ErrChanged
	}
	return sha, before.Size, nil
}

func expectMusicDigest(ctx context.Context, root *os.Root, name, expected string) error {
	sha, _, err := musicDigest(ctx, root, name)
	if err != nil {
		return err
	}
	if sha != expected {
		return filecheck.ErrChanged
	}
	return nil
}

func sameMusicFile(root *os.Root, a, b string) bool {
	x, e := root.Lstat(a)
	y, f := root.Lstat(b)
	return e == nil && f == nil && x.Mode().IsRegular() && y.Mode().IsRegular() && os.SameFile(x, y)
}

// copyMusicStage writes a new, exclusive sibling. Never truncate an existing
// path, including an interrupted stage. The caller verifies the digest.
func copyMusicStage(ctx context.Context, root *os.Root, source, stage string) error {
	in, err := filecheck.Open(root, source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := root.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, musicReader{ctx, in})
	return errors.Join(err, out.Sync(), out.Close())
}

type musicReader struct {
	ctx context.Context
	r   io.Reader
}

func (r musicReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

type musicRollback struct {
	id, fileID                               int64
	source, dest, backup, tags, originalRoot string
	originalSHA, editedSHA                   string
	phase                                    int
}

func rollbackConflict() error {
	return &Error{Code: "music_rollback_changed", Message: "A file or its catalog entry changed since the edit. No unverified file will be removed. Check the original, edited and retained paths before retrying."}
}

func (s *Service) RollbackEdits(ctx context.Context, jobID int64, reviewed bool) (EditJob, error) {
	if !reviewed {
		return EditJob{}, ErrInvalidInput
	}
	defer organization.ReadMedia()()
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	job, err := s.EditJob(ctx, jobID)
	if err != nil || job.State == "rolled_back" {
		return job, err
	}
	if job.State != "completed" && job.State != "failed" {
		return EditJob{}, &Error{Code: "music_rollback_state", Message: "Only a completed or failed edit can be rolled back."}
	}
	var interrupted bool
	if err := s.store.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM music_edit_files
		WHERE job_id=? AND original_sha<>'' AND state IN ('pending','failed'))`, jobID).Scan(&interrupted); err != nil {
		return EditJob{}, err
	}
	if interrupted {
		return EditJob{}, rollbackConflict()
	}
	rows, err := s.store.db.QueryContext(ctx, `SELECT id,music_file_id,source_path,destination_path,backup_path,previous_tag_json,original_root,original_sha,edited_sha,rollback_phase
		FROM music_edit_files WHERE job_id=? AND state='completed' ORDER BY id DESC`, jobID)
	if err != nil {
		return EditJob{}, err
	}
	var items []musicRollback
	for rows.Next() {
		var it musicRollback
		if err := rows.Scan(&it.id, &it.fileID, &it.source, &it.dest, &it.backup, &it.tags, &it.originalRoot, &it.originalSHA, &it.editedSHA, &it.phase); err != nil {
			rows.Close()
			return EditJob{}, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return EditJob{}, err
	}
	root, err := os.OpenRoot(s.mediaDir)
	if err != nil {
		return EditJob{}, err
	}
	defer root.Close()
	for i := range items {
		it := &items[i]
		if len(it.originalSHA) != 64 || len(it.editedSHA) != 64 {
			return EditJob{}, &Error{Code: "music_rollback_unverified", Message: "This older edit has no recorded file identities. Restore its retained files manually after checking their contents."}
		}
		if filepath.IsAbs(it.backup) {
			rel, err := filepath.Rel(s.mediaDir, it.backup)
			if err != nil || !filepath.IsLocal(rel) {
				return EditJob{}, rollbackConflict()
			}
			it.backup = filepath.ToSlash(rel)
		}
		if it.phase == 0 {
			if err := s.checkMusicRollback(ctx, root, *it); err != nil {
				return EditJob{}, err
			}
		}
	}
	for _, it := range items {
		if err := s.rollbackMusicFile(ctx, root, it); err != nil {
			return EditJob{}, err
		}
	}
	if _, err := s.store.db.ExecContext(ctx, `UPDATE music_edit_jobs SET state='rolled_back',error='',updated_at=unixepoch() WHERE id=?`, jobID); err != nil {
		return EditJob{}, err
	}
	return s.EditJob(ctx, jobID)
}

func (s *Service) checkMusicRollback(ctx context.Context, root *os.Root, it musicRollback) error {
	var name, sha string
	if err := s.store.db.QueryRowContext(ctx, `SELECT relative_path,sha256 FROM music_files WHERE id=?`, it.fileID).Scan(&name, &sha); err != nil {
		return err
	}
	if name != it.dest || sha != it.editedSHA || expectMusicDigest(ctx, root, it.dest, it.editedSHA) != nil {
		return rollbackConflict()
	}
	if it.backup != "" && expectMusicDigest(ctx, root, it.backup, it.originalSHA) != nil {
		return rollbackConflict()
	}
	if it.source != it.dest {
		if _, err := root.Lstat(it.source); !errors.Is(err, os.ErrNotExist) {
			return rollbackConflict()
		}
	}
	return nil
}

func (s *Service) rollbackMusicFile(ctx context.Context, root *os.Root, it musicRollback) error {
	prefix := path.Join(path.Dir(it.source), fmt.Sprintf(".reelscarey-rollback-%d", it.id))
	stage, hold := prefix+"-original", prefix+"-edited"
	if it.phase == 0 {
		// The journal precedes any filesystem changes.
		for _, name := range []string{stage, hold} {
			if _, err := root.Lstat(name); !errors.Is(err, os.ErrNotExist) {
				return rollbackConflict()
			}
		}
		if _, err := s.store.db.ExecContext(ctx, `UPDATE music_edit_files SET rollback_phase=1 WHERE id=?`, it.id); err != nil {
			return err
		}
		it.phase = 1
	}
	if it.phase == 1 {
		donor := it.dest
		if it.backup != "" {
			donor = it.backup
		}
		if err := expectMusicDigest(ctx, root, donor, it.originalSHA); err != nil {
			return rollbackConflict()
		}
		if _, err := root.Lstat(stage); errors.Is(err, os.ErrNotExist) {
			if err := copyMusicStage(ctx, root, donor, stage); err != nil {
				return err
			}
		}
		sha, size, err := musicDigest(ctx, root, stage)
		if err != nil || sha != it.originalSHA {
			return rollbackConflict()
		}
		if !sameMusicFile(root, it.source, stage) {
			if it.source == it.dest {
				// Retain the edited file before releasing the in-place name.
				if _, err := root.Lstat(hold); errors.Is(err, os.ErrNotExist) {
					if err := expectMusicDigest(ctx, root, it.dest, it.editedSHA); err != nil {
						return rollbackConflict()
					}
					if err := root.Link(it.dest, hold); err != nil {
						return err
					}
				}
				if err := expectMusicDigest(ctx, root, hold, it.editedSHA); err != nil {
					return rollbackConflict()
				}
				if sameMusicFile(root, it.dest, hold) {
					if err := root.Remove(it.dest); err != nil {
						return err
					}
				}
			} else if err := expectMusicDigest(ctx, root, it.dest, it.editedSHA); err != nil {
				return rollbackConflict()
			}
			if err := root.Link(stage, it.source); err != nil {
				return rollbackConflict()
			}
		}
		tx, err := s.store.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		result, err := tx.ExecContext(ctx, `UPDATE music_files SET relative_path=?,root=?,sha256=?,size_bytes=?,tag_json=? WHERE id=? AND relative_path=? AND sha256=?`,
			it.source, it.originalRoot, sha, size, it.tags, it.fileID, it.dest, it.editedSHA)
		if err != nil {
			return err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			return rollbackConflict()
		}
		if _, err := tx.ExecContext(ctx, `UPDATE music_edit_files SET rollback_phase=2 WHERE id=?`, it.id); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	if err := expectMusicDigest(ctx, root, it.source, it.originalSHA); err != nil {
		return rollbackConflict()
	}
	cleanup := map[string]string{stage: it.originalSHA, hold: it.editedSHA}
	if it.source != it.dest {
		cleanup[it.dest] = it.editedSHA
	}
	if strings.HasSuffix(it.backup, ".reelscarey-backup") && it.backup == it.source+".reelscarey-backup" {
		cleanup[it.backup] = it.originalSHA
	}
	for name, sha := range cleanup {
		if err := expectMusicDigest(ctx, root, name, sha); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return rollbackConflict()
		}
		if err := root.Remove(name); err != nil {
			return err
		}
	}
	_, err := s.store.db.ExecContext(ctx, `UPDATE music_edit_files SET state='rolled_back',updated_at=unixepoch() WHERE id=?`, it.id)
	return err
}
