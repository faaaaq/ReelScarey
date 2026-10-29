package subtitle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type importPlan struct {
	ID string
	Track
	FileID                  int64
	Destination, Stage, SHA string
	Size                    int64
	Content                 []byte
	Phase                   string
}

func (s *Service) pending(ctx context.Context, id int64, t Track) (*importPlan, error) {
	var p importPlan
	p.Track = t
	e := s.db.QueryRowContext(ctx, `SELECT id,provider_file_id,destination,stage_name,sha256,size_bytes,content,phase FROM subtitle_imports WHERE media_file_id=? AND language=? AND variant=? AND phase<>'committed'`, id, t.Language, t.Variant).Scan(&p.ID, &p.FileID, &p.Destination, &p.Stage, &p.SHA, &p.Size, &p.Content, &p.Phase)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	return &p, nil
}
func destination(m media, t Track, formats ...string) string {
	format := "srt"
	if len(formats) > 0 && formats[0] != "" {
		format = formats[0]
	}
	suffix := "." + t.Language
	if t.Variant != "full" {
		suffix += "." + t.Variant
	}
	return strings.TrimSuffix(m.Path, path.Ext(m.Path)) + suffix + "." + format
}
func (s *Service) plan(ctx context.Context, m media, c Candidate, body []byte) (importPlan, error) {
	format := c.Format
	if format == "" {
		format = "srt"
	}
	if !validTrack(c.Track) || !supportedSubtitleFormat(format) || len(body) == 0 || len(body) > MaxSubtitle {
		return importPlan{}, problem("invalid_subtitle", "The selected subtitle track or content is invalid.")
	}
	if old, e := s.pending(ctx, m.ID, c.Track); e != nil {
		return importPlan{}, e
	} else if old != nil {
		if old.FileID != c.FileID {
			return importPlan{}, problem("pending_import", "A different subtitle for this track is already staged. Retry that import first.")
		}
		return *old, nil
	}
	sum := sha256.Sum256(body)
	id := transfer.ID()
	p := importPlan{ID: id, Track: c.Track, FileID: c.FileID, Destination: destination(m, c.Track, format), Stage: ".reelscarey-sub-" + id + ".part", SHA: hex.EncodeToString(sum[:]), Size: int64(len(body)), Content: body, Phase: "planned"}
	_, e := s.db.ExecContext(ctx, `INSERT INTO subtitle_imports(id,media_file_id,language,variant,provider_file_id,destination,stage_name,sha256,size_bytes,content,phase,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,'planned',?) ON CONFLICT(media_file_id,language,variant) DO UPDATE SET id=excluded.id,provider_file_id=excluded.provider_file_id,destination=excluded.destination,stage_name=excluded.stage_name,sha256=excluded.sha256,size_bytes=excluded.size_bytes,content=excluded.content,phase='planned',updated_at=excluded.updated_at WHERE subtitle_imports.phase='committed'`, id, m.ID, c.Language, c.Variant, c.FileID, p.Destination, p.Stage, p.SHA, p.Size, body, s.now().Unix())
	return p, e
}
func (s *Service) phase(ctx context.Context, j Job, p importPlan, phase string) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.fence(ctx, tx, j, "", false); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_imports SET phase=?,updated_at=? WHERE id=?`, phase, s.now().Unix(), p.ID); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) publish(ctx context.Context, j Job, m media, o *openedMedia, generation string, p importPlan) error {
	format := strings.TrimPrefix(subtitleExtension(p.Destination), ".")
	if !validTrack(p.Track) || !supportedSubtitleFormat(format) || !local(p.Destination) || p.Destination != destination(m, p.Track, format) || path.Base(p.Stage) != p.Stage || p.Stage != ".reelscarey-sub-"+p.ID+".part" || p.Size <= 0 || p.Size > MaxSubtitle || len(p.Content) != int(p.Size) {
		return problem("invalid_subtitle", "The saved subtitle import plan is invalid.")
	}
	sum := sha256.Sum256(p.Content)
	if hex.EncodeToString(sum[:]) != p.SHA {
		return problem("invalid_subtitle", "The saved subtitle content failed verification.")
	}
	if e := o.unchanged(m); e != nil {
		return e
	}
	if e := s.fence(ctx, nil, j, generation, true); e != nil {
		return e
	}
	name := path.Base(p.Destination)
	stageInfo, e := o.folder.Lstat(p.Stage)
	if errors.Is(e, os.ErrNotExist) {
		if p.Phase == "published" {
			return s.commit(ctx, j, m, o, generation, p)
		}
		if _, e = o.folder.Lstat(name); e == nil {
			return problem("destination_exists", "A subtitle already exists at the destination. It was not replaced.")
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		file, createErr := o.folder.OpenFile(p.Stage, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if createErr != nil {
			return problem("storage_unavailable", "A subtitle staging file could not be created. Check media folder permissions.")
		}
		if _, e = file.Write(p.Content); e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		stageInfo, e = o.folder.Lstat(p.Stage)
	}
	if e != nil {
		return e
	}
	if !stageInfo.Mode().IsRegular() {
		return problem("staging_conflict", "The subtitle staging path is not a regular file and was left unchanged.")
	}
	stage, e := o.folder.Open(p.Stage)
	if e != nil {
		return e
	}
	info, e := stage.Stat()
	if e != nil || !os.SameFile(info, stageInfo) {
		stage.Close()
		return problem("staging_conflict", "The subtitle staging file changed during recovery.")
	}
	if info.Size() > MaxSubtitle {
		stage.Close()
		return problem("staging_conflict", "The subtitle staging file exceeds the supported size and was left unchanged.")
	}
	actual, e := digest(ctx, stage)
	stage.Close()
	if e != nil {
		return e
	}
	if info.Size() != p.Size || actual != p.SHA {
		if info.Size() >= 0 && info.Size() < p.Size {
			prefix := sha256.Sum256(p.Content[:info.Size()])
			if actual == hex.EncodeToString(prefix[:]) {
				oldID := p.ID
				p.ID = transfer.ID()
				p.Stage = ".reelscarey-sub-" + p.ID + ".part"
				p.Phase = "planned"
				tx, e := s.db.BeginTx(ctx, nil)
				if e != nil {
					return e
				}
				defer tx.Rollback()
				if e = s.fence(ctx, tx, j, generation, true); e != nil {
					return e
				}
				r, e := tx.ExecContext(ctx, `UPDATE subtitle_imports SET id=?,stage_name=?,phase='planned',updated_at=? WHERE id=?`, p.ID, p.Stage, s.now().Unix(), oldID)
				if e != nil {
					return e
				}
				n, _ := r.RowsAffected()
				if n != 1 {
					return ErrLease
				}
				if e = tx.Commit(); e != nil {
					return e
				}
				return s.publish(ctx, j, m, o, generation, p)
			}
		}
		return problem("staging_conflict", "The subtitle staging file differs from its journal. It was left for review.")
	}
	if e = s.phase(ctx, j, p, "staged"); e != nil {
		return e
	}
	if e = o.unchanged(m); e != nil {
		return e
	}
	// Hold the policy transaction across exclusive publication and registration.
	// Preference/connection changes serialize before or after this filesystem step.
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.fence(ctx, tx, j, generation, true); e != nil {
		return e
	}
	if e = o.folder.Link(p.Stage, name); e != nil {
		if !errors.Is(e, os.ErrExist) {
			return problem("publish_failed", "The media filesystem could not publish the subtitle without replacing a file. The staging file remains for retry.")
		}
		visible, se := o.folder.Lstat(name)
		if se != nil || !visible.Mode().IsRegular() || !os.SameFile(stageInfo, visible) {
			return problem("destination_exists", "A different subtitle already exists. Existing files were preserved.")
		}
	}
	parent, e := o.folder.Open(".")
	if e != nil {
		return e
	}
	syncErr := parent.Sync()
	parent.Close()
	if syncErr != nil && runtime.GOOS != "windows" {
		return syncErr
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_imports SET phase='published',updated_at=? WHERE id=?`, s.now().Unix(), p.ID); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	return s.commit(ctx, j, m, o, generation, p)
}
func (s *Service) commit(ctx context.Context, j Job, m media, o *openedMedia, generation string, p importPlan) error {
	name := path.Base(p.Destination)
	before, e := o.folder.Lstat(name)
	if e != nil || !before.Mode().IsRegular() {
		return problem("destination_changed", "The published subtitle is missing or no longer a regular file.")
	}
	file, e := o.folder.Open(name)
	if e != nil {
		return e
	}
	info, e := file.Stat()
	if e != nil || !os.SameFile(before, info) {
		file.Close()
		return problem("destination_changed", "The published subtitle changed during registration.")
	}
	if info.Size() != p.Size {
		file.Close()
		return problem("destination_changed", "The published subtitle size differs from its journal.")
	}
	hash, e := digest(ctx, file)
	file.Close()
	if e != nil {
		return e
	}
	if info.Size() != p.Size || hash != p.SHA {
		return problem("destination_changed", "The published subtitle differs from the verified import journal.")
	}
	visible, e := o.root.Stat(filepath.FromSlash(p.Destination))
	if e != nil || !os.SameFile(visible, info) {
		return problem("destination_changed", "The subtitle's destination folder moved during registration.")
	}
	if e = o.unchanged(m); e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = s.fence(ctx, tx, j, generation, true); e != nil {
		return e
	}
	var tracks string
	if e = tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT tracks FROM subtitle_overrides WHERE media_id=?),(SELECT tracks FROM subtitle_preferences WHERE id=1))`, m.MediaID).Scan(&tracks); e != nil {
		return e
	}
	var wanted []Track
	if e = json.Unmarshal([]byte(tracks), &wanted); e != nil {
		return e
	}
	if !slices.Contains(wanted, p.Track) {
		return ErrLease
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,provider_file_id,state,message,updated_at) VALUES(?,?,?,'downloaded',?,?,?,'available','Verified subtitle saved.',?) ON CONFLICT(media_file_id,language,variant) DO UPDATE SET source='downloaded',relative_path=excluded.relative_path,sha256=excluded.sha256,provider_file_id=excluded.provider_file_id,state='available',message=excluded.message,updated_at=excluded.updated_at`, m.ID, p.Language, p.Variant, p.Destination, p.SHA, p.FileID, s.now().Unix()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE subtitle_imports SET phase='committed',content=NULL,updated_at=? WHERE id=?`, s.now().Unix(), p.ID); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	if staged, e := o.folder.Lstat(p.Stage); e == nil && staged.Mode().IsRegular() && os.SameFile(staged, info) {
		_ = o.folder.Remove(p.Stage)
	}
	return nil
}

// Only remove a journaled staging link when the committed destination is the
// same regular file and still has its recorded content. Unknown files remain.
func (s *Service) cleanupCommitted(ctx context.Context, m media, o *openedMedia) error {
	rows, e := s.db.QueryContext(ctx, `SELECT id,stage_name,destination,sha256,size_bytes FROM subtitle_imports WHERE media_file_id=? AND phase='committed'`, m.ID)
	if e != nil {
		return e
	}
	var plans []importPlan
	for rows.Next() {
		var p importPlan
		if e = rows.Scan(&p.ID, &p.Stage, &p.Destination, &p.SHA, &p.Size); e != nil {
			rows.Close()
			return e
		}
		plans = append(plans, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, p := range plans {
		if p.Stage != ".reelscarey-sub-"+p.ID+".part" || path.Base(p.Stage) != p.Stage || !local(p.Destination) || path.Dir(p.Destination) != path.Dir(m.Path) {
			continue
		}
		staged, e := o.folder.Lstat(p.Stage)
		if e != nil || !staged.Mode().IsRegular() || staged.Size() != p.Size {
			continue
		}
		visible, e := o.folder.Lstat(path.Base(p.Destination))
		if e != nil || !visible.Mode().IsRegular() || !os.SameFile(staged, visible) {
			continue
		}
		file, e := o.folder.Open(p.Stage)
		if e != nil {
			continue
		}
		info, se := file.Stat()
		hash, he := digest(ctx, file)
		file.Close()
		if se == nil && he == nil && os.SameFile(info, staged) && hash == p.SHA {
			_ = o.folder.Remove(p.Stage)
		}
	}
	return nil
}
