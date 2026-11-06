package youtube

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type ImportReview struct {
	JobID       string           `json:"job_id"`
	Size        int64            `json:"size_bytes"`
	SHA256      string           `json:"sha256"`
	Probe       string           `json:"probe_json"`
	Destination string           `json:"destination"`
	TargetLabel string           `json:"target_label"`
	Subtitles   []SubtitleReview `json:"subtitles"`
}
type SubtitleReview struct {
	Language    string `json:"language"`
	Size        int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
	Destination string `json:"destination"`
}

// ReviewImport derives a destination from the current organization settings and
// stored verified video evidence. It does not create or modify any media file.
func (s *Store) ReviewImport(ctx context.Context, cfg config.Config, jobID string) (ImportReview, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return ImportReview{}, e
	}
	defer tx.Rollback()
	out, e := s.reviewImportTx(ctx, tx, cfg, jobID)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s *Store) reviewImportTx(ctx context.Context, tx *sql.Tx, cfg config.Config, jobID string) (ImportReview, error) {
	var r ImportReview
	var t Target
	var state, source, snapshot, title, videoID string
	e := tx.QueryRowContext(ctx, `SELECT state,target_kind,COALESCE(collection_id,0),COALESCE(media_id,0),COALESCE(episode_id,0),target_snapshot,source_path,source_size,source_sha256,probe_json,video_title,video_id FROM youtube_jobs WHERE id=?`, jobID).Scan(&state, &t.Kind, &t.CollectionID, &t.MediaID, &t.EpisodeID, &snapshot, &source, &r.Size, &r.SHA256, &r.Probe, &title, &videoID)
	if e != nil || state != "awaiting_review" || r.Size <= 0 || len(r.SHA256) != 64 || source == "" {
		return r, ErrChanged
	}
	current, label, e := targetSnapshot(ctx, tx, t)
	if e != nil {
		return r, e
	}
	if current != snapshot {
		return r, ErrChanged
	}
	r.JobID = jobID
	r.TargetLabel = label
	ext := strings.ToLower(path.Ext(source))
	switch ext {
	case ".mp4", ".mkv", ".webm", ".mov", ".m4v":
	default:
		return r, ErrInput
	}
	if t.Kind == "custom" {
		if !storeVideoID.MatchString(videoID) {
			return r, ErrChanged
		}
		r.Destination = fmt.Sprintf("YouTube/%s [%d]/%s [%s]%s", organization.SafeComponent(label, 80), t.CollectionID, organization.SafeComponent(title, 100), videoID, ext)
	} else {
		snap, e := organization.New(s.db, cfg).CaptureTx(ctx, tx, t.MediaID)
		if e != nil {
			return r, e
		}
		var episodes []organization.Episode
		if t.Kind == "episode" {
			var ep organization.Episode
			e = tx.QueryRowContext(ctx, `SELECT e.id,s.number,e.number,e.title FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE e.id=? AND s.series_id=?`, t.EpisodeID, t.MediaID).Scan(&ep.ID, &ep.Season, &ep.Number, &ep.Title)
			if e != nil {
				return r, ErrChanged
			}
			episodes = []organization.Episode{ep}
		}
		r.Destination, e = organization.Destination(snap, episodes, source)
		if e != nil {
			return r, e
		}
	}
	var replacement int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements WHERE source_kind='youtube' AND job_id=?`, jobID).Scan(&replacement); e != nil {
		return r, e
	}
	if replacement > 0 {
		r.Destination = path.Join(".reelscarey-versions", "acquired-youtube-"+jobID+ext)
	} else {
		// YouTube imports use the same inspected quality label as regular
		// downloads: resolution plus an explicit HDR/SDR tag.
		var probe transfer.Probe
		if json.Unmarshal([]byte(r.Probe), &probe) == nil {
			if tag := organization.QualityTag(probe.Width, probe.Height, probe.HDR); tag != "" {
				destinationExt := path.Ext(r.Destination)
				r.Destination = strings.TrimSuffix(r.Destination, destinationExt) + " [" + tag + "]" + destinationExt
			}
		}
	}
	r.Subtitles = []SubtitleReview{}
	rows, e := tx.QueryContext(ctx, `SELECT language,size_bytes,sha256 FROM youtube_subtitle_sources WHERE job_id=? ORDER BY language`, jobID)
	if e != nil {
		return r, e
	}
	for rows.Next() {
		var sub SubtitleReview
		if e = rows.Scan(&sub.Language, &sub.Size, &sub.SHA256); e != nil {
			rows.Close()
			return r, e
		}
		if !subtitleLanguage.MatchString(sub.Language) {
			rows.Close()
			return r, ErrChanged
		}
		sub.Destination = strings.TrimSuffix(r.Destination, path.Ext(r.Destination)) + "." + sub.Language + ".vtt"
		r.Subtitles = append(r.Subtitles, sub)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return r, e
	}
	return r, nil
}

// ApproveImport compares exactly the evidence and destination shown in review.
// Publication and registration run later under a durable import journal.
func (s *Store) ApproveImport(ctx context.Context, cfg config.Config, review ImportReview) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	fresh, e := s.reviewImportTx(ctx, tx, cfg, review.JobID)
	if e != nil {
		return e
	}
	if fresh.Size != review.Size || fresh.SHA256 != review.SHA256 || fresh.Destination != review.Destination || fresh.Probe != review.Probe || fresh.TargetLabel != review.TargetLabel || !reflect.DeepEqual(fresh.Subtitles, review.Subtitles) {
		return ErrChanged
	}
	var source string
	var mtime int64
	if e = tx.QueryRowContext(ctx, `SELECT source_path,source_mtime FROM youtube_jobs WHERE id=?`, review.JobID).Scan(&source, &mtime); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO youtube_imports(job_id,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,probe_json,phase,updated_at) VALUES(?,?,?,?,?,?,?,?,'planned',unixepoch())`, review.JobID, source, fresh.Destination, ".reelscarey-youtube-"+review.JobID+".part", fresh.Size, mtime, fresh.SHA256, fresh.Probe)
	if e != nil {
		return e
	}
	for _, sub := range fresh.Subtitles {
		var source string
		var mtime int64
		if e = tx.QueryRowContext(ctx, `SELECT source_path,source_mtime FROM youtube_subtitle_sources WHERE job_id=? AND language=?`, review.JobID, sub.Language).Scan(&source, &mtime); e != nil {
			return e
		}
		stage := ".reelscarey-youtube-" + review.JobID + "-" + sub.Language + ".part"
		if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_subtitle_imports(job_id,language,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,phase) VALUES(?,?,?,?,?,?,?,?,'planned')`, review.JobID, sub.Language, source, sub.Destination, stage, sub.Size, mtime, sub.SHA256); e != nil {
			return e
		}
	}
	res, e := tx.ExecContext(ctx, `UPDATE youtube_jobs SET state='importing',message='Import approved and waiting for publication.',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state='awaiting_review'`, review.JobID)
	if e != nil {
		return e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return ErrChanged
	}
	return tx.Commit()
}

// ProcessImport handles one approved journal; source and stage are reverified
// after every restart. Unknown or conflicting files are never overwritten.
func (w *Worker) ProcessImport(ctx context.Context, cfg config.Config) (bool, error) {
	if w == nil || w.Store == nil || w.StagingRoot == "" || cfg.MediaDir == "" {
		return false, ErrInput
	}
	db := w.Store.db
	owner, e := id()
	if e != nil {
		return false, e
	}
	var jobID string
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	e = tx.QueryRowContext(ctx, `SELECT id FROM youtube_jobs WHERE state='importing' AND lease_until<unixepoch() ORDER BY updated_at,id LIMIT 1`).Scan(&jobID)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	res, e := tx.ExecContext(ctx, `UPDATE youtube_jobs SET lease_owner=?,lease_until=unixepoch()+60 WHERE id=? AND state='importing' AND lease_until<unixepoch()`, owner, jobID)
	if e != nil {
		return false, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return false, nil
	}
	if e = tx.Commit(); e != nil {
		return false, e
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go w.renew(work, cancel, done, jobID, owner, "importing")
	defer func() { cancel(); <-done }()
	e = w.executeImport(work, cfg, jobID, owner)
	if e == nil {
		e = w.CleanupCommitted(work, cfg)
	}
	if e != nil {
		_, _ = db.ExecContext(context.Background(), `UPDATE youtube_jobs SET message='Import needs attention; verified files were left in place.',lease_owner='',lease_until=unixepoch()+60,updated_at=unixepoch() WHERE id=? AND state='importing' AND lease_owner=?`, jobID, owner)
	}
	return true, e
}

func (w *Worker) executeImport(ctx context.Context, cfg config.Config, jobID, owner string) error {
	defer organization.ReadMedia()()
	db := w.Store.db
	if e := organization.EnsureMediaAvailable(ctx, db); e != nil {
		return e
	}
	stageRoot, e := filepath.EvalSymlinks(w.StagingRoot)
	if e != nil {
		return e
	}
	mediaRoot, e := filepath.EvalSymlinks(cfg.MediaDir)
	if e != nil {
		return e
	}
	if rootsOverlap(stageRoot, mediaRoot) {
		return ErrInput
	}
	if cfg.ConfigDir != "" {
		configRoot, err := filepath.EvalSymlinks(cfg.ConfigDir)
		if err != nil {
			return err
		}
		if rootsOverlap(configRoot, stageRoot) || rootsOverlap(configRoot, mediaRoot) {
			return ErrInput
		}
	}
	var source, dest, stage, hash, probe, phase string
	var size, mtime int64
	if e := db.QueryRowContext(ctx, `SELECT source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,probe_json,phase FROM youtube_imports WHERE job_id=?`, jobID).Scan(&source, &dest, &stage, &size, &mtime, &hash, &probe, &phase); e != nil {
		return e
	}
	if !safePath(source) || !safePath(dest) || !safePath(stage) || strings.Contains(stage, "/") {
		return ErrInput
	}
	srcRoot, e := os.OpenRoot(w.StagingRoot)
	if e != nil {
		return e
	}
	defer srcRoot.Close()
	if e = checkSegments(srcRoot, source, false); e != nil {
		return e
	}
	src, e := srcRoot.Open(filepath.FromSlash(source))
	if e != nil {
		return e
	}
	defer src.Close()
	info, e := src.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != size || info.ModTime().UnixNano() != mtime {
		return ErrChanged
	}
	actual, e := hashFile(ctx, src)
	if e != nil || actual != hash {
		return ErrChanged
	}
	dstRoot, e := os.OpenRoot(cfg.MediaDir)
	if e != nil {
		return e
	}
	defer dstRoot.Close()
	dir := path.Dir(dest)
	if e = checkSegments(dstRoot, dir, true); e != nil {
		return e
	}
	folder, e := dstRoot.OpenRoot(filepath.FromSlash(dir))
	if e != nil {
		return e
	}
	defer folder.Close()
	name := path.Base(dest)
	stageInfo, e := folder.Lstat(stage)
	if e == nil && phase == "planned" {
		return ErrChanged
	}
	if errors.Is(e, os.ErrNotExist) {
		if _, e = folder.Lstat(name); e == nil {
			return ErrBusy
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		var f *os.File
		f, e = folder.OpenFile(stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		e = copyVerified(ctx, src, f, size)
		if e == nil {
			e = f.Sync()
		}
		if closeErr := f.Close(); e == nil {
			e = closeErr
		}
		if e != nil {
			return e
		}
		stageInfo, e = folder.Lstat(stage)
	}
	if e != nil || !stageInfo.Mode().IsRegular() {
		return ErrChanged
	}
	stageFile, e := folder.OpenFile(stage, os.O_RDWR, 0)
	if e != nil {
		return e
	}
	opened, e := stageFile.Stat()
	if e != nil || !os.SameFile(stageInfo, opened) {
		stageFile.Close()
		return ErrChanged
	}
	if opened.Size() != size {
		e = ErrChanged
	}
	if e == nil {
		actual, e = hashFile(ctx, stageFile)
		if e == nil && (actual != hash) {
			e = ErrChanged
		}
	}
	if e == nil {
		e = stageFile.Sync()
	}
	if closeErr := stageFile.Close(); e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = w.importPhase(ctx, jobID, owner, "staged"); e != nil {
		return e
	}
	if e = folder.Link(stage, name); e != nil {
		if !errors.Is(e, os.ErrExist) {
			return e
		}
		existing, openErr := folder.Open(name)
		if openErr != nil {
			return ErrBusy
		}
		existingInfo, statErr := existing.Stat()
		existing.Close()
		if statErr != nil || !os.SameFile(opened, existingInfo) {
			return ErrBusy
		}
	}
	parent, e := folder.Open(".")
	if e != nil {
		return e
	}
	syncErr := parent.Sync()
	parent.Close()
	if syncErr != nil && runtime.GOOS != "windows" {
		return syncErr
	}
	if e = w.importPhase(ctx, jobID, owner, "published"); e != nil {
		return e
	}
	if e = w.publishSubtitles(ctx, srcRoot, folder, jobID, owner, dir); e != nil {
		return e
	}
	if e = w.register(ctx, jobID, owner, dest, size, hash, probe); e != nil {
		return e
	}
	return nil
}

// CleanupCommitted removes only a journal-owned staging link that is still the
// same file as the committed destination. Unknown or replaced files are kept.
func (w *Worker) CleanupCommitted(ctx context.Context, cfg config.Config) error {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, w.Store.db); e != nil {
		return e
	}
	rows, e := w.Store.db.QueryContext(ctx, `SELECT destination_path,stage_name FROM youtube_imports WHERE phase='committed' UNION ALL SELECT destination_path,stage_name FROM youtube_subtitle_imports WHERE phase='committed'`)
	if e != nil {
		return e
	}
	type pair struct{ dest, stage string }
	entries := []pair{}
	for rows.Next() {
		var p pair
		if e = rows.Scan(&p.dest, &p.stage); e != nil {
			rows.Close()
			return e
		}
		entries = append(entries, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	root, e := os.OpenRoot(cfg.MediaDir)
	if e != nil {
		return e
	}
	defer root.Close()
	for _, p := range entries {
		if !safePath(p.dest) || !safePath(p.stage) || strings.Contains(p.stage, "/") || !strings.HasPrefix(p.stage, ".reelscarey-youtube-") {
			continue
		}
		dir := path.Dir(p.dest)
		if e = checkSegments(root, dir, false); e != nil {
			continue
		}
		folder, err := root.OpenRoot(filepath.FromSlash(dir))
		if err != nil {
			continue
		}
		a, ea := folder.Lstat(p.stage)
		b, eb := folder.Lstat(path.Base(p.dest))
		if ea == nil && eb == nil && a.Mode().IsRegular() && b.Mode().IsRegular() && os.SameFile(a, b) {
			_ = folder.Remove(p.stage)
		}
		folder.Close()
	}
	return nil
}

type subtitlePlan struct {
	language, source, destination, stage, hash, phase string
	size, mtime                                       int64
}

func (w *Worker) publishSubtitles(ctx context.Context, srcRoot, folder *os.Root, jobID, owner, dir string) error {
	rows, e := w.Store.db.QueryContext(ctx, `SELECT language,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,phase FROM youtube_subtitle_imports WHERE job_id=? ORDER BY language`, jobID)
	if e != nil {
		return e
	}
	plans := []subtitlePlan{}
	for rows.Next() {
		var p subtitlePlan
		if e = rows.Scan(&p.language, &p.source, &p.destination, &p.stage, &p.size, &p.mtime, &p.hash, &p.phase); e != nil {
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
		if !subtitleLanguage.MatchString(p.language) || !safePath(p.source) || !safePath(p.destination) || !safePath(p.stage) || strings.Contains(p.stage, "/") || path.Dir(p.destination) != dir {
			return ErrInput
		}
		if e = checkSegments(srcRoot, p.source, false); e != nil {
			return e
		}
		src, e := srcRoot.Open(filepath.FromSlash(p.source))
		if e != nil {
			return e
		}
		info, e := src.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() != p.size || info.ModTime().UnixNano() != p.mtime {
			src.Close()
			return ErrChanged
		}
		actual, e := hashFile(ctx, src)
		if e != nil || actual != p.hash {
			src.Close()
			return ErrChanged
		}
		name := path.Base(p.destination)
		st, e := folder.Lstat(p.stage)
		if e == nil && p.phase == "planned" {
			src.Close()
			return ErrChanged
		}
		if errors.Is(e, os.ErrNotExist) {
			if _, err := folder.Lstat(name); err == nil {
				src.Close()
				return ErrBusy
			} else if !errors.Is(err, os.ErrNotExist) {
				src.Close()
				return err
			}
			var f *os.File
			f, e = folder.OpenFile(p.stage, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
			if e == nil {
				e = copyVerified(ctx, src, f, p.size)
			}
			if e == nil {
				e = f.Sync()
			}
			if f != nil {
				if err := f.Close(); e == nil {
					e = err
				}
			}
			if e == nil {
				st, e = folder.Lstat(p.stage)
			}
		}
		if e != nil || !st.Mode().IsRegular() {
			src.Close()
			return ErrChanged
		}
		f, e := folder.OpenFile(p.stage, os.O_RDWR, 0)
		if e != nil {
			src.Close()
			return e
		}
		opened, e := f.Stat()
		if e != nil || !os.SameFile(st, opened) {
			f.Close()
			src.Close()
			return ErrChanged
		}
		if opened.Size() != p.size {
			e = ErrChanged
		}
		if e == nil {
			actual, e = hashFile(ctx, f)
			if e == nil && actual != p.hash {
				e = ErrChanged
			}
		}
		if e == nil {
			e = f.Sync()
		}
		if err := f.Close(); e == nil {
			e = err
		}
		src.Close()
		if e != nil {
			return e
		}
		if e = w.subtitlePhase(ctx, jobID, owner, p.language, "staged"); e != nil {
			return e
		}
		if e = folder.Link(p.stage, name); e != nil {
			if !errors.Is(e, os.ErrExist) {
				return e
			}
			existing, err := folder.Open(name)
			if err != nil {
				return ErrBusy
			}
			existInfo, err := existing.Stat()
			existing.Close()
			if err != nil || !os.SameFile(opened, existInfo) {
				return ErrBusy
			}
		}
		parent, e := folder.Open(".")
		if e != nil {
			return e
		}
		syncErr := parent.Sync()
		parent.Close()
		if syncErr != nil && runtime.GOOS != "windows" {
			return syncErr
		}
		if e = w.subtitlePhase(ctx, jobID, owner, p.language, "published"); e != nil {
			return e
		}
	}
	return nil
}
func (w *Worker) subtitlePhase(ctx context.Context, jobID, owner, language, phase string) error {
	r, e := w.Store.db.ExecContext(ctx, `UPDATE youtube_subtitle_imports SET phase=? WHERE job_id=? AND language=? AND EXISTS(SELECT 1 FROM youtube_jobs WHERE id=? AND state='importing' AND lease_owner=? AND lease_until>=unixepoch())`, phase, jobID, language, jobID, owner)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrChanged
	}
	return nil
}

func rootsOverlap(a, b string) bool {
	inside := func(parent, child string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && (rel == "." || filepath.IsLocal(rel))
	}
	return inside(a, b) || inside(b, a)
}

func safePath(v string) bool {
	if v == "" || path.IsAbs(v) || strings.ContainsAny(v, `\:`) {
		return false
	}
	for _, p := range strings.Split(v, "/") {
		if p == "" || p == "." || p == ".." {
			return false
		}
	}
	return true
}
func checkSegments(root *os.Root, p string, create bool) error {
	if p == "." {
		return nil
	}
	if !safePath(p) {
		return ErrInput
	}
	parts := strings.Split(p, "/")
	for i := range parts {
		name := filepath.FromSlash(strings.Join(parts[:i+1], "/"))
		st, e := root.Lstat(name)
		if errors.Is(e, os.ErrNotExist) && create {
			if e = root.Mkdir(name, 0755); e != nil && !errors.Is(e, os.ErrExist) {
				return e
			}
			st, e = root.Lstat(name)
		}
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 || i < len(parts)-1 && !st.IsDir() || create && !st.IsDir() {
			return ErrChanged
		}
	}
	return nil
}
func hashFile(ctx context.Context, f *os.File) (string, error) {
	if _, e := f.Seek(0, 0); e != nil {
		return "", e
	}
	h := sha256.New()
	_, e := io.Copy(h, &cancelReader{ctx: ctx, r: f})
	return hex.EncodeToString(h.Sum(nil)), e
}
func copyVerified(ctx context.Context, src, dst *os.File, size int64) error {
	info, e := dst.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > size {
		return ErrChanged
	}
	offset := info.Size()
	if _, e = src.Seek(0, 0); e != nil {
		return e
	}
	if _, e = dst.Seek(0, 0); e != nil {
		return e
	}
	a, b := sha256.New(), sha256.New()
	if _, e = io.CopyN(a, &cancelReader{ctx: ctx, r: src}, offset); e != nil {
		return e
	}
	if _, e = io.CopyN(b, &cancelReader{ctx: ctx, r: dst}, offset); e != nil {
		return e
	}
	if !strings.EqualFold(hex.EncodeToString(a.Sum(nil)), hex.EncodeToString(b.Sum(nil))) {
		return ErrChanged
	}
	if _, e = dst.Seek(offset, 0); e != nil {
		return e
	}
	_, e = io.Copy(dst, &cancelReader{ctx: ctx, r: io.LimitReader(src, size-offset)})
	return e
}

func (w *Worker) renew(ctx context.Context, cancel context.CancelFunc, done chan<- struct{}, jobID, owner, state string) {
	defer close(done)
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r, err := w.Store.db.ExecContext(ctx, `UPDATE youtube_jobs SET lease_until=unixepoch()+60 WHERE id=? AND state=? AND lease_owner=? AND lease_until>=unixepoch()`, jobID, state, owner)
			if err != nil {
				cancel()
				return
			}
			n, err := r.RowsAffected()
			if err != nil || n != 1 {
				cancel()
				return
			}
		}
	}
}

func (w *Worker) importPhase(ctx context.Context, jobID, owner, phase string) error {
	r, err := w.Store.db.ExecContext(ctx, `UPDATE youtube_imports SET phase=?,updated_at=unixepoch() WHERE job_id=? AND EXISTS (SELECT 1 FROM youtube_jobs WHERE id=? AND state='importing' AND lease_owner=? AND lease_until>=unixepoch())`, phase, jobID, jobID, owner)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrChanged
	}
	return nil
}

func (w *Worker) register(ctx context.Context, jobID, owner, dest string, size int64, hash, probe string) error {
	tx, err := w.Store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var t Target
	var video, title, snapshot string
	err = tx.QueryRowContext(ctx, `SELECT target_kind,COALESCE(collection_id,0),COALESCE(media_id,0),COALESCE(episode_id,0),video_id,video_title,target_snapshot FROM youtube_jobs WHERE id=? AND state='importing' AND lease_owner=? AND lease_until>=unixepoch()`, jobID, owner).Scan(&t.Kind, &t.CollectionID, &t.MediaID, &t.EpisodeID, &video, &title, &snapshot)
	if err != nil {
		return ErrChanged
	}
	current, _, err := targetSnapshot(ctx, tx, t)
	if err != nil {
		return err
	}
	if current != snapshot {
		return ErrChanged
	}
	var replacement int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements WHERE source_kind='youtube' AND job_id=?`, jobID).Scan(&replacement); err != nil {
		return err
	}
	if replacement > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE acquisition_replacements SET source_path=?,size_bytes=?,sha256=?,probe_json=? WHERE source_kind='youtube' AND job_id=?`, dest, size, hash, probe, jobID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE youtube_imports SET phase='committed' WHERE job_id=?`, jobID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE youtube_subtitle_imports SET phase='committed' WHERE job_id=?`, jobID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE youtube_jobs SET state='awaiting_review',message='Verified acquisition retained. Review replacement in Organize library.',lease_owner='',lease_until=0 WHERE id=?`, jobID); err != nil {
			return err
		}
		return tx.Commit()
	}
	var fileID any
	if t.Kind != "custom" {
		var movie, series any
		if t.Kind == "movie" {
			movie = t.MediaID
		} else {
			series = t.MediaID
		}
		r, e := tx.ExecContext(ctx, `INSERT INTO media_files(movie_id,series_id,download_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(?,?,NULL,-1,?,?,?,?,unixepoch(),'existing',?)`, movie, series, dest, size, hash, probe, title)
		if e != nil {
			return e
		}
		fid, e := r.LastInsertId()
		if e != nil {
			return e
		}
		fileID = fid
		if t.Kind == "episode" {
			if _, e = tx.ExecContext(ctx, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(?,?)`, t.EpisodeID, fid); e != nil {
				return e
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO youtube_files(job_id,collection_id,media_file_id,video_id,title,relative_path,size_bytes,sha256,probe_json,imported_at) VALUES(?,?,?,?,?,?,?,?,?,unixepoch())`, jobID, nullable(t.CollectionID), fileID, video, title, dest, size, hash, probe); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT language,destination_path,size_bytes,sha256,phase FROM youtube_subtitle_imports WHERE job_id=? ORDER BY language`, jobID)
	if err != nil {
		return err
	}
	var subtitles []subtitlePlan
	for rows.Next() {
		var p subtitlePlan
		var phase string
		if err = rows.Scan(&p.language, &p.destination, &p.size, &p.hash, &phase); err != nil {
			rows.Close()
			return err
		}
		if phase != "published" {
			rows.Close()
			return ErrChanged
		}
		subtitles = append(subtitles, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, sub := range subtitles {
		if _, err = tx.ExecContext(ctx, `INSERT INTO youtube_subtitle_files(job_id,language,relative_path,sha256,size_bytes) VALUES(?,?,?,?,?)`, jobID, sub.language, sub.destination, sub.hash, sub.size); err != nil {
			return err
		}
		if fileID != nil {
			if _, err = tx.ExecContext(ctx, `INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,updated_at) VALUES(?,?,'full','downloaded',?,?,unixepoch())`, fileID, sub.language, sub.destination, sub.hash); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE youtube_subtitle_imports SET phase='committed' WHERE job_id=? AND phase='published'`, jobID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE youtube_imports SET phase='committed',updated_at=unixepoch() WHERE job_id=? AND phase='published'`, jobID); err != nil {
		return err
	}
	r, err := tx.ExecContext(ctx, `UPDATE youtube_jobs SET state='completed',message='Imported.',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state='importing' AND lease_owner=? AND lease_until>=unixepoch()`, jobID, owner)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		return ErrChanged
	}
	return tx.Commit()
}
