package library

import (
	"context"
	"database/sql"
	"errors"
)

var ErrMonitoringInput = errors.New("Choose 1–100 distinct saved titles of one media type and a monitoring preference.")
var ErrMonitoringConflict = errors.New("One or more selected titles changed. Review the selection again before applying. No titles were changed.")

type MonitoringItem struct {
	ID                 int64  `json:"id"`
	Title              string `json:"title"`
	Year               *int   `json:"year"`
	Monitored          bool   `json:"monitored"`
	SeriesRevision     int64  `json:"series_revision"`
	AutomationRevision int    `json:"automation_revision"`
}
type MonitoringExpectation struct {
	ID                 int64 `json:"id"`
	Monitored          *bool `json:"monitored"`
	SeriesRevision     int64 `json:"series_revision"`
	AutomationRevision int   `json:"automation_revision"`
}
type MonitoringResult struct {
	Total     int  `json:"total"`
	Changed   int  `json:"changed"`
	Unchanged int  `json:"unchanged"`
	Monitored bool `json:"monitored"`
}

func validMonitoringIDs(kind string, ids []int64) bool {
	if (kind != "movie" && kind != "series") || len(ids) < 1 || len(ids) > 100 {
		return false
	}
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id < 1 || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func monitoringItem(ctx context.Context, tx *sql.Tx, kind string, id int64) (MonitoringItem, error) {
	var out MonitoringItem
	e := tx.QueryRowContext(ctx, `SELECT m.id,m.title,m.year,m.monitored,COALESCE(d.revision,0),COALESCE(a.revision,0) FROM media_items m LEFT JOIN series_details d ON d.media_id=m.id LEFT JOIN item_automation a ON a.media_id=m.id WHERE m.id=? AND m.kind=?`, id, kind).Scan(&out.ID, &out.Title, &out.Year, &out.Monitored, &out.SeriesRevision, &out.AutomationRevision)
	return out, e
}

// Review reads only explicit catalog IDs, never a query or a filesystem root.
func (s *Store) ReviewMonitoring(ctx context.Context, kind string, ids []int64) ([]MonitoringItem, error) {
	if !validMonitoringIDs(kind, ids) {
		return nil, ErrMonitoringInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	out := make([]MonitoringItem, 0, len(ids))
	for _, id := range ids {
		item, e := monitoringItem(ctx, tx, kind, id)
		if e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, tx.Commit()
}

// ApplyMonitoring checks the complete review before changing anything. Existing
// TV and automation revisions fence every current monitoring writer, including
// a preference changed away and back. Profiles and child selections are retained.
func (s *Store) ApplyMonitoring(ctx context.Context, kind string, expected []MonitoringExpectation, monitored bool) (MonitoringResult, error) {
	ids := make([]int64, 0, len(expected))
	for _, v := range expected {
		if v.Monitored == nil || v.SeriesRevision < 0 || v.AutomationRevision < 0 {
			return MonitoringResult{}, ErrMonitoringInput
		}
		ids = append(ids, v.ID)
	}
	if !validMonitoringIDs(kind, ids) {
		return MonitoringResult{}, ErrMonitoringInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return MonitoringResult{}, e
	}
	defer tx.Rollback()
	current := make([]MonitoringItem, 0, len(expected))
	for _, v := range expected {
		item, e := monitoringItem(ctx, tx, kind, v.ID)
		if errors.Is(e, sql.ErrNoRows) {
			return MonitoringResult{}, ErrMonitoringConflict
		}
		if e != nil {
			return MonitoringResult{}, e
		}
		if item.Monitored != *v.Monitored || item.SeriesRevision != v.SeriesRevision || item.AutomationRevision != v.AutomationRevision {
			return MonitoringResult{}, ErrMonitoringConflict
		}
		current = append(current, item)
	}
	out := MonitoringResult{Total: len(expected), Monitored: monitored}
	for _, v := range current {
		if v.Monitored == monitored {
			out.Unchanged++
			continue
		}
		if kind == "series" {
			if e = bumpSeries(ctx, tx, v.ID, v.SeriesRevision); e != nil {
				return MonitoringResult{}, e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE media_items SET monitored=? WHERE id=?`, monitored, v.ID); e != nil {
			return MonitoringResult{}, e
		}
		// NULL retains the schedule default for a previously unassigned title. The
		// conflict branch changes only revision, preserving an explicit profile.
		if _, e = tx.ExecContext(ctx, `INSERT INTO item_automation(media_id,profile_id,revision) VALUES(?,NULL,1) ON CONFLICT(media_id) DO UPDATE SET revision=item_automation.revision+1`, v.ID); e != nil {
			return MonitoringResult{}, e
		}
		out.Changed++
	}
	return out, tx.Commit()
}
