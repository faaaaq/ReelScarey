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
)

// ErrEnglishSubtitles blocks importing a verified video whose required
// English captions were never acquired. The staged video and every downloaded
// track are retained for review; nothing is deleted.
var ErrEnglishSubtitles = errors.New("English subtitles are required before this download can be imported. Retry the subtitles or choose another source.")

// maxSubtitleLanguages matches the DownloadOptions cap; one slot is reserved
// for the mandatory English track when the saved options omit it.
const maxSubtitleLanguages = 8

func hasEnglishSubtitle(subtitles []SubtitleReview) bool {
	for _, sub := range subtitles {
		if isEnglishSubtitle(sub.Language) {
			return true
		}
	}
	return false
}

// isEnglishSubtitle reports whether a language tag names English audio-side
// caption content: `en` and regional variants such as `en-US`/`en-GB`.
func isEnglishSubtitle(language string) bool {
	lower := strings.ToLower(language)
	return lower == "en" || strings.HasPrefix(lower, "en-")
}

// requiredLanguages merges the mandatory English track into the requested
// set. A video with no English captions can never satisfy the subtitle
// requirement, so English is always requested (manual first, auto fallback).
func requiredLanguages(requested []string) []string {
	for _, language := range requested {
		if isEnglishSubtitle(language) {
			return requested
		}
	}
	// Keep room for the mandatory English track; the last extra language is
	// dropped rather than failing the whole download.
	out := requested
	if len(out) >= maxSubtitleLanguages {
		out = out[:maxSubtitleLanguages-1]
	}
	return append(append([]string{}, out...), "en")
}

// RetrySubtitles re-fetches captions for a verified download without
// redownloading the video (yt-dlp --skip-download). New caption evidence is
// journaled like an ordinary download and the job returns to review; import
// stays blocked until English captions are present.
func (w *Worker) RetrySubtitles(ctx context.Context, jobID string) (bool, error) {
	if w == nil || w.Store == nil || w.Downloader == nil || w.StagingRoot == "" || !textOK(jobID, 64) {
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
	var vid, source string
	e = tx.QueryRowContext(ctx, `SELECT video_id,source_path FROM youtube_jobs WHERE id=? AND state='awaiting_review' AND source_path<>''`, jobID).Scan(&vid, &source)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	res, e := tx.ExecContext(ctx, `UPDATE youtube_jobs SET lease_owner=?,lease_until=unixepoch()+300,updated_at=unixepoch() WHERE id=? AND state='awaiting_review' AND lease_until<unixepoch()`, owner, jobID)
	if e != nil {
		return false, e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return false, nil
	}
	var rawLanguages string
	var automatic bool
	if e = tx.QueryRowContext(ctx, `SELECT o.languages_json,o.automatic FROM youtube_jobs j JOIN youtube_subtitle_options o ON o.review_id=j.review_id WHERE j.id=?`, jobID).Scan(&rawLanguages, &automatic); e != nil {
		return false, e
	}
	if e = tx.Commit(); e != nil {
		return false, e
	}
	var opts DownloadOptions
	opts.Automatic = automatic
	opts.SkipDownload = true
	if e = json.Unmarshal([]byte(rawLanguages), &opts.Languages); e != nil {
		return false, e
	}
	opts.Languages = requiredLanguages(opts.Languages)
	if !validSubtitleOptions(opts) {
		return false, ErrInput
	}
	work, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	release := func() {
		_, _ = db.ExecContext(context.Background(), `UPDATE youtube_jobs SET lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND lease_owner=?`, jobID, owner)
	}
	defer release()
	rootPath, e := filepath.Abs(w.StagingRoot)
	if e != nil {
		return false, e
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(rootPath))
	if e != nil {
		return false, e
	}
	for _, other := range []string{w.MediaRoot, w.ConfigRoot} {
		if other == "" {
			continue
		}
		resolved, err := filepath.EvalSymlinks(other)
		if err != nil {
			return false, err
		}
		if rootsOverlap(parent, resolved) {
			return false, ErrInput
		}
	}
	if e = os.MkdirAll(rootPath, 0700); e != nil {
		return false, e
	}
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return false, e
	}
	defer root.Close()
	attempt, e := id()
	if e != nil {
		return false, e
	}
	dir := jobID + "-" + attempt
	if e = root.Mkdir(dir, 0700); e != nil {
		return false, e
	}
	downloader, ok := w.Downloader.(subtitleDownloader)
	if !ok {
		return false, ErrDownload
	}
	result, e := downloader.DownloadWithSubtitles(work, vid, filepath.Join(rootPath, dir), opts)
	if e != nil {
		// The video is untouched: report the retry honestly and keep the job
		// reviewable with its earlier evidence. The reason text stays bounded
		// so a yt-dlp failure cannot flood the queue message.
		reason := e.Error()
		if len(reason) > 200 {
			reason = reason[:200]
		}
		_, _ = db.ExecContext(context.Background(), `UPDATE youtube_jobs SET message='English subtitle retry failed ('||?||'); the verified video is unchanged and import stays blocked.',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state='awaiting_review' AND lease_owner=?`, reason, jobID, owner)
		_ = root.Remove(dir)
		return true, nil
	}
	attemptRoot, e := root.OpenRoot(dir)
	if e != nil {
		return false, e
	}
	defer attemptRoot.Close()
	type subEvidence struct {
		language, path, hash string
		size, mtime          int64
	}
	evidence := []subEvidence{}
	for _, sub := range result.Subtitles {
		if sub.Name != vid+"."+sub.Language+".vtt" {
			return false, ErrDownload
		}
		st, err := attemptRoot.Lstat(sub.Name)
		if err != nil || !st.Mode().IsRegular() || st.Size() < 1 || st.Size() > 4<<20 {
			return false, ErrDownload
		}
		sf, err := attemptRoot.Open(sub.Name)
		if err != nil {
			return false, err
		}
		content, err := io.ReadAll(io.LimitReader(&cancelReader{ctx: work, r: sf}, (4<<20)+1))
		afterSub, statErr := sf.Stat()
		sf.Close()
		if err != nil || statErr != nil || afterSub.Size() != st.Size() || afterSub.ModTime() != st.ModTime() {
			return false, ErrDownload
		}
		if len(content) > 4<<20 || !utf8.Valid(content) || !strings.HasPrefix(strings.TrimPrefix(string(content), "\ufeff"), "WEBVTT") {
			return false, ErrDownload
		}
		sum := sha256.Sum256(content)
		evidence = append(evidence, subEvidence{sub.Language, filepath.ToSlash(filepath.Join(dir, sub.Name)), hex.EncodeToString(sum[:]), st.Size(), st.ModTime().UnixNano()})
	}
	englishPresent := false
	for _, sub := range evidence {
		if isEnglishSubtitle(sub.language) {
			englishPresent = true
			break
		}
	}
	tx, e = db.BeginTx(work, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	for _, sub := range evidence {
		// Never overwrite or duplicate an already journaled track.
		var existing int
		if e = tx.QueryRowContext(work, `SELECT COUNT(*) FROM youtube_subtitle_sources WHERE job_id=? AND language=?`, jobID, sub.language).Scan(&existing); e != nil {
			return false, e
		}
		if existing > 0 {
			continue
		}
		if _, e = tx.ExecContext(work, `INSERT INTO youtube_subtitle_sources(job_id,language,source_path,size_bytes,source_mtime,sha256) VALUES(?,?,?,?,?,?)`, jobID, sub.language, sub.path, sub.size, sub.mtime, sub.hash); e != nil {
			return false, e
		}
	}
	message := "English subtitles remain unavailable; import stays blocked."
	if englishPresent {
		message = "English subtitles acquired. Review the file and target before import."
	}
	if _, e = tx.ExecContext(work, `UPDATE youtube_jobs SET message=?,lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state='awaiting_review' AND lease_owner=?`, message, jobID, owner); e != nil {
		return false, e
	}
	if e = tx.Commit(); e != nil {
		return false, e
	}
	if englishPresent {
		var kind string
		var auto bool
		if e = db.QueryRowContext(work, `SELECT target_kind,auto_import FROM youtube_jobs WHERE id=?`, jobID).Scan(&kind, &auto); e == nil && kind == "episode" && auto && w.Config.ConfigDir != "" {
			review, reviewErr := w.Store.ReviewImport(work, w.Config, jobID)
			if reviewErr == nil {
				reviewErr = w.Store.ApproveImport(work, w.Config, review)
			}
			if reviewErr != nil {
				_, _ = db.ExecContext(context.Background(), `UPDATE youtube_jobs SET message='Verified. Automatic import needs review before it can continue.' WHERE id=? AND state='awaiting_review'`, jobID)
			}
		}
	}
	return true, nil
}
