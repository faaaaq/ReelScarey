package organization

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"
)

// EnsureMediaAvailable keeps normal workers out until interrupted organization
// has reconciled its durable journal. Failed plans leave a valid canonical copy.
func EnsureMediaAvailable(ctx context.Context, db *sql.DB) error {
	return ensureMediaAvailable(ctx, db)
}

// EnsureMediaAvailableTx fences a media reservation in the caller's write transaction.
func EnsureMediaAvailableTx(ctx context.Context, tx *sql.Tx) error {
	return ensureMediaAvailable(ctx, tx)
}

func ensureMediaAvailable(ctx context.Context, db query) error {
	var n int
	if e := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM organization_plans WHERE state IN ('queued','running') OR (state='failed' AND (json_extract(snapshot,'$.folder_move') IS NOT NULL OR json_extract(snapshot,'$.replacement') IS NOT NULL) AND json_extract(snapshot,'$.phase') NOT IN ('committed','failed_safe'))`).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return problem("organization_busy", "A reviewed organization batch is using the library. Media work will resume after it finishes.")
	}
	return nil
}
func allFiles(p Plan) []Companion {
	out := []Companion{}
	for _, v := range p.Files {
		if !v.Unchanged {
			out = append(out, Companion{Source: v.Source, Destination: v.Destination, Size: v.Size, Mtime: v.Mtime, SHA: v.SHA, Identity: v.Identity})
		}
		for _, c := range v.Companions {
			if c.Source != c.Destination {
				out = append(out, c)
			}
		}
	}
	return out
}
func (s *Service) execute(ctx context.Context, p *Plan) error {
	if p.Replacement != nil {
		return s.executeReplacement(ctx, p)
	}
	if p.FolderMove != nil {
		return s.executeFolder(ctx, p)
	}
	root, _, e := s.openRoot(p.Root)
	if e != nil {
		return e
	}
	defer root.Close()
	if p.Phase != "committed" {
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		if e = s.validate(ctx, root, tx, *p, true); e != nil {
			tx.Rollback()
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		for n, v := range allFiles(*p) {
			if e = checkSource(ctx, root, v, true); e != nil {
				return e
			}
			if e = s.publish(ctx, root, *p, v, n); e != nil {
				return e
			}
		}
		if e = s.savePhase(ctx, p, "published"); e != nil {
			return e
		}
		// Recheck all content and metadata after publication, before references move.
		tx, e = s.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		if e = s.validate(ctx, root, tx, *p, true); e != nil {
			return e
		}
		for _, v := range allFiles(*p) {
			if e = checkSource(ctx, root, v, true); e != nil {
				return e
			}
			if e = s.checkOutput(ctx, root, tx, p.ID, v); e != nil {
				return e
			}
		}
		for _, v := range p.Files {
			if !v.Unchanged {
				if _, e = tx.ExecContext(ctx, `UPDATE media_files SET relative_path=? WHERE id=? AND relative_path=? AND sha256=?`, v.Destination, v.FileID, v.Source, v.SHA); e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, `UPDATE youtube_imports SET destination_path=? WHERE phase='committed' AND destination_path=? AND job_id IN (SELECT job_id FROM youtube_files WHERE media_file_id=?)`, v.Destination, v.Source, v.FileID); e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, `UPDATE youtube_files SET relative_path=? WHERE media_file_id=? AND relative_path=? AND sha256=?`, v.Destination, v.FileID, v.Source, v.SHA); e != nil {
					return e
				}
			}
			for _, c := range v.Companions {
				if _, e = tx.ExecContext(ctx, `UPDATE subtitles SET relative_path=? WHERE media_file_id=? AND relative_path=?`, c.Destination, v.FileID, c.Source); e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, `UPDATE subtitle_imports SET destination=? WHERE media_file_id=? AND destination=? AND phase='committed'`, c.Destination, v.FileID, c.Source); e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, `UPDATE youtube_subtitle_imports SET destination_path=? WHERE phase='committed' AND destination_path=? AND job_id IN (SELECT job_id FROM youtube_files WHERE media_file_id=?)`, c.Destination, c.Source, v.FileID); e != nil {
					return e
				}
				if _, e = tx.ExecContext(ctx, `UPDATE youtube_subtitle_files SET relative_path=? WHERE relative_path=? AND job_id IN (SELECT job_id FROM youtube_files WHERE media_file_id=?)`, c.Destination, c.Source, v.FileID); e != nil {
					return e
				}
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM subtitle_inspections WHERE media_file_id=?`, v.FileID); e != nil {
				return e
			}
		}
		// A target collection affects future imports only when all selected files
		// have reached the new layout. Metadata/history/episode IDs remain intact.
		if p.TargetLibrary != "" {
			seen := map[int64]bool{}
			for _, v := range p.Files {
				if seen[v.MediaID] {
					continue
				}
				seen[v.MediaID] = true
				if _, e = tx.ExecContext(ctx, `INSERT INTO media_organization(media_id,library_key,revision) VALUES(?,?,1) ON CONFLICT(media_id) DO UPDATE SET library_key=excluded.library_key,revision=media_organization.revision+1`, v.MediaID, p.TargetLibrary); e != nil {
					return e
				}
			}
		}
		p.Phase = "committed"
		raw, e := json.Marshal(p)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE organization_plans SET snapshot=?,updated_at=unixepoch() WHERE id=?`, string(raw), p.ID); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
	}
	// References already point to verified outputs. Cleanup never touches a
	// download-client source, unknown file, or file replaced after publication.
	for n, v := range allFiles(*p) {
		if e = s.checkOutput(ctx, root, s.db, p.ID, v); e != nil {
			return e
		}
		if e = checkSource(ctx, root, v, true); e == nil {
			if e = root.Remove(filepath.FromSlash(v.Source)); e != nil {
				return problem("cleanup_pending", "The new file is registered, but its old library link could not be removed. Retry cleanup.")
			}
		} else if !errors.Is(e, os.ErrNotExist) {
			return problem("cleanup_pending", "The new file is registered. An old library path changed, so it was retained for review.")
		}
		stage := path.Join(path.Dir(v.Destination), fmt.Sprintf(".reelscarey-organize-%s-%d.part", p.ID, n))
		var saved string
		if e = s.db.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, p.ID, v.Destination).Scan(&saved); e != nil {
			return e
		}
		if f, e := root.Open(filepath.FromSlash(stage)); e == nil {
			id, _ := identity(f)
			f.Close()
			if id == saved {
				_ = root.Remove(filepath.FromSlash(stage))
			}
		}
	}
	_, e = s.db.ExecContext(ctx, `UPDATE organization_plans SET state='completed',message='',updated_at=unixepoch() WHERE id=?`, p.ID)
	return e
}
func (s *Service) checkOutput(ctx context.Context, root *os.Root, q query, planID string, v Companion) error {
	var saved string
	if e := q.QueryRowContext(ctx, `SELECT identity FROM organization_outputs WHERE plan_id=? AND destination=?`, planID, v.Destination).Scan(&saved); e != nil {
		return e
	}
	if e := segments(root, v.Destination, false); e != nil {
		return e
	}
	f, e := root.Open(filepath.FromSlash(v.Destination))
	if e != nil {
		return e
	}
	defer f.Close()
	actual, e := identity(f)
	if e != nil {
		return e
	}
	if saved == "" || actual != saved {
		return problem("destination_changed", "A published destination was replaced and was retained for review.")
	}
	info, e := f.Stat()
	if e != nil {
		return e
	}
	h, e := hash(ctx, f)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() || info.Size() != v.Size || h != v.SHA {
		return problem("destination_changed", "A published destination changed and was retained for review.")
	}
	return nil
}
func (s *Service) publish(ctx context.Context, root *os.Root, p Plan, v Companion, n int) error {
	dir := path.Dir(v.Destination)
	if e := segments(root, dir, true); e != nil {
		return e
	}
	if e := root.MkdirAll(filepath.FromSlash(dir), 0755); e != nil {
		return e
	}
	if e := segments(root, dir, false); e != nil {
		return e
	}
	stage := path.Join(dir, fmt.Sprintf(".reelscarey-organize-%s-%d.part", p.ID, n))
	if _, e := s.db.ExecContext(ctx, `INSERT INTO organization_outputs(plan_id,destination,stage) VALUES(?,?,?) ON CONFLICT(plan_id,destination) DO NOTHING`, p.ID, v.Destination, stage); e != nil {
		return e
	}
	var saved, phase string
	if e := s.db.QueryRowContext(ctx, `SELECT identity,phase FROM organization_outputs WHERE plan_id=? AND destination=? AND stage=?`, p.ID, v.Destination, stage).Scan(&saved, &phase); e != nil {
		return e
	}
	stageInfo, stageErr := root.Lstat(filepath.FromSlash(stage))
	if errors.Is(stageErr, os.ErrNotExist) {
		if phase == "published" {
			return s.checkOutput(ctx, root, s.db, p.ID, v)
		}
		if exists, e := existsFolded(root, v.Destination); e != nil {
			return e
		} else if exists {
			return problem("destination_exists", "A destination already exists and was not overwritten.")
		}
		// A hardlink preserves seeding and timestamps. Copy fallback is staged and
		// journaled before bytes are written, then published with an exclusive link.
		link := s.link
		if link == nil {
			link = func(root *os.Root, old, new string) error { return root.Link(old, new) }
		}
		e := link(root, filepath.FromSlash(v.Source), filepath.FromSlash(stage))
		copyMode := false
		if e != nil {
			if errors.Is(e, os.ErrExist) {
				return problem("staging_conflict", "An unexpected organization staging file appeared.")
			}
			f, e := root.OpenFile(filepath.FromSlash(stage), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if e != nil {
				return e
			}
			saved, e = identity(f)
			f.Close()
			if e != nil {
				return e
			}
			copyMode = true
		} else {
			f, e := root.Open(filepath.FromSlash(stage))
			if e != nil {
				return e
			}
			saved, e = identity(f)
			f.Close()
			if e != nil {
				return e
			}
		}
		if _, e = s.db.ExecContext(ctx, `UPDATE organization_outputs SET identity=?,phase=? WHERE plan_id=? AND destination=?`, saved, map[bool]string{true: "copying", false: "staged"}[copyMode], p.ID, v.Destination); e != nil {
			return e
		}
		phase = map[bool]string{true: "copying", false: "staged"}[copyMode]
	} else if stageErr != nil {
		return stageErr
	} else if !stageInfo.Mode().IsRegular() {
		return problem("staging_conflict", "An organization staging path is not a regular file.")
	}
	if e := segments(root, stage, false); e != nil {
		return e
	}
	flags := os.O_RDONLY
	if phase == "copying" {
		flags = os.O_RDWR
	}
	stageFile, e := root.OpenFile(filepath.FromSlash(stage), flags, 0)
	if e != nil {
		return e
	}
	defer stageFile.Close()
	actual, e := identity(stageFile)
	if e != nil {
		return e
	}
	if saved == "" { // Interrupted hardlink creation can be proven by source identity.
		if actual != v.Identity {
			return problem("staging_conflict", "An unverified staging file was retained for review.")
		}
		saved = actual
		if _, e = s.db.ExecContext(ctx, `UPDATE organization_outputs SET identity=?,phase='staged' WHERE plan_id=? AND destination=?`, saved, p.ID, v.Destination); e != nil {
			return e
		}
	}
	if actual != saved {
		return problem("staging_conflict", "An organization staging file was replaced.")
	}
	if phase == "copying" {
		src, e := root.Open(filepath.FromSlash(v.Source))
		if e != nil {
			return e
		}
		defer src.Close()
		id, e := identity(src)
		if e != nil || id != v.Identity {
			return problem("source_changed", "The source changed during staged copying.")
		}
		if e = stageFile.Truncate(0); e != nil {
			return e
		}
		if _, e = stageFile.Seek(0, 0); e != nil {
			return e
		}
		if _, e = io.Copy(stageFile, contextReader{ctx, src}); e != nil {
			return e
		}
		if e = stageFile.Chmod(0644); e != nil {
			return e
		}
		if e = stageFile.Sync(); e != nil {
			return e
		}
	}
	h, e := hash(ctx, stageFile)
	if e != nil {
		return e
	}
	info, e := stageFile.Stat()
	if e != nil {
		return e
	}
	if info.Size() != v.Size || h != v.SHA {
		return problem("staging_conflict", "The staged file does not match the reviewed content.")
	}
	if e = stageFile.Close(); e != nil {
		return e
	}
	if e = root.Chtimes(filepath.FromSlash(stage), time.Unix(0, v.Mtime), time.Unix(0, v.Mtime)); e != nil {
		return e
	}
	// Open the same directory handle for no-replace publication. Link cannot
	// overwrite an existing file, including on POSIX where Rename would.
	folder, e := root.OpenRoot(filepath.FromSlash(dir))
	if e != nil {
		return e
	}
	defer folder.Close()
	if exists, e := existsFolded(root, v.Destination); e != nil {
		return e
	} else if exists {
		if _, e = folder.Lstat(path.Base(v.Destination)); e != nil {
			return problem("destination_exists", "A differently cased destination appeared and was left unchanged.")
		}
	}
	if e = folder.Link(path.Base(stage), path.Base(v.Destination)); e != nil {
		if !errors.Is(e, os.ErrExist) {
			return problem("publish_failed", "This filesystem cannot publish without overwriting. The verified stage remains for retry.")
		}
		a, e := folder.Lstat(path.Base(stage))
		if e != nil {
			return e
		}
		b, e := folder.Lstat(path.Base(v.Destination))
		if e != nil || !b.Mode().IsRegular() || !os.SameFile(a, b) {
			return problem("destination_exists", "Another destination appeared and was left unchanged.")
		}
	}
	if runtime.GOOS != "windows" {
		parent, e := folder.Open(".")
		if e != nil {
			return e
		}
		e = parent.Sync()
		parent.Close()
		if e != nil {
			return e
		}
	}
	_, e = s.db.ExecContext(ctx, `UPDATE organization_outputs SET phase='published' WHERE plan_id=? AND destination=?`, p.ID, v.Destination)
	return e
}
