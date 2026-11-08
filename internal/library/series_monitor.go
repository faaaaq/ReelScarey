package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// SeriesMonitorMode is the one-time monitoring choice made when a show is
// added. It is not a persisted policy; later edits use the normal monitoring
// controls.
type SeriesMonitorMode string

const (
	SeriesMonitorNone    SeriesMonitorMode = "none"
	SeriesMonitorMissing SeriesMonitorMode = "missing"
	SeriesMonitorCurrent SeriesMonitorMode = "current"
	SeriesMonitorFuture  SeriesMonitorMode = "future"
)

// ValidSeriesMonitorMode reports whether mode is one of the add-time choices.
func ValidSeriesMonitorMode(mode string) bool {
	switch SeriesMonitorMode(mode) {
	case SeriesMonitorNone, SeriesMonitorMissing, SeriesMonitorCurrent, SeriesMonitorFuture:
		return true
	}
	return false
}

// validDateSQL accepts only a complete civil date, matching the calendar and
// scheduler rule.
func validDateSQL(column string) string {
	return fmt.Sprintf(`length(%s)=10 AND substr(%s,1,4) BETWEEN '0001' AND '9999' AND date(%s,'+0 days')=%s`, column, column, column, column)
}

// currentSeasonID returns the latest regular season that has started, or the
// next regular season that has not started yet. A nil result means the show has
// no usable season date.
func currentSeasonID(ctx context.Context, tx *sql.Tx, seriesID int64) (*int64, error) {
	started := validDateSQL("air_date") + ` AND air_date<=date('now')`
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM seasons WHERE series_id=? AND number>0 AND `+started+` ORDER BY number DESC LIMIT 1`, seriesID).Scan(&id)
	if err == nil {
		return &id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	upcoming := validDateSQL("air_date") + ` AND air_date>date('now')`
	err = tx.QueryRowContext(ctx, `SELECT id FROM seasons WHERE series_id=? AND number>0 AND `+upcoming+` ORDER BY number ASC LIMIT 1`, seriesID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// SetSeriesMonitorMode applies the one-time monitoring choice after a show is
// added. It never changes the show's identity or files. Regular seasons are
// selected by their air date; specials stay unmonitored.
func (s *Store) SetSeriesMonitorMode(ctx context.Context, id int64, mode string) error {
	if !ValidSeriesMonitorMode(mode) {
		return ErrMonitoringInput
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM series_details WHERE media_id=?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists != 1 {
		return sql.ErrNoRows
	}

	monitored := 0
	if SeriesMonitorMode(mode) != SeriesMonitorNone {
		monitored = 1
	}
	if _, err = tx.ExecContext(ctx, `UPDATE media_items SET monitored=? WHERE id=? AND kind='series'`, monitored, id); err != nil {
		return err
	}
	// Reset regular seasons and episodes before applying the chosen scope.
	if _, err = tx.ExecContext(ctx, `UPDATE seasons SET monitored=0 WHERE series_id=?`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE episodes SET monitored=0 WHERE season_id IN (SELECT id FROM seasons WHERE series_id=?)`, id); err != nil {
		return err
	}
	if SeriesMonitorMode(mode) == SeriesMonitorNone {
		// The show is paused and every child stays off.
	} else {
		seasonDate := validDateSQL("air_date")
		episodeDate := validDateSQL("e.air_date")
		switch SeriesMonitorMode(mode) {
		case SeriesMonitorMissing:
			// Every regular season that has started, and its episodes that
			// have already aired.
			if _, err = tx.ExecContext(ctx, `UPDATE seasons SET monitored=1 WHERE series_id=? AND number>0 AND `+seasonDate+` AND air_date<=date('now')`, id); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE episodes AS e SET monitored=1 WHERE e.season_id IN (SELECT s.id FROM seasons s WHERE s.series_id=? AND s.monitored=1) AND `+episodeDate+` AND e.air_date<=date('now')`, id); err != nil {
				return err
			}
		case SeriesMonitorCurrent:
			current, err := currentSeasonID(ctx, tx, id)
			if err != nil {
				return err
			}
			if current != nil {
				if _, err = tx.ExecContext(ctx, `UPDATE seasons SET monitored=1 WHERE id=?`, *current); err != nil {
					return err
				}
				if _, err = tx.ExecContext(ctx, `UPDATE episodes SET monitored=1 WHERE season_id=?`, *current); err != nil {
					return err
				}
			}
		case SeriesMonitorFuture:
			// The current season and every later season, with episodes that
			// release today or later.
			current, err := currentSeasonID(ctx, tx, id)
			if err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, `UPDATE seasons SET monitored=1 WHERE series_id=? AND number>0 AND `+seasonDate+` AND air_date>=date('now')`, id); err != nil {
				return err
			}
			if current != nil {
				if _, err = tx.ExecContext(ctx, `UPDATE seasons SET monitored=1 WHERE id=?`, *current); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, `UPDATE episodes AS e SET monitored=1 WHERE e.season_id IN (SELECT s.id FROM seasons s WHERE s.series_id=? AND s.monitored=1) AND `+episodeDate+` AND e.air_date>=date('now')`, id); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE series_details SET updated_at=? WHERE media_id=?`, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}
