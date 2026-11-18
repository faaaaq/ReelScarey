package organization

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// TitleQueueStatus is the durable state of the automatic embedded-title queue:
// how many registered files still wait, whether the queue is paused, and which
// plan the worker is on right now.
type TitleQueueStatus struct {
	Paused   bool   `json:"paused"`
	Pending  int64  `json:"pending"`
	ActiveID string `json:"active_plan_id,omitempty"`
	Message  string `json:"message,omitempty"`
}

// titleUpdatesPaused reports the durable pause flag. A missing row (an older
// database) reads as running.
func (s *Service) titleUpdatesPaused(ctx context.Context) (bool, error) {
	var paused bool
	err := s.db.QueryRowContext(ctx, `SELECT paused FROM title_queue_state WHERE id=1`).Scan(&paused)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return paused, err
}

// TitleQueue reports the automatic title-update queue.
func (s *Service) TitleQueue(ctx context.Context) (TitleQueueStatus, error) {
	out := TitleQueueStatus{}
	var e error
	if out.Paused, e = s.titleUpdatesPaused(ctx); e != nil {
		return out, e
	}
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_title_updates`).Scan(&out.Pending); e != nil {
		return out, e
	}
	var id, message string
	e = s.db.QueryRowContext(ctx, `SELECT id,message FROM organization_plans WHERE state IN ('queued','running') AND json_extract(snapshot,'$.replacement.automatic_title')=1 ORDER BY id LIMIT 1`).Scan(&id, &message)
	switch {
	case errors.Is(e, sql.ErrNoRows):
	case e != nil:
		return out, e
	default:
		out.ActiveID = id
		out.Message = message
	}
	return out, nil
}

// SetTitleQueuePaused pauses or resumes the automatic title queue. The pending
// work stays queued; a paused queue simply stops claiming new files. A file
// already being rewritten finishes safely.
func (s *Service) SetTitleQueuePaused(ctx context.Context, paused bool) (TitleQueueStatus, error) {
	if _, e := s.db.ExecContext(ctx, `INSERT INTO title_queue_state(id,paused,updated_at) VALUES(1,?,unixepoch())
		ON CONFLICT(id) DO UPDATE SET paused=excluded.paused,updated_at=excluded.updated_at`, paused); e != nil {
		return TitleQueueStatus{}, e
	}
	state := "resumed"
	if paused {
		state = "paused by the owner; the file in progress finishes safely"
	}
	if _, e := s.db.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title','title-queue','Embedded title updates',?)`, state); e != nil {
		return TitleQueueStatus{}, e
	}
	return s.TitleQueue(ctx)
}

// DiscardPendingTitles stops the batch: every file still waiting is dropped
// from the queue and recorded once, so nothing is silently forgotten. A file
// already being rewritten finishes safely. No media is changed by discarding.
func (s *Service) DiscardPendingTitles(ctx context.Context) (TitleQueueStatus, error) {
	var pending int64
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM video_title_updates`).Scan(&pending); e != nil {
		return TitleQueueStatus{}, e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return TitleQueueStatus{}, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `DELETE FROM video_title_updates`); e != nil {
		return TitleQueueStatus{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title','title-queue','Embedded title updates',?)`, pluralizePending(pending)); e != nil {
		return TitleQueueStatus{}, e
	}
	if e = tx.Commit(); e != nil {
		return TitleQueueStatus{}, e
	}
	return s.TitleQueue(ctx)
}

func pluralizePending(n int64) string {
	switch n {
	case 0:
		return "no pending title updates to discard"
	case 1:
		return "1 pending title update discarded by the owner; no files were changed"
	default:
		return fmt.Sprintf("%d pending title updates discarded by the owner; no files were changed", n)
	}
}
