package releaseguard

import (
	"context"
	"database/sql"
	"time"
)

// Stalled describes a download job whose byte progress has not advanced for at
// least the requested threshold. LastProgressAt and StalledAt are Unix seconds.
type Stalled struct {
	ID             string  `json:"id"`
	Protocol       string  `json:"protocol"`
	Title          string  `json:"title"`
	InfoHash       string  `json:"info_hash"`
	Indexer        string  `json:"indexer"`
	Progress       float64 `json:"progress"`
	LastProgressAt int64   `json:"last_progress_at"`
	StalledAt      int64   `json:"stalled_at"`
}

// DetectStalled reports downloading jobs whose last recorded progress sample is
// older than threshold. It is intentionally conservative:
//
//   - Only the 'downloading' state is considered. queued and submitting jobs
//     have not begun transfer, importing jobs are governed by transfer leases,
//     and blocked/failed/completed/cancelled jobs are terminal or explicitly
//     non-progressing. None of those are ever returned.
//   - Jobs at progress >= 1 are excluded while they finalize, even if the client
//     still labels them downloading.
//   - last_progress_at == 0 means no progress sample has been recorded since the
//     migration. Such jobs are skipped rather than guessed from created_at or
//     updated_at, because those timestamps change for reasons unrelated to
//     byte progress.
//   - A job is not reported again until new progress moves last_progress_at past
//     its previous stalled_at, so repeated detection does not duplicate alerts.
//
// A non-positive threshold is rejected; it would report healthy jobs.
func DetectStalled(ctx context.Context, db *sql.DB, now time.Time, threshold time.Duration) ([]Stalled, error) {
	if threshold <= 0 {
		return nil, invalid("stalled threshold must be positive")
	}
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.Add(-threshold).Unix()
	rows, err := db.QueryContext(ctx, `SELECT id, protocol, title, info_hash, indexer_name,
			progress, last_progress_at, stalled_at
		FROM downloads
		WHERE state = 'downloading'
			AND progress < 1
			AND last_progress_at > 0
			AND last_progress_at <= ?
			AND (stalled_at = 0 OR stalled_at < last_progress_at)
		ORDER BY last_progress_at, id`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Stalled, 0)
	for rows.Next() {
		var item Stalled
		if err := rows.Scan(&item.ID, &item.Protocol, &item.Title, &item.InfoHash, &item.Indexer,
			&item.Progress, &item.LastProgressAt, &item.StalledAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// MarkStalled records the detection time on a still-active downloading job. It
// never marks completed, cancelled, failed, blocked, queued, submitting or
// importing jobs, jobs at progress >= 1, or jobs without a progress sample. The
// boolean reports whether an eligible row was updated.
func MarkStalled(ctx context.Context, db *sql.DB, id string, now time.Time) (bool, error) {
	if now.IsZero() {
		now = time.Now()
	}
	result, err := db.ExecContext(ctx, `UPDATE downloads
		SET stalled_at = ?
		WHERE id = ?
			AND state = 'downloading'
			AND progress < 1
			AND last_progress_at > 0
			AND last_progress_at <= ?
			AND (stalled_at = 0 OR stalled_at < last_progress_at)`, now.Unix(), id, now.Unix())
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected > 0, nil
}
