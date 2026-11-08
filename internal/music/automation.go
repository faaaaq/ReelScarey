package music

import (
	"context"
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
}

const maxDueAlbums = 500

// ScheduleRunner performs the search/queue work for one due-album pass. It is
// injected by the composition root so music never imports transfer.
type ScheduleRunner func(context.Context, MusicSchedule, MusicPreferences, []WantedAlbum) error

func (s *Service) SetScheduleRunner(runner ScheduleRunner) { s.scheduleRunner = runner }

// RunSchedule performs one bounded wanted-album pass and records the outcome.
func (s *Service) RunSchedule(ctx context.Context) error {
	schedule, err := s.GetSchedule(ctx)
	if err != nil {
		return err
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
	albums, err := s.DueWantedAlbums(ctx, 25)
	if err != nil {
		return err
	}
	runErr := s.scheduleRunner(ctx, schedule, prefs, albums)
	now := time.Now().Unix()
	message := ""
	if runErr != nil {
		message = runErr.Error()
	}
	_, _ = s.store.db.ExecContext(context.WithoutCancel(ctx), `UPDATE music_schedule SET last_run=?,next_run=?,last_error=?,updated_at=unixepoch() WHERE id=1`,
		now, now+int64(schedule.IntervalHours)*3600, message)
	return runErr
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
			schedule, err := s.GetSchedule(ctx)
			if err != nil || !schedule.Enabled || schedule.NextRun > time.Now().Unix() {
				continue
			}
			_ = s.RunSchedule(ctx)
		}
	}
}

// GetSchedule reads the single persisted schedule row. A missing row is an
// error because migration 063 seeds it.
func (s *Service) GetSchedule(ctx context.Context) (MusicSchedule, error) {
	var schedule MusicSchedule
	err := s.store.db.QueryRowContext(ctx, `SELECT enabled, interval_hours, mode, next_run, last_run, last_error, revision FROM music_schedule WHERE id=1`).
		Scan(&schedule.Enabled, &schedule.IntervalHours, &schedule.Mode, &schedule.NextRun, &schedule.LastRun, &schedule.LastError, &schedule.Revision)
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
	if limit < 1 {
		return nil, ErrInvalidInput
	}
	if limit > maxDueAlbums {
		limit = maxDueAlbums
	}
	rows, err := s.store.db.QueryContext(ctx, wantedAlbumQuery+` LIMIT ?`, limit)
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
