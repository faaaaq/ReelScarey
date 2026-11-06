package youtube

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/organization"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

var ErrInput = errors.New("Choose a valid YouTube video and an existing target.")
var ErrChanged = errors.New("The target or review changed or expired. Review again.")
var ErrBusy = errors.New("The target already has a file or an active download.")
var storeVideoID = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type Collection struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Revision int    `json:"revision"`
}
type Video struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Channel string `json:"channel"`
}
type Target struct {
	Kind         string `json:"kind"`
	CollectionID int64  `json:"collection_id,omitempty"`
	MediaID      int64  `json:"media_id,omitempty"`
	EpisodeID    int64  `json:"episode_id,omitempty"`
}
type Review struct {
	ID          string          `json:"id"`
	Video       Video           `json:"video"`
	Target      Target          `json:"target"`
	TargetLabel string          `json:"target_label"`
	ExpiresAt   int64           `json:"expires_at"`
	Subtitles   DownloadOptions `json:"subtitles"`
}
type Job struct {
	ID             string  `json:"id"`
	Video          Video   `json:"video"`
	Target         Target  `json:"target"`
	TargetLabel    string  `json:"target_label"`
	State          string  `json:"state"`
	Message        string  `json:"message"`
	Quality        string  `json:"quality"`
	Progress       float64 `json:"progress"`
	Downloaded     int64   `json:"downloaded_bytes"`
	Total          int64   `json:"total_bytes"`
	Speed          int64   `json:"speed_bps"`
	ETA            int64   `json:"eta_seconds"`
	AutoImport     bool    `json:"auto_import"`
	SeriesSourceID string  `json:"series_source_id,omitempty"`
	CreatedAt      int64   `json:"created_at"`
	UpdatedAt      int64   `json:"updated_at"`
}
type File struct {
	JobID        string `json:"job_id"`
	Title        string `json:"title"`
	CollectionID int64  `json:"collection_id"`
	Collection   string `json:"collection"`
	Path         string `json:"relative_path"`
	Size         int64  `json:"size_bytes"`
}

func textOK(s string, max int) bool {
	return len(s) > 0 && len(s) <= max && utf8.ValidString(s) && !strings.ContainsFunc(s, unicode.IsControl)
}
func id() (string, error) {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}
func nullable(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func (s *Store) CreateCollection(ctx context.Context, title string) (Collection, error) {
	title = strings.TrimSpace(title)
	if !textOK(title, 200) {
		return Collection{}, ErrInput
	}
	r, e := s.db.ExecContext(ctx, `INSERT INTO youtube_collections(title,created_at) VALUES(?,unixepoch())`, title)
	if e != nil {
		return Collection{}, e
	}
	n, e := r.LastInsertId()
	return Collection{ID: n, Title: title, Revision: 1}, e
}
func (s *Store) Collections(ctx context.Context) ([]Collection, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT id,title,revision FROM youtube_collections ORDER BY title COLLATE NOCASE,id LIMIT 500`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Collection{}
	for rows.Next() {
		var v Collection
		if e = rows.Scan(&v.ID, &v.Title, &v.Revision); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func targetSnapshot(ctx context.Context, q queryer, t Target) (string, string, error) {
	var title, extra string
	var a, b int64
	switch t.Kind {
	case "custom":
		if t.CollectionID < 1 || t.MediaID != 0 || t.EpisodeID != 0 {
			return "", "", ErrInput
		}
		if e := q.QueryRowContext(ctx, `SELECT title,revision FROM youtube_collections WHERE id=?`, t.CollectionID).Scan(&title, &a); e != nil {
			return "", "", ErrChanged
		}
	case "movie":
		if t.CollectionID != 0 || t.MediaID < 1 || t.EpisodeID != 0 {
			return "", "", ErrInput
		}
		if e := q.QueryRowContext(ctx, `SELECT title,COALESCE(year,0),tmdb_id FROM media_items WHERE id=? AND kind='movie'`, t.MediaID).Scan(&title, &a, &b); e != nil {
			return "", "", ErrChanged
		}
		var count int
		if e := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM downloads WHERE movie_id=? AND state NOT IN ('completed','cancelled')`, t.MediaID).Scan(&count); e != nil {
			return "", "", e
		}
		if count > 0 {
			return "", "", ErrBusy
		}
	case "episode":
		if t.CollectionID != 0 || t.MediaID < 1 || t.EpisodeID < 1 {
			return "", "", ErrInput
		}
		if e := q.QueryRowContext(ctx, `SELECT m.title,e.title,s.number,e.number FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE e.id=? AND m.id=? AND m.kind='series'`, t.EpisodeID, t.MediaID).Scan(&title, &extra, &a, &b); e != nil {
			return "", "", ErrChanged
		}
		var count int
		if e := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode_downloads WHERE episode_id=?`, t.EpisodeID).Scan(&count); e != nil {
			return "", "", e
		}
		if count > 0 {
			return "", "", ErrBusy
		}
	default:
		return "", "", ErrInput
	}
	fileID, version, e := replacementTarget(ctx, q, t)
	if e != nil {
		return "", "", e
	}
	if fileID > 0 {
		extra += " (replacement; original retained)"
	}
	raw, _ := json.Marshal(struct {
		Target       Target
		Title, Extra string
		A, B         int64
		FileVersion  string
	}{t, title, extra, a, b, version})
	label := title
	if extra != "" {
		label += " — " + extra
	}
	return string(raw), label, nil
}

// Preview accepts metadata obtained from the server's inspector, not from an
// untrusted client. It never starts acquisition or touches media files.
func (s *Store) Preview(ctx context.Context, v Video, t Target) (Review, error) {
	return s.PreviewWithSubtitles(ctx, v, t, DownloadOptions{})
}
func (s *Store) PreviewWithSubtitles(ctx context.Context, v Video, t Target, subtitles DownloadOptions) (Review, error) {
	if !validSubtitleOptions(subtitles) {
		return Review{}, ErrInput
	}
	if subtitles.Languages == nil {
		subtitles.Languages = []string{}
	}
	v.Title = strings.TrimSpace(v.Title)
	v.Channel = strings.TrimSpace(v.Channel)
	if !storeVideoID.MatchString(v.ID) || !textOK(v.Title, 500) || len(v.Channel) > 500 || !utf8.ValidString(v.Channel) || strings.ContainsFunc(v.Channel, unicode.IsControl) {
		return Review{}, ErrInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Review{}, e
	}
	defer tx.Rollback()
	snap, label, e := targetSnapshot(ctx, tx, t)
	if e != nil {
		return Review{}, e
	}
	key, e := id()
	if e != nil {
		return Review{}, e
	}
	expiry := time.Now().Unix() + 1800
	_, e = tx.ExecContext(ctx, `INSERT INTO youtube_reviews(id,video_id,video_title,channel,target_kind,collection_id,media_id,episode_id,target_snapshot,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, key, v.ID, v.Title, v.Channel, t.Kind, nullable(t.CollectionID), nullable(t.MediaID), nullable(t.EpisodeID), snap, expiry)
	if e != nil {
		return Review{}, e
	}
	languages, e := json.Marshal(subtitles.Languages)
	if e != nil {
		return Review{}, e
	}
	quality, _ := normalizedQuality(subtitles.Quality)
	if _, e = tx.ExecContext(ctx, `INSERT INTO youtube_subtitle_options(review_id,languages_json,automatic,quality) VALUES(?,?,?,?)`, key, string(languages), subtitles.Automatic, quality); e != nil {
		return Review{}, e
	}
	if e = tx.Commit(); e != nil {
		return Review{}, e
	}
	return Review{ID: key, Video: v, Target: t, TargetLabel: label, ExpiresAt: expiry, Subtitles: subtitles}, nil
}
func (s *Store) Queue(ctx context.Context, reviewID string) (Job, error) {
	if len(reviewID) != 32 {
		return Job{}, ErrInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Job{}, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return Job{}, e
	}
	job, e := s.queueTx(ctx, tx, reviewID)
	if e != nil {
		return Job{}, e
	}
	if e = tx.Commit(); e != nil {
		return Job{}, e
	}
	return job, nil
}
func (s *Store) queueTx(ctx context.Context, tx *sql.Tx, reviewID string) (Job, error) {
	var v Video
	var t Target
	var snap, quality string
	var expiry, consumed int64
	e := tx.QueryRowContext(ctx, `SELECT r.video_id,r.video_title,r.channel,r.target_kind,COALESCE(r.collection_id,0),COALESCE(r.media_id,0),COALESCE(r.episode_id,0),r.target_snapshot,r.expires_at,r.consumed_at,COALESCE(o.quality,'best') FROM youtube_reviews r LEFT JOIN youtube_subtitle_options o ON o.review_id=r.id WHERE r.id=?`, reviewID).Scan(&v.ID, &v.Title, &v.Channel, &t.Kind, &t.CollectionID, &t.MediaID, &t.EpisodeID, &snap, &expiry, &consumed, &quality)
	if e != nil || expiry < time.Now().Unix() || consumed != 0 {
		return Job{}, ErrChanged
	}
	current, label, e := targetSnapshot(ctx, tx, t)
	if e != nil {
		return Job{}, e
	}
	if current != snap {
		return Job{}, ErrChanged
	}
	key, e := id()
	if e != nil {
		return Job{}, e
	}
	now := time.Now().Unix()
	_, e = tx.ExecContext(ctx, `INSERT INTO youtube_jobs(id,review_id,video_id,video_title,channel,target_kind,collection_id,media_id,episode_id,target_snapshot,target_label,quality,state,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,'queued',?,?)`, key, reviewID, v.ID, v.Title, v.Channel, t.Kind, nullable(t.CollectionID), nullable(t.MediaID), nullable(t.EpisodeID), snap, label, quality, now, now)
	if e != nil {
		return Job{}, ErrBusy
	}
	fileID, _, e := replacementTarget(ctx, tx, t)
	if e != nil {
		return Job{}, e
	}
	if fileID > 0 {
		ids := []int64{}
		if t.Kind == "episode" {
			ids = append(ids, t.EpisodeID)
		}
		if e = organization.ReserveAcquisition(ctx, tx, "youtube", key, fileID, t.MediaID, ids); e != nil {
			return Job{}, e
		}
	}
	res, e := tx.ExecContext(ctx, `UPDATE youtube_reviews SET consumed_at=? WHERE id=? AND consumed_at=0 AND expires_at>=?`, now, reviewID, now)
	if e != nil {
		return Job{}, e
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return Job{}, ErrChanged
	}
	return Job{ID: key, Video: v, Target: t, TargetLabel: label, Quality: quality, State: "queued", CreatedAt: now, UpdatedAt: now}, nil
}

// QueueBulk applies the complete reviewed selection or none of it. Every item
// uses the existing target snapshot, collision and acquisition reservations.
func (s *Store) QueueBulk(ctx context.Context, reviewIDs []string) ([]Job, error) {
	if len(reviewIDs) < 1 || len(reviewIDs) > 25 {
		return nil, ErrInput
	}
	seen := map[string]bool{}
	for _, v := range reviewIDs {
		if len(v) != 32 || seen[v] {
			return nil, ErrInput
		}
		seen[v] = true
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return nil, e
	}
	jobs := []Job{}
	for _, v := range reviewIDs {
		job, e := s.queueTx(ctx, tx, v)
		if e != nil {
			return nil, e
		}
		jobs = append(jobs, job)
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return jobs, nil
}
func scanJob(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	e := row.Scan(&j.ID, &j.Video.ID, &j.Video.Title, &j.Video.Channel, &j.Target.Kind, &j.Target.CollectionID, &j.Target.MediaID, &j.Target.EpisodeID, &j.TargetLabel, &j.State, &j.Message, &j.Quality, &j.Progress, &j.Downloaded, &j.Total, &j.Speed, &j.ETA, &j.AutoImport, &j.SeriesSourceID, &j.CreatedAt, &j.UpdatedAt)
	return j, e
}

const jobFields = `id,video_id,video_title,channel,target_kind,COALESCE(collection_id,0),COALESCE(media_id,0),COALESCE(episode_id,0),target_label,state,message,quality,progress,downloaded_bytes,total_bytes,speed_bps,eta_seconds,auto_import,series_source_id,created_at,updated_at`

func (s *Store) Job(ctx context.Context, id string) (Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobFields+` FROM youtube_jobs WHERE id=?`, id))
}
func (s *Store) Jobs(ctx context.Context) ([]Job, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT `+jobFields+` FROM youtube_jobs ORDER BY created_at DESC,id DESC LIMIT 200`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Job{}
	for rows.Next() {
		j, e := scanJob(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Cancel releases a target only before publication starts. Partial downloaded
// bytes remain in their isolated attempt folder; importing jobs need recovery.
func (s *Store) Cancel(ctx context.Context, jobID string) error {
	var bound int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements WHERE source_kind='youtube' AND job_id=? AND plan_id<>''`, jobID).Scan(&bound); err != nil {
		return err
	}
	if bound > 0 {
		return ErrBusy
	}
	if err := organization.EnsureMediaAvailable(ctx, s.db); err != nil {
		return err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE youtube_jobs SET state='cancelled',message='Cancelled. Any isolated downloaded files were retained.',lease_owner='',lease_until=0,updated_at=unixepoch() WHERE id=? AND state IN ('queued','downloading','awaiting_review','failed')`, jobID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrChanged
	}
	return nil
}
func (s *Store) Files(ctx context.Context) ([]File, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT y.job_id,y.title,c.id,c.title,y.relative_path,y.size_bytes FROM youtube_files y JOIN youtube_collections c ON c.id=y.collection_id ORDER BY c.title COLLATE NOCASE,y.title COLLATE NOCASE,y.job_id LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		var f File
		if err = rows.Scan(&f.JobID, &f.Title, &f.CollectionID, &f.Collection, &f.Path, &f.Size); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
