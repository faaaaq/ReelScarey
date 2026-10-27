package youtube

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type Downloader interface {
	Download(context.Context, string, string) (string, error)
}
type subtitleDownloader interface {
	DownloadWithSubtitles(context.Context, string, string, DownloadOptions) (DownloadResult, error)
}
type Worker struct {
	Store       *Store
	Downloader  Downloader
	Prober      transfer.Prober
	StagingRoot string
	MediaRoot   string
	ConfigRoot  string
}

// ProcessOne claims one durable job. Every attempt gets a new private directory;
// uncertain partial output from an earlier attempt is retained, never deleted or
// reinterpreted as a completed video. A verified file waits for import review.
func (w *Worker) ProcessOne(ctx context.Context) (bool, error) {
	if w == nil || w.Store == nil || w.Downloader == nil || w.Prober == nil || w.StagingRoot == "" {
		return false, ErrInput
	}
	db := w.Store.db
	owner, e := id()
	if e != nil {
		return false, e
	}
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	var jobID, vid, snapshot string
	var target Target
	e = tx.QueryRowContext(ctx, `SELECT id,video_id,target_kind,COALESCE(collection_id,0),COALESCE(media_id,0),COALESCE(episode_id,0),target_snapshot FROM youtube_jobs WHERE state='queued' OR (state='downloading' AND lease_until<unixepoch()) ORDER BY created_at,id LIMIT 1`).Scan(&jobID, &vid, &target.Kind, &target.CollectionID, &target.MediaID, &target.EpisodeID, &snapshot)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	current, _, e := targetSnapshot(ctx, tx, target)
	if e != nil || current != snapshot {
		_, updateErr := tx.ExecContext(ctx, `UPDATE youtube_jobs SET state='failed',message='Target changed or is unavailable; review again.',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=?`, jobID)
		if updateErr != nil {
			return false, updateErr
		}
		return true, tx.Commit()
	}
	res, e := tx.ExecContext(ctx, `UPDATE youtube_jobs SET state='downloading',lease_owner=?,lease_until=unixepoch()+60,updated_at=unixepoch() WHERE id=? AND (state='queued' OR (state='downloading' AND lease_until<unixepoch()))`, owner, jobID)
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
	go func() {
		defer close(done)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-work.Done():
				return
			case <-ticker.C:
				r, err := db.ExecContext(work, `UPDATE youtube_jobs SET lease_until=unixepoch()+60 WHERE id=? AND state='downloading' AND lease_owner=? AND lease_until>=unixepoch()`, jobID, owner)
				if err != nil {
					cancel()
					return
				}
				count, _ := r.RowsAffected()
				if count != 1 {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	fail := func(err error) (bool, error) {
		if ctx.Err() != nil {
			return true, ctx.Err()
		}
		_, dbErr := db.ExecContext(context.Background(), `UPDATE youtube_jobs SET state='failed',message='Acquisition failed. The isolated files were retained for review.',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state='downloading' AND lease_owner=?`, jobID, owner)
		if dbErr != nil {
			return true, dbErr
		}
		return true, err
	}
	rootPath, e := filepath.Abs(w.StagingRoot)
	if e != nil {
		return fail(e)
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(rootPath))
	if e != nil {
		return fail(e)
	}
	for _, other := range []string{w.MediaRoot, w.ConfigRoot} {
		if other == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(other)
		if err != nil {
			return fail(err)
		}
		if rootsOverlap(parent, resolved) {
			return fail(ErrInput)
		}
	}
	if e = os.MkdirAll(rootPath, 0700); e != nil {
		return fail(e)
	}
	rootInfo, e := os.Lstat(rootPath)
	if e != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return fail(ErrChanged)
	}
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return fail(e)
	}
	defer root.Close()
	attempt, e := id()
	if e != nil {
		return fail(e)
	}
	dir := jobID + "-" + attempt
	if e = root.Mkdir(dir, 0700); e != nil {
		return fail(e)
	}
	var rawLanguages string
	var automatic bool
	e = db.QueryRowContext(work, `SELECT o.languages_json,o.automatic FROM youtube_jobs j JOIN youtube_subtitle_options o ON o.review_id=j.review_id WHERE j.id=?`, jobID).Scan(&rawLanguages, &automatic)
	if e != nil {
		return fail(e)
	}
	var opts DownloadOptions
	opts.Automatic = automatic
	if e = json.Unmarshal([]byte(rawLanguages), &opts.Languages); e != nil || !validSubtitleOptions(opts) {
		return fail(ErrInput)
	}
	var name string
	subtitleResults := []SubtitleFile{}
	missing := []string{}
	if len(opts.Languages) > 0 {
		downloader, ok := w.Downloader.(subtitleDownloader)
		if !ok {
			return fail(ErrDownload)
		}
		var result DownloadResult
		result, e = downloader.DownloadWithSubtitles(work, vid, filepath.Join(rootPath, dir), opts)
		name = result.Video
		subtitleResults = result.Subtitles
		missing = result.MissingLanguages
	} else {
		name, e = w.Downloader.Download(work, vid, filepath.Join(rootPath, dir))
	}
	if e != nil {
		return fail(e)
	}
	if filepath.Base(name) != name || !strings.HasPrefix(name, vid+".") {
		return fail(ErrDownload)
	}
	attemptRoot, e := root.OpenRoot(dir)
	if e != nil {
		return fail(e)
	}
	defer attemptRoot.Close()
	info, e := attemptRoot.Lstat(name)
	if e != nil || !info.Mode().IsRegular() || info.Size() <= 0 {
		return fail(ErrDownload)
	}
	f, e := attemptRoot.Open(name)
	if e != nil {
		return fail(e)
	}
	defer f.Close()
	opened, e := f.Stat()
	if e != nil || !os.SameFile(info, opened) {
		return fail(ErrDownload)
	}
	probe, e := w.Prober.Probe(work, f)
	if e != nil {
		return fail(e)
	}
	if _, e = f.Seek(0, 0); e != nil {
		return fail(e)
	}
	hash := sha256.New()
	if _, e = io.Copy(hash, &cancelReader{ctx: work, r: f}); e != nil {
		return fail(e)
	}
	after, e := f.Stat()
	if e != nil || after.Size() != opened.Size() || after.ModTime() != opened.ModTime() {
		return fail(ErrDownload)
	}
	raw, e := json.Marshal(probe)
	if e != nil {
		return fail(e)
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	relative := filepath.ToSlash(filepath.Join(dir, name))
	type subEvidence struct {
		language, path, hash string
		size, mtime          int64
	}
	evidence := []subEvidence{}
	seen := map[string]bool{}
	for _, sub := range subtitleResults {
		key := strings.ToLower(sub.Language)
		allowed := false
		for _, v := range opts.Languages {
			if strings.EqualFold(v, sub.Language) {
				allowed = true
				break
			}
		}
		if !allowed || seen[key] || sub.Name != vid+"."+sub.Language+".vtt" {
			return fail(ErrDownload)
		}
		seen[key] = true
		st, err := attemptRoot.Lstat(sub.Name)
		if err != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > 4<<20 {
			return fail(ErrDownload)
		}
		sf, err := attemptRoot.Open(sub.Name)
		if err != nil {
			return fail(err)
		}
		openedSub, err := sf.Stat()
		if err != nil || !os.SameFile(st, openedSub) {
			sf.Close()
			return fail(ErrDownload)
		}
		content, err := io.ReadAll(io.LimitReader(&cancelReader{ctx: work, r: sf}, (4<<20)+1))
		if err != nil || len(content) > 4<<20 || !utf8.Valid(content) || !strings.HasPrefix(strings.TrimPrefix(string(content), "\ufeff"), "WEBVTT") {
			sf.Close()
			return fail(ErrDownload)
		}
		sh := sha256.New()
		_, err = sh.Write(content)
		afterSub, statErr := sf.Stat()
		sf.Close()
		if err != nil || statErr != nil || afterSub.Size() != st.Size() || afterSub.ModTime() != st.ModTime() {
			return fail(ErrDownload)
		}
		evidence = append(evidence, subEvidence{sub.Language, filepath.ToSlash(filepath.Join(dir, sub.Name)), hex.EncodeToString(sh.Sum(nil)), st.Size(), st.ModTime().UnixNano()})
	}
	missing = missing[:0]
	for _, lang := range opts.Languages {
		if !seen[strings.ToLower(lang)] {
			missing = append(missing, lang)
		}
	}
	message := "Download verified. Review the file and target before import."
	if len(missing) > 0 {
		message += " Requested subtitles unavailable: " + strings.Join(missing, ", ") + "."
	}
	tx, e = db.BeginTx(work, nil)
	if e != nil {
		return fail(e)
	}
	defer tx.Rollback()
	for _, sub := range evidence {
		if _, e = tx.ExecContext(work, `INSERT INTO youtube_subtitle_sources(job_id,language,source_path,size_bytes,source_mtime,sha256) VALUES(?,?,?,?,?,?)`, jobID, sub.language, sub.path, sub.size, sub.mtime, sub.hash); e != nil {
			_ = tx.Rollback()
			return fail(e)
		}
	}
	res, e = tx.ExecContext(work, `UPDATE youtube_jobs SET state='awaiting_review',source_path=?,source_size=?,source_mtime=?,source_sha256=?,probe_json=?,message=?,lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state='downloading' AND lease_owner=? AND lease_until>=unixepoch()`, relative, opened.Size(), opened.ModTime().UnixNano(), sum, string(raw), message, jobID, owner)
	if e != nil {
		_ = tx.Rollback()
		return fail(e)
	}
	n, _ = res.RowsAffected()
	if n != 1 {
		return true, ErrChanged
	}
	if e = tx.Commit(); e != nil {
		return fail(e)
	}
	return true, nil
}

type cancelReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *cancelReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(p)
}
