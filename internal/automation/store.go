// Package automation owns bounded, durable library searches. It delegates every
// download and import to transfer; it never accesses media paths or credentials.
package automation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/transfer"
)

type Workflow interface {
	Status(context.Context, string) (transfer.IntegrationStatus, error)
	Search(context.Context, int64, string) ([]transfer.Release, error)
	SearchTV(context.Context, int64, int, int, string) ([]transfer.Release, error)
	SearchTVUpgrade(context.Context, int64, int, int) ([]transfer.Release, error)
	SearchAnimeAutomatic(context.Context, int64, int, int, bool) ([]transfer.Release, error)
	QueueAutomatic(context.Context, string, transfer.QueueGuard) (transfer.Job, bool, error)
	QueueAutomaticAfter(context.Context, string, time.Duration, transfer.QueueGuard) (transfer.Job, bool, error)
	QueueAutomaticVersion(context.Context, string, string, transfer.QueueGuard) (transfer.Job, bool, error)
	// Feed checking (Sonarr/Radarr-style) uses the provider's newest releases
	// and queues the exact feed entry through the same reservations and importer.
	Recent(context.Context) ([]transfer.RecentFeed, error)
	QueueCandidate(context.Context, string, int64, *int64, string, transfer.QueueGuard) (transfer.Job, bool, error)
	QueueCandidateAfter(context.Context, string, int64, *int64, string, time.Duration, transfer.QueueGuard) (transfer.Job, bool, error)
	// QueueAnnounceAfter queues one exact captured IPTorrents announce through
	// the ordinary reservation and delayed-submission workflow. It revalidates
	// the captured source revision and re-fetches the torrent from the tracker's
	// fixed origin; automation never carries a download URL or credential.
	QueueAnnounceAfter(context.Context, int64, string, int, int64, *int64, string, time.Duration, transfer.QueueGuard) (transfer.Job, bool, error)
}
type Service struct {
	db       *sql.DB
	workflow Workflow
	now      func() time.Time
}

func New(db *sql.DB, w Workflow) *Service { return &Service{db: db, workflow: w, now: time.Now} }

type Error struct{ Code, Message string }

func (e *Error) Error() string     { return e.Message }
func invalid(message string) error { return &Error{"invalid_automation", message} }

var ErrLease = errors.New("search lease was lost or its settings changed")

// problemInvalidScope rejects an unsupported bulk-search scope.
var problemInvalidScope = &Error{"invalid_automation", "Choose the movie or TV episode scope."}

type Schedule struct {
	Enabled          bool   `json:"enabled"`
	Mode             string `json:"mode"`
	Interval         int    `json:"interval_minutes"`
	Batch            int    `json:"batch_size"`
	DefaultProfile   int64  `json:"default_profile_id"`
	Revision         int    `json:"revision"`
	NextRun          int64  `json:"next_run"`
	Requested        bool   `json:"requested"`
	FocusMedia       string `json:"focus_media"`
	FocusEpisode     string `json:"focus_episode"`
	FocusKind        string `json:"focus_kind"`
	State            string `json:"state"`
	Message          string `json:"message"`
	IndexerConnected bool   `json:"indexer_connected"`
	ClientConnected  bool   `json:"client_connected"`
}

const scheduleColumns = `enabled,mode,interval_minutes,batch_size,default_profile_id,revision,next_run,requested,focus_media,focus_episode,focus_kind,state,message`

func scanSchedule(row interface{ Scan(...any) error }) (Schedule, error) {
	var v Schedule
	e := row.Scan(&v.Enabled, &v.Mode, &v.Interval, &v.Batch, &v.DefaultProfile, &v.Revision, &v.NextRun, &v.Requested, &v.FocusMedia, &v.FocusEpisode, &v.FocusKind, &v.State, &v.Message)
	return v, e
}
func (s *Service) Get(ctx context.Context) (Schedule, error) {
	v, e := scanSchedule(s.db.QueryRowContext(ctx, `SELECT `+scheduleColumns+` FROM search_schedule WHERE id=1`))
	if e != nil {
		return v, e
	}
	if s.workflow != nil {
		a, e := s.workflow.Status(ctx, "indexers")
		if e != nil {
			return v, e
		}
		b, e := s.workflow.Status(ctx, "clients")
		if e != nil {
			return v, e
		}
		v.IndexerConnected = a.Configured
		v.ClientConnected = b.Configured
	}
	if (v.Enabled || v.Requested) && !v.IndexerConnected {
		v.State = "waiting_indexer"
		v.Message = "Waiting for an enabled indexer. Add a direct tracker, RSS feed, Torznab or Newznab connection in Indexers."
	}
	return v, nil
}

type Update struct {
	Enabled        bool   `json:"enabled"`
	Mode           string `json:"mode"`
	Interval       int    `json:"interval_minutes"`
	Batch          int    `json:"batch_size"`
	DefaultProfile int64  `json:"default_profile_id"`
	Revision       int    `json:"revision"`
}

func (s *Service) Update(ctx context.Context, in Update) (Schedule, error) {
	if (in.Mode != "search" && in.Mode != "download") || in.Interval < 15 || in.Interval > 10080 || in.Batch < 1 || in.Batch > 25 {
		return Schedule{}, invalid("Choose search or download mode, an interval of 15–10080 minutes and 1–25 targets per run.")
	}
	if _, e := quality.New(s.db).Get(ctx, in.DefaultProfile); errors.Is(e, sql.ErrNoRows) {
		return Schedule{}, invalid("Choose an existing default quality profile.")
	} else if e != nil {
		return Schedule{}, e
	}
	state, message := "paused", "Scheduled searches are paused. Existing downloads continue."
	next := int64(0)
	if in.Enabled {
		state = "scheduled"
		message = "Waiting for the next search window."
		next = s.now().Unix()
	}
	r, e := s.db.ExecContext(ctx, `UPDATE search_schedule SET enabled=?,mode=?,interval_minutes=?,batch_size=?,default_profile_id=?,revision=revision+1,next_run=?,requested=0,attempts=0,state=?,message=? WHERE id=1 AND revision=?`, in.Enabled, in.Mode, in.Interval, in.Batch, in.DefaultProfile, next, state, message, in.Revision)
	if e != nil {
		return Schedule{}, e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return Schedule{}, &Error{"revision_conflict", "Automation settings changed. Refresh before saving."}
	}
	return s.Get(ctx)
}
func (s *Service) Request(ctx context.Context) error {
	r, e := s.db.ExecContext(ctx, `UPDATE search_schedule SET requested=1,next_run=0,attempts=0,state='scheduled',message='A search was requested.' WHERE id=1 AND requested=0 AND lease_until<?`, s.now().Unix())
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return &Error{"automation_busy", "A search is already requested or running."}
	}
	return nil
}

// RequestItem scopes the next search run to one show, movie or episode after
// an explicit owner action on a library page. The run follows the saved mode
// exactly like any other scheduled run: download mode queues the best
// matching release, search-only mode records candidates for review.
func (s *Service) RequestItem(ctx context.Context, mediaID int64, episodeID *int64) error {
	var episodeCount int
	e := s.db.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE s.series_id=m.id AND e.id=?) FROM media_items m WHERE m.id=?`, episodeID, mediaID).Scan(&episodeCount)
	if errors.Is(e, sql.ErrNoRows) {
		return &Error{"not_found", "This title is not in the saved library."}
	}
	if e != nil {
		return e
	}
	if episodeID != nil && (*episodeID == 0 || episodeCount != 1) {
		return &Error{"not_found", "This episode does not belong to the saved show."}
	}
	focusMedia := strconv.FormatInt(mediaID, 10)
	focusEpisode := ""
	if episodeID != nil {
		focusEpisode = strconv.FormatInt(*episodeID, 10)
	}
	// A running search is not disturbed: the finish logic preserves requests
	// made while it was active and starts the focused run immediately after.
	// The cursor restarts so the focused pass visits every matching target.
	if _, e = s.db.ExecContext(ctx, `UPDATE search_schedule SET requested=1,focus_media=?,focus_episode=?,focus_kind='',cursor='',next_run=0 WHERE id=1`, focusMedia, focusEpisode); e != nil {
		return e
	}
	return nil
}

// RequestBulk scopes the next search run to every missing monitored movie or
// TV episode. It backs the Calendar's one-click searches; the run follows the
// saved mode exactly like any other scheduled run.
func (s *Service) RequestBulk(ctx context.Context, kind string) error {
	if kind != "movie" && kind != "series" {
		return problemInvalidScope
	}
	if _, e := s.db.ExecContext(ctx, `UPDATE search_schedule SET requested=1,focus_media='',focus_episode='',focus_kind=?,cursor='',next_run=0 WHERE id=1`, kind); e != nil {
		return e
	}
	return nil
}

type Assignment struct {
	ID        int64  `json:"media_id"`
	Title     string `json:"title"`
	Kind      string `json:"kind"`
	Monitored bool   `json:"monitored"`
	ProfileID *int64 `json:"profile_id"`
	Revision  int    `json:"revision"`
}
type AssignmentPage struct {
	Items []Assignment `json:"items"`
	Page  int          `json:"page"`
	Total int          `json:"total"`
	Limit int          `json:"limit"`
}

func (s *Service) Assignments(ctx context.Context, page int) (AssignmentPage, error) {
	out := AssignmentPage{Items: []Assignment{}, Page: page, Limit: 24}
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_items`).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := s.db.QueryContext(ctx, `SELECT m.id,m.title,m.kind,m.monitored,a.profile_id,COALESCE(a.revision,0) FROM media_items m LEFT JOIN item_automation a ON a.media_id=m.id ORDER BY `+library.TitleSort+` COLLATE NOCASE,m.id LIMIT 24 OFFSET ?`, (page-1)*24)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var a Assignment
		if e = rows.Scan(&a.ID, &a.Title, &a.Kind, &a.Monitored, &a.ProfileID, &a.Revision); e != nil {
			return out, e
		}
		out.Items = append(out.Items, a)
	}
	return out, rows.Err()
}

type AssignmentUpdate struct {
	ProfileID *int64 `json:"profile_id"`
	Revision  int    `json:"revision"`
	Monitored *bool  `json:"monitored,omitempty"`
}

func (s *Service) Assign(ctx context.Context, id int64, in AssignmentUpdate) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var kind string
	var revision int
	if e = tx.QueryRowContext(ctx, `SELECT m.kind,COALESCE(a.revision,0) FROM media_items m LEFT JOIN item_automation a ON a.media_id=m.id WHERE m.id=?`, id).Scan(&kind, &revision); e != nil {
		return e
	}
	if revision != in.Revision {
		return &Error{"revision_conflict", "This item's automation preferences changed. Refresh before saving."}
	}
	if in.ProfileID != nil {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM quality_profiles WHERE id=?`, *in.ProfileID).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return invalid("Choose an existing quality profile or the schedule default.")
		}
	}
	if in.Monitored != nil {
		if kind != "movie" {
			return invalid("Change TV monitoring from the show's season and episode controls.")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE media_items SET monitored=? WHERE id=?`, *in.Monitored, id); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO item_automation(media_id,profile_id,revision) VALUES(?,?,1) ON CONFLICT(media_id) DO UPDATE SET profile_id=excluded.profile_id,revision=item_automation.revision+1`, id, in.ProfileID); e != nil {
		return e
	}
	return tx.Commit()
}

type Run struct {
	ID       string `json:"id"`
	Started  int64  `json:"started_at"`
	Finished int64  `json:"finished_at"`
	State    string `json:"state"`
	Message  string `json:"message"`
	Mode     string `json:"mode"`
	Checked  int    `json:"checked"`
	Queued   int    `json:"queued"`
	// Matched counts targets where the run found an eligible release, whether
	// it queued a download or recorded the candidate for review.
	Matched int `json:"matched"`
}
type Result struct {
	Kind       string             `json:"kind"`
	Season     int                `json:"season"`
	Episode    int                `json:"episode"`
	ID         int64              `json:"id"`
	MediaID    int64              `json:"media_id"`
	EpisodeID  *int64             `json:"episode_id"`
	Target     string             `json:"target"`
	Outcome    string             `json:"outcome"`
	Message    string             `json:"message"`
	Decisions  []quality.Decision `json:"decisions"`
	DownloadID *string            `json:"download_id"`
}

func (s *Service) Runs(ctx context.Context) ([]Run, error) {
	out := []Run{}
	rows, e := s.db.QueryContext(ctx, `SELECT r.id,r.started_at,r.finished_at,r.state,r.message,r.mode,r.checked,r.queued,
		(SELECT COUNT(*) FROM automation_results ar WHERE ar.run_id=r.id AND ar.outcome IN ('match','upgrade_available','queued'))
		FROM automation_runs r ORDER BY r.started_at DESC,r.rowid DESC LIMIT 30`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v Run
		if e = rows.Scan(&v.ID, &v.Started, &v.Finished, &v.State, &v.Message, &v.Mode, &v.Checked, &v.Queued, &v.Matched); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func targetName(t searchTarget) string {
	if t.TV {
		if t.Anime && t.AnimeMode == "absolute" && t.AbsoluteNumber != nil {
			return fmt.Sprintf("%s · S%02dE%02d · absolute %d", t.Title, t.Season, t.Episode, *t.AbsoluteNumber)
		}
		return fmt.Sprintf("%s · S%02dE%02d", t.Title, t.Season, t.Episode)
	}
	if t.MovieVersion != "" {
		return t.Title + " · " + quality.MovieVersionName(t.MovieVersion)
	}
	return t.Title
}
