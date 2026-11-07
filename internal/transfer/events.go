package transfer

import (
	"context"
	"database/sql"
)

// recordDownloadCompleted adds the History entry for a finished import. The
// NOT EXISTS guard makes retries and restart reconciliation idempotent, so a
// repeated tick never duplicates the event.
func recordDownloadCompleted(ctx context.Context, tx *sql.Tx, id, title string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
SELECT unixepoch(),'download',?,?,'completed'
WHERE NOT EXISTS (SELECT 1 FROM activity_events WHERE kind='download' AND object_id=? AND state='completed')`, id, title, id)
	return err
}
