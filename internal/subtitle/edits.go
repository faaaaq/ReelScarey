package subtitle

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type SubtitleEdit struct {
	ID     string `json:"id"`
	FileID int64  `json:"media_file_id"`
	Track
	Action                 string `json:"action"`
	OldSHA                 string `json:"old_sha256"`
	NewSHA                 string `json:"new_sha256"`
	Size                   int    `json:"size_bytes"`
	Cues                   int    `json:"cues"`
	Offset                 int    `json:"offset_ms"`
	Destination            string `json:"destination"`
	Previous               string `json:"previous_path"`
	State                  string `json:"state"`
	Phase                  string `json:"phase"`
	Message                string `json:"message"`
	Expires                int64  `json:"expires_at"`
	Created                int64  `json:"created_at"`
	stage, snapshot, owner string
	content                []byte
}

const editColumns = `id,media_file_id,language,variant,action,old_sha,new_sha,size_bytes,cues,offset_ms,destination,previous_path,state,phase,message,expires_at,created_at,stage_path,snapshot,lease_owner,content`

func scanEdit(row interface{ Scan(...any) error }) (SubtitleEdit, error) {
	var v SubtitleEdit
	e := row.Scan(&v.ID, &v.FileID, &v.Language, &v.Variant, &v.Action, &v.OldSHA, &v.NewSHA, &v.Size, &v.Cues, &v.Offset, &v.Destination, &v.Previous, &v.State, &v.Phase, &v.Message, &v.Expires, &v.Created, &v.stage, &v.snapshot, &v.owner, &v.content)
	return v, e
}

type EditRequest struct {
	Track
	ReferenceFormat string `json:"reference_format,omitempty"`
	Action          string `json:"action"`
	Offset          int    `json:"offset_ms"`
	Content         string `json:"content"`
	PreviousID      string `json:"previous_edit_id"`
}
type sidecarSnapshot struct {
	MediaPath, MediaSHA, Path, SHA, RegisteredSHA string
	Mtime, Size                                   int64
	Track
	Identity      string
	Root          string
	MediaSize     int64
	MediaMtime    int64
	MediaIdentity string
}

func (s *Service) sidecar(ctx context.Context, fileID int64, t Track) (sidecarSnapshot, []byte, error) {
	var snap sidecarSnapshot
	snap.Track = t
	e := s.db.QueryRowContext(ctx, `SELECT f.relative_path,f.sha256,f.size_bytes,s.relative_path,s.sha256 FROM media_files f JOIN subtitles s ON s.media_file_id=f.id WHERE f.id=? AND s.language=? AND s.variant=? AND s.source<>'embedded' AND s.relative_path<>''`, fileID, t.Language, t.Variant).Scan(&snap.MediaPath, &snap.MediaSHA, &snap.MediaSize, &snap.Path, &snap.RegisteredSHA)
	if e != nil {
		return snap, nil, e
	}
	if !local(snap.Path) || !local(snap.MediaPath) || path.Dir(snap.Path) != path.Dir(snap.MediaPath) {
		return snap, nil, problem("unsafe_path", "Only registered sidecars beside their media file can be edited.")
	}
	root, e := s.openEditRoot()
	if e != nil {
		return snap, nil, e
	}
	defer root.Close()
	snap.Root, _ = filepath.EvalSymlinks(s.cfg.MediaDir)
	if e = segments(root, snap.MediaPath); e != nil {
		return snap, nil, e
	}
	video, e := root.Lstat(filepath.FromSlash(snap.MediaPath))
	if e != nil || !video.Mode().IsRegular() || video.Size() != snap.MediaSize {
		return snap, nil, problem("source_changed", "Registered media changed before the subtitle review.")
	}
	snap.MediaMtime = video.ModTime().UnixNano()
	snap.MediaIdentity, e = sidecarIdentity(root, snap.MediaPath, video)
	if e != nil {
		return snap, nil, e
	}
	body, info, e := readSidecar(ctx, root, snap.Path)
	if e != nil {
		return snap, nil, e
	}
	snap.Size = info.Size()
	snap.Mtime = info.ModTime().UnixNano()
	snap.SHA = contentSHA(body)
	snap.Identity, e = sidecarIdentity(root, snap.Path, info)
	if e != nil {
		return snap, nil, e
	}
	if snap.RegisteredSHA != "" && snap.SHA != snap.RegisteredSHA {
		return snap, nil, problem("revision_conflict", "The registered subtitle changed. Inspect it before editing.")
	}
	return snap, body, nil
}
func contentSHA(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func readSidecar(ctx context.Context, root *os.Root, name string) ([]byte, os.FileInfo, error) {
	if !local(name) {
		return nil, nil, problem("unsafe_path", "Subtitle path is unsafe.")
	}
	if e := segments(root, path.Dir(name)); e != nil {
		return nil, nil, e
	}
	before, e := root.Lstat(filepath.FromSlash(name))
	if e != nil {
		return nil, nil, e
	}
	if !before.Mode().IsRegular() || before.Size() < 1 || before.Size() > MaxSubtitle {
		return nil, nil, problem("invalid_subtitle", "Subtitle must be a regular file of at most 4 MiB.")
	}
	f, e := root.Open(filepath.FromSlash(name))
	if e != nil {
		return nil, nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !os.SameFile(before, info) {
		return nil, nil, problem("source_changed", "Subtitle changed during inspection.")
	}
	b, e := io.ReadAll(io.LimitReader(contextReader{ctx, f}, MaxSubtitle+1))
	after, ae := f.Stat()
	visible, ve := root.Lstat(filepath.FromSlash(name))
	if e != nil || ae != nil || ve != nil || !os.SameFile(info, visible) || after.Size() != info.Size() || after.ModTime() != info.ModTime() || len(b) != int(info.Size()) {
		return nil, nil, problem("source_changed", "Subtitle changed during inspection.")
	}
	return b, info, nil
}

func sidecarIdentity(root *os.Root, name string, expected os.FileInfo) (string, error) {
	f, e := root.Open(filepath.FromSlash(name))
	if e != nil {
		return "", e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !os.SameFile(expected, info) {
		return "", problem("source_changed", "Subtitle identity changed.")
	}
	return organization.FileIdentity(f)
}
func (s *Service) ReviewEdit(ctx context.Context, fileID int64, in EditRequest) (SubtitleEdit, error) {
	defer organization.WriteMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return SubtitleEdit{}, e
	}
	if !validTrack(in.Track) {
		return SubtitleEdit{}, problem("invalid_subtitle", "Choose a registered subtitle track.")
	}
	snap, old, e := s.sidecar(ctx, fileID, in.Track)
	if e != nil {
		return SubtitleEdit{}, e
	}
	content := old
	message := "Review required; the original will be retained."
	switch in.Action {
	case "align":
		if in.Offset != 0 || !supportedSubtitleFormat(in.ReferenceFormat) {
			return SubtitleEdit{}, problem("invalid_subtitle", "Choose a supported reference subtitle and review alignment separately.")
		}
		content, message, e = AlignTimedText(old, subtitleExtension(snap.Path), []byte(in.Content), "."+in.ReferenceFormat)
		if e != nil {
			return SubtitleEdit{}, e
		}
	case "offset":
		if in.Offset == 0 {
			return SubtitleEdit{}, problem("invalid_subtitle", "Choose a non-zero timing adjustment.")
		}
	case "replace":
		if in.Offset != 0 {
			return SubtitleEdit{}, problem("invalid_subtitle", "Review timing and replacement separately.")
		}
		content = []byte(in.Content)
	case "rollback":
		var previous, sha string
		e = s.db.QueryRowContext(ctx, `SELECT previous_path,old_sha FROM subtitle_edits WHERE id=? AND media_file_id=? AND language=? AND variant=? AND phase='committed'`, in.PreviousID, fileID, in.Language, in.Variant).Scan(&previous, &sha)
		if e != nil {
			return SubtitleEdit{}, e
		}
		root, e := s.openEditRoot()
		if e != nil {
			return SubtitleEdit{}, e
		}
		content, _, e = readSidecar(ctx, root, previous)
		root.Close()
		if e != nil {
			return SubtitleEdit{}, e
		}
		if contentSHA(content) != sha {
			return SubtitleEdit{}, problem("source_changed", "Retained subtitle changed; rollback refused.")
		}
		in.Offset = 0
	default:
		return SubtitleEdit{}, problem("invalid_subtitle", "Choose a timing adjustment, reference alignment, replacement or reviewed rollback.")
	}
	body, cues, e := ShiftTimedText(content, subtitleExtension(snap.Path), in.Offset)
	if e != nil {
		return SubtitleEdit{}, e
	}
	if contentSHA(body) == snap.SHA {
		return SubtitleEdit{}, problem("invalid_subtitle", "The reviewed subtitle would be unchanged.")
	}
	id := transfer.ID()
	raw, _ := json.Marshal(snap)
	folder := path.Dir(snap.Path)
	v := SubtitleEdit{ID: id, FileID: fileID, Track: in.Track, Action: in.Action, OldSHA: snap.SHA, NewSHA: contentSHA(body), Size: len(body), Cues: cues, Offset: in.Offset, Destination: snap.Path, Previous: path.Join(".reelscarey-subtitle-history", id+".previous"), State: "review", Phase: "planned", Message: message, Expires: s.now().Unix() + 1800, Created: s.now().Unix(), stage: path.Join(folder, ".reelscarey-subedit-"+id+".part"), snapshot: string(raw), content: body}
	tx, txErr := s.db.BeginTx(ctx, nil)
	if txErr != nil {
		return v, txErr
	}
	defer tx.Rollback()
	if _, txErr = tx.ExecContext(ctx, `DELETE FROM subtitle_edits WHERE state='review' AND (expires_at<? OR (media_file_id=? AND language=? AND variant=?))`, s.now().Unix(), fileID, in.Language, in.Variant); txErr != nil {
		return v, txErr
	}
	var reviews int
	if txErr = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_edits WHERE state='review'`).Scan(&reviews); txErr != nil {
		return v, txErr
	}
	if reviews >= 20 {
		return v, problem("subtitle_busy", "Finish or let existing subtitle reviews expire before creating more.")
	}
	if _, txErr = tx.ExecContext(ctx, `INSERT INTO subtitle_edits(id,media_file_id,language,variant,action,snapshot,content,old_sha,new_sha,size_bytes,cues,offset_ms,destination,stage_path,previous_path,expires_at,created_at,message) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, v.ID, v.FileID, v.Language, v.Variant, v.Action, v.snapshot, body, v.OldSHA, v.NewSHA, v.Size, v.Cues, v.Offset, v.Destination, v.stage, v.Previous, v.Expires, v.Created, v.Message); txErr != nil {
		return v, txErr
	}
	return v, tx.Commit()
}
func (s *Service) Edits(ctx context.Context, fileID int64) ([]SubtitleEdit, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+editColumns+` FROM subtitle_edits WHERE media_file_id=? AND (state<>'review' OR expires_at>=?) ORDER BY created_at DESC,id DESC LIMIT 30`, fileID, s.now().Unix())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []SubtitleEdit{}
	for rows.Next() {
		v, e := scanEdit(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Service) QueueEdit(ctx context.Context, id string, reviewed bool) error {
	if !reviewed {
		return problem("review_required", "Review the replacement or timing change and retained original before applying.")
	}
	defer organization.WriteMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return e
	}
	v, e := scanEdit(s.db.QueryRowContext(ctx, `SELECT `+editColumns+` FROM subtitle_edits WHERE id=?`, id))
	if e != nil {
		return e
	}
	if v.State != "review" || v.Expires < s.now().Unix() {
		return problem("revision_conflict", "Subtitle edit review expired or was already applied.")
	}
	snap, _, e := s.sidecar(ctx, v.FileID, v.Track)
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(snap)
	if string(raw) != v.snapshot {
		return problem("revision_conflict", "Subtitle or media registration changed. Create another review.")
	}
	var busy int
	if e = s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM subtitle_jobs WHERE media_file_id=? AND (lease_until>=? OR requested=1))+(SELECT COUNT(*) FROM subtitle_imports WHERE media_file_id=? AND phase<>'committed')`, v.FileID, s.now().Unix(), v.FileID).Scan(&busy); e != nil {
		return e
	}
	if busy > 0 {
		return problem("subtitle_busy", "Finish or cancel the pending subtitle job before editing this file.")
	}
	_, e = s.db.ExecContext(ctx, `UPDATE subtitle_edits SET state='queued',message='Reviewed subtitle edit queued.' WHERE id=? AND state='review'`, id)
	return e
}
func (s *Service) RetryEdit(ctx context.Context, id string) error {
	r, e := s.db.ExecContext(ctx, `UPDATE subtitle_edits SET state='queued',lease_owner='',lease_until=0,message='Reviewed subtitle recovery queued.' WHERE id=? AND state='blocked'`, id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return problem("revision_conflict", "Choose a blocked reviewed subtitle edit.")
	}
	return nil
}
func (s *Service) ProcessEdit(ctx context.Context) (bool, error) {
	var n int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_edits WHERE state IN ('queued','running') AND lease_until<?`, s.now().Unix()).Scan(&n); e != nil || n == 0 {
		return false, e
	}
	defer organization.WriteMedia()()
	if e := organization.EnsureOrganizationAvailable(ctx, s.db); e != nil {
		return false, e
	}
	owner := transfer.ID()
	v, e := scanEdit(s.db.QueryRowContext(ctx, `UPDATE subtitle_edits SET state='running',lease_owner=?,lease_until=? WHERE id=(SELECT id FROM subtitle_edits WHERE state IN ('queued','running') AND lease_until<? ORDER BY created_at,id LIMIT 1) RETURNING `+editColumns, owner, s.now().Unix()+60, s.now().Unix()))
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	e = s.executeEdit(ctx, &v)
	finish, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e != nil {
		state := "blocked"
		if ctx.Err() != nil {
			state = "queued"
		}
		_, _ = s.db.ExecContext(finish, `UPDATE subtitle_edits SET state=?,lease_owner='',lease_until=0,message='Subtitle recovery needs attention; originals and owned staging files were retained.' WHERE id=? AND lease_owner=?`, state, v.ID, v.owner)
	}
	return true, e
}
func (s *Service) executeEdit(ctx context.Context, v *SubtitleEdit) error {
	var snap sidecarSnapshot
	if e := json.Unmarshal([]byte(v.snapshot), &snap); e != nil {
		return e
	}
	root, e := s.openEditRoot()
	if e != nil {
		return e
	}
	defer root.Close()
	resolved, e := filepath.EvalSymlinks(s.cfg.MediaDir)
	if e != nil || resolved != snap.Root {
		return problem("source_changed", "Media root changed; subtitle recovery was held.")
	}
	for _, name := range []string{v.Destination, v.stage} {
		if !local(name) || path.Dir(name) != path.Dir(snap.MediaPath) {
			return problem("unsafe_path", "Subtitle journal paths are invalid.")
		}
	}
	if v.Previous != path.Join(".reelscarey-subtitle-history", v.ID+".previous") {
		return problem("unsafe_path", "Retained subtitle journal path is invalid.")
	}
	if e = segments(root, snap.MediaPath); e != nil {
		return e
	}
	video, e := root.Lstat(filepath.FromSlash(snap.MediaPath))
	if e != nil || !video.Mode().IsRegular() || video.Size() != snap.MediaSize || video.ModTime().UnixNano() != snap.MediaMtime {
		return problem("source_changed", "Registered media changed; subtitle editing was held.")
	}
	videoIdentity, e := sidecarIdentity(root, snap.MediaPath, video)
	if e != nil || videoIdentity != snap.MediaIdentity {
		return problem("source_changed", "Registered media identity changed; subtitle editing was held.")
	}
	var mediaPath, mediaSHA string
	if e = s.db.QueryRowContext(ctx, `SELECT relative_path,sha256 FROM media_files WHERE id=?`, v.FileID).Scan(&mediaPath, &mediaSHA); e != nil {
		return e
	}
	if mediaPath != snap.MediaPath || mediaSHA != snap.MediaSHA {
		return problem("source_changed", "Media registration changed; subtitle edit held.")
	}
	phase := func(value string) error {
		r, e := s.db.ExecContext(ctx, `UPDATE subtitle_edits SET phase=?,lease_until=? WHERE id=? AND lease_owner=?`, value, s.now().Unix()+60, v.ID, v.owner)
		if e != nil {
			return e
		}
		n, _ := r.RowsAffected()
		if n != 1 {
			return ErrLease
		}
		v.Phase = value
		return nil
	}
	if v.Phase == "planned" {
		current, _, e := readSidecar(ctx, root, v.Destination)
		if e != nil {
			return e
		}
		if contentSHA(current) != v.OldSHA {
			return problem("source_changed", "Original subtitle changed before editing.")
		}
		if len(v.content) != v.Size || contentSHA(v.content) != v.NewSHA {
			return problem("invalid_subtitle", "Reviewed subtitle content is incomplete.")
		}
		if _, e = root.Lstat(v.stage); errors.Is(e, os.ErrNotExist) {
			out, e := root.OpenFile(v.stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return e
			}
			_, e = out.Write(v.content)
			e = errors.Join(e, out.Sync(), out.Close())
			if e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		b, _, e := readSidecar(ctx, root, v.stage)
		if e != nil || contentSHA(b) != v.NewSHA {
			return problem("source_changed", "Subtitle staging changed; it was preserved.")
		}
		if info, e := root.Lstat(".reelscarey-subtitle-history"); errors.Is(e, os.ErrNotExist) {
			if e = root.Mkdir(".reelscarey-subtitle-history", 0700); e != nil {
				return e
			}
		} else if e != nil {
			return e
		} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return problem("unsafe_path", "Subtitle history must be a regular private directory.")
		}
		if _, e = root.Lstat(v.Previous); errors.Is(e, os.ErrNotExist) {
			out, e := root.OpenFile(v.Previous, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return e
			}
			_, e = out.Write(current)
			e = errors.Join(e, out.Sync(), out.Close())
			if e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		previous, _, e := readSidecar(ctx, root, v.Previous)
		if e != nil || contentSHA(previous) != v.OldSHA {
			return problem("source_changed", "Retained subtitle failed verification.")
		}
		if e = syncSubtitleDir(root, path.Dir(v.Destination)); e != nil {
			return e
		}
		if e = syncSubtitleDir(root, ".reelscarey-subtitle-history"); e != nil {
			return e
		}
		if e = syncSubtitleDir(root, "."); e != nil {
			return e
		}
		if e = phase("staged"); e != nil {
			return e
		}
	}
	if v.Phase == "staged" {
		previous, _, e := readSidecar(ctx, root, v.Previous)
		if e != nil || contentSHA(previous) != v.OldSHA {
			return problem("source_changed", "Retained subtitle changed; publication held.")
		}
		stage, staged, e := readSidecar(ctx, root, v.stage)
		if e != nil || contentSHA(stage) != v.NewSHA {
			return problem("source_changed", "Subtitle staging changed; publication held.")
		}
		current, info, e := readSidecar(ctx, root, v.Destination)
		if e == nil && contentSHA(current) == v.OldSHA {
			actual, ie := sidecarIdentity(root, v.Destination, info)
			if ie != nil || actual != snap.Identity {
				return problem("source_changed", "Original subtitle identity changed; it was preserved.")
			}
			if e = root.Remove(v.Destination); e != nil {
				return e
			}
			e = os.ErrNotExist
		} else if e == nil && (contentSHA(current) != v.NewSHA || !os.SameFile(info, staged)) {
			return problem("destination_changed", "Subtitle destination is occupied by a changed or foreign file; it was preserved.")
		}
		if errors.Is(e, os.ErrNotExist) {
			if e = root.Link(v.stage, v.Destination); e != nil {
				return e
			}
		} else if e != nil {
			return e
		}
		if e = syncSubtitleDir(root, path.Dir(v.Destination)); e != nil {
			return e
		}
		if e = phase("published"); e != nil {
			return e
		}
	}
	if v.Phase == "published" {
		b, current, e := readSidecar(ctx, root, v.Destination)
		if e != nil || contentSHA(b) != v.NewSHA {
			return problem("destination_changed", "Published subtitle changed; registration held.")
		}
		_, staged, e := readSidecar(ctx, root, v.stage)
		if e != nil || !os.SameFile(current, staged) {
			return problem("destination_changed", "Published subtitle ownership could not be verified.")
		}
		tx, e := s.db.BeginTx(ctx, nil)
		if e != nil {
			return e
		}
		defer tx.Rollback()
		var valid int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_edits ed JOIN media_files f ON f.id=ed.media_file_id JOIN subtitles st ON st.media_file_id=f.id AND st.language=ed.language AND st.variant=ed.variant WHERE ed.id=? AND ed.lease_owner=? AND f.relative_path=? AND f.sha256=? AND st.relative_path=? AND st.sha256=?`, v.ID, v.owner, snap.MediaPath, snap.MediaSHA, snap.Path, snap.RegisteredSHA).Scan(&valid); e != nil {
			return e
		}
		if valid != 1 {
			return ErrLease
		}
		if _, e = tx.ExecContext(ctx, `UPDATE subtitles SET sha256=?,message='Reviewed subtitle edit; previous version retained.',updated_at=? WHERE media_file_id=? AND language=? AND variant=?`, v.NewSHA, s.now().Unix(), v.FileID, v.Language, v.Variant); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE subtitle_imports SET sha256=?,size_bytes=? WHERE media_file_id=? AND language=? AND variant=? AND destination=? AND phase='committed'`, v.NewSHA, v.Size, v.FileID, v.Language, v.Variant, v.Destination); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE youtube_subtitle_files SET sha256=?,size_bytes=? WHERE relative_path=?`, v.NewSHA, v.Size, v.Destination); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE youtube_subtitle_imports SET sha256=?,size_bytes=? WHERE destination_path=? AND phase='committed'`, v.NewSHA, v.Size, v.Destination); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM subtitle_candidates WHERE media_file_id=?`, v.FileID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE subtitle_edits SET phase='committed',state='completed',content=NULL,message='Subtitle edit verified; previous version retained.',lease_owner='',lease_until=0 WHERE id=? AND lease_owner=?`, v.ID, v.owner); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		v.Phase = "committed"
	}
	if v.Phase == "committed" {
		_, a, e := readSidecar(ctx, root, v.Destination)
		if e != nil {
			return e
		}
		_, b, e := readSidecar(ctx, root, v.stage)
		if e == nil && os.SameFile(a, b) {
			_ = root.Remove(v.stage)
		}
		return syncSubtitleDir(root, path.Dir(v.Destination))
	}
	return nil
}
func syncSubtitleDir(root *os.Root, dir string) error {
	f, e := root.Open(filepath.FromSlash(dir))
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}

func (s *Service) openEditRoot() (*os.Root, error) {
	media, e := filepath.EvalSymlinks(s.cfg.MediaDir)
	if e != nil {
		return nil, e
	}
	media, e = filepath.Abs(media)
	if e != nil {
		return nil, e
	}
	contains := func(a, b string) bool {
		r, e := filepath.Rel(a, b)
		return e == nil && (r == "." || filepath.IsLocal(r))
	}
	for _, p := range []string{s.cfg.ConfigDir, s.cfg.DownloadsDir} {
		if p == "" {
			continue
		}
		if resolved, e := filepath.EvalSymlinks(p); e == nil {
			p = resolved
		}
		p, e = filepath.Abs(p)
		if e != nil {
			return nil, e
		}
		if contains(media, p) || contains(p, media) {
			return nil, problem("unsafe_path", "Media, config and downloads must be separate before editing subtitles.")
		}
	}
	return os.OpenRoot(s.cfg.MediaDir)
}
