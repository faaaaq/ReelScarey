package music

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// MusicSchedule is the persisted wanted-album search schedule. The revision
// fences a reviewed change against a concurrent editor.
type MusicSchedule struct {
	Enabled       bool   `json:"enabled"`
	IntervalHours int    `json:"interval_hours"`
	Mode          string `json:"mode"`
	NextRun       int64  `json:"next_run"`
	LastRun       int64  `json:"last_run"`
	LastError     string `json:"last_error"`
	Revision      int    `json:"revision"`
	cursorAlbumID int64
}

const maxDueAlbums = 500

// ScheduleRunner performs the search/queue work for one due-album pass. It is
// injected by the composition root so music never imports transfer.
type ScheduleRunner func(context.Context, MusicSchedule, MusicPreferences, []WantedAlbum) error

func (s *Service) SetScheduleRunner(runner ScheduleRunner) { s.scheduleRunner = runner }

// RunSchedule performs one bounded wanted-album pass and records the outcome.
func (s *Service) RunSchedule(ctx context.Context) error {
	return s.runSchedule(ctx, false)
}

func (s *Service) runSchedule(ctx context.Context, dueOnly bool) error {
	if !s.scheduleMu.TryLock() {
		return &Error{Code: "music_schedule_busy", Message: "A music search is already running."}
	}
	defer s.scheduleMu.Unlock()
	schedule, err := s.GetSchedule(ctx)
	if err != nil {
		return err
	}
	if dueOnly && (!schedule.Enabled || schedule.NextRun > time.Now().Unix()) {
		return nil
	}
	if !schedule.Enabled {
		return &Error{Code: "music_schedule_disabled", Message: "Enable the music schedule before running it."}
	}
	if s.scheduleRunner == nil {
		return &Error{Code: "music_schedule_unavailable", Message: "The music schedule runner is not configured."}
	}
	prefs, err := s.GetPreferences(ctx)
	if err != nil {
		return err
	}
	albums, err := s.dueWantedAlbums(ctx, 25, schedule.cursorAlbumID)
	if err != nil {
		return err
	}
	runErr := s.scheduleRunner(ctx, schedule, prefs, albums)
	now := time.Now().Unix()
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	cursor := schedule.cursorAlbumID
	if len(albums) != 0 {
		cursor = albums[len(albums)-1].ID
	}
	finish, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_, err = s.store.db.ExecContext(finish, `UPDATE music_schedule SET last_run=?,next_run=?,last_error=?,cursor_album_id=?,updated_at=unixepoch() WHERE id=1 AND revision=?`,
		now, now+int64(schedule.IntervalHours)*3600, message, cursor, schedule.Revision)
	return errors.Join(runErr, err)
}

// RunScheduleLoop checks the persisted schedule once a minute. It is started by
// the composition root alongside the other durable workers.
func (s *Service) RunScheduleLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = s.runSchedule(ctx, true)
		}
	}
}

// GetSchedule reads the single persisted schedule row. A missing row is an
// error because migration 063 seeds it.
func (s *Service) GetSchedule(ctx context.Context) (MusicSchedule, error) {
	var schedule MusicSchedule
	err := s.store.db.QueryRowContext(ctx, `SELECT enabled, interval_hours, mode, next_run, last_run, last_error, revision,cursor_album_id FROM music_schedule WHERE id=1`).
		Scan(&schedule.Enabled, &schedule.IntervalHours, &schedule.Mode, &schedule.NextRun, &schedule.LastRun, &schedule.LastError, &schedule.Revision, &schedule.cursorAlbumID)
	if err != nil {
		return MusicSchedule{}, err
	}
	return schedule, nil
}

// SaveSchedule validates a reviewed schedule, compares its revision inside a
// write transaction and increments the stored revision. Enabling schedules the
// next run one interval from now; disabling clears it.
func (s *Service) SaveSchedule(ctx context.Context, in MusicSchedule) (MusicSchedule, error) {
	if in.IntervalHours < 1 || in.IntervalHours > 168 {
		return MusicSchedule{}, ErrInvalidInput
	}
	mode := strings.ToLower(strings.TrimSpace(in.Mode))
	if mode != "search" && mode != "download" {
		return MusicSchedule{}, ErrInvalidInput
	}
	tx, err := s.store.db.BeginTx(ctx, nil)
	if err != nil {
		return MusicSchedule{}, err
	}
	defer tx.Rollback()
	var current int
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM music_schedule WHERE id=1`).Scan(&current); err != nil {
		return MusicSchedule{}, err
	}
	if current != in.Revision {
		return MusicSchedule{}, ErrRevisionConflict
	}
	result, err := tx.ExecContext(ctx, `UPDATE music_schedule
		SET enabled=?, interval_hours=?, mode=?,
			next_run=CASE WHEN ?=1 THEN unixepoch()+?*3600 ELSE 0 END,
			revision=revision+1, updated_at=unixepoch()
		WHERE id=1 AND revision=?`,
		boolInt(in.Enabled), in.IntervalHours, mode, boolInt(in.Enabled), in.IntervalHours, in.Revision)
	if err != nil {
		return MusicSchedule{}, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return MusicSchedule{}, err
	} else if affected != 1 {
		return MusicSchedule{}, ErrRevisionConflict
	}
	if err := tx.Commit(); err != nil {
		return MusicSchedule{}, err
	}
	return s.GetSchedule(ctx)
}

// DueWantedAlbums returns a bounded set of monitored albums that still have no
// registered files, in the same deterministic order as the Wanted page.
func (s *Service) DueWantedAlbums(ctx context.Context, limit int) ([]WantedAlbum, error) {
	return s.dueWantedAlbums(ctx, limit, -1)
}

func (s *Service) dueWantedAlbums(ctx context.Context, limit int, after int64) ([]WantedAlbum, error) {
	if limit < 1 {
		return nil, ErrInvalidInput
	}
	if limit > maxDueAlbums {
		limit = maxDueAlbums
	}
	query := wantedAlbumSelection + ` AND NOT EXISTS(SELECT 1 FROM music_downloads d WHERE d.album_id=a.id AND d.state NOT IN ('completed','cancelled'))`
	order := ` ORDER BY ar.name COLLATE NOCASE,a.title COLLATE NOCASE,a.id LIMIT ?`
	args := []any{limit}
	if after >= 0 {
		order = ` ORDER BY a.id<=?,a.id LIMIT ?`
		args = []any{after, limit}
	}
	rows, err := s.store.db.QueryContext(ctx, query+order, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WantedAlbum{}
	for rows.Next() {
		album, err := scanWantedAlbum(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, album)
	}
	return out, rows.Err()
}

// CheckScheduledDownload runs inside the same transaction as the submission
// reservation. A search result obtained before a setting change cannot queue.
func CheckScheduledDownload(ctx context.Context, tx *sql.Tx, albumID int64, schedule MusicSchedule, prefs AlbumPreferences) error {
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM music_schedule s,music_preferences p,music_albums a
		JOIN music_artists ar ON ar.id=a.artist_id
		WHERE s.id=1 AND s.enabled=1 AND s.mode='download' AND s.revision=?
		AND p.id=1 AND p.revision=? AND a.id=? AND a.revision=? AND a.monitored=1 AND ar.monitored=1
		AND NOT EXISTS(SELECT 1 FROM music_files f WHERE f.album_id=a.id)
		AND NOT EXISTS(SELECT 1 FROM music_downloads d WHERE d.album_id=a.id AND d.state NOT IN ('completed','cancelled'))
	)`, schedule.Revision, prefs.Preferences.Revision, albumID, prefs.Revision).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrRevisionConflict
	}
	return nil
}
