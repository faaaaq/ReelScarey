package organization

import (
	"context"
	"database/sql"
	"errors"
)

func (s *Service) queueRegisteredTitle(ctx context.Context) error {
	if s.tag == nil || s.probe == nil {
		return nil
	}
	if err := EnsureMediaAvailable(ctx, s.db); err != nil {
		return nil
	}
	var fileID int64
	err := s.db.QueryRowContext(ctx, `SELECT media_file_id FROM video_title_updates WHERE next_attempt<=unixepoch() ORDER BY next_attempt,media_file_id LIMIT 1`).Scan(&fileID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	_, err = s.previewEmbeddedTitle(ctx, fileID, "", true)
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var typed *Error
	if errors.As(err, &typed) && (typed.Code == "invalid_selection" || typed.Code == "source_changed") {
		tx, dbErr := s.db.BeginTx(ctx, nil)
		if dbErr != nil {
			return dbErr
		}
		defer tx.Rollback()
		if _, dbErr = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) SELECT unixepoch(),'title',m.id,m.title,? FROM media_files f JOIN media_items m ON m.id=COALESCE(f.movie_id,f.series_id) WHERE f.id=?`, typed.Message, fileID); dbErr != nil {
			return dbErr
		}
		if _, dbErr = tx.ExecContext(ctx, `DELETE FROM video_title_updates WHERE media_file_id=?`, fileID); dbErr != nil {
			return dbErr
		}
		return tx.Commit()
	}
	_, err = s.db.ExecContext(ctx, `UPDATE video_title_updates SET next_attempt=unixepoch()+300 WHERE media_file_id=?`, fileID)
	return err
}
