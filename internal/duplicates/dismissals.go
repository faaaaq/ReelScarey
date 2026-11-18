package duplicates

import (
	"context"
	"database/sql"
	"fmt"
)

// Dismissal identity of one review group: a movie identity or an episode
// identity. Episode groups use the episode's row id; movie groups store 0.
type Key struct {
	Kind      string `json:"kind"`
	MediaID   int64  `json:"media_id"`
	EpisodeID int64  `json:"episode_id,omitempty"`
}

func normalizeKey(kind string, mediaID, episodeID int64) (Key, error) {
	if kind != "movie" && kind != "episode" {
		return Key{}, fmt.Errorf("duplicates: unknown group kind %q", kind)
	}
	if mediaID < 1 {
		return Key{}, fmt.Errorf("duplicates: missing media id")
	}
	if kind == "episode" {
		if episodeID < 1 {
			return Key{}, fmt.Errorf("duplicates: missing episode id")
		}
	} else {
		episodeID = 0
	}
	return Key{Kind: kind, MediaID: mediaID, EpisodeID: episodeID}, nil
}

// Dismiss records an owner decision to stop flagging one review group. The
// group stays dismissed until Restore removes the decision; a registration
// change that merges or splits files does not resurrect a dismissed group
// because the identity, not the file list, is remembered.
func Dismiss(ctx context.Context, db *sql.DB, key Key) error {
	normalized, err := normalizeKey(key.Kind, key.MediaID, key.EpisodeID)
	if err != nil {
		return err
	}
	if db == nil {
		return fmt.Errorf("duplicates: nil database")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO duplicate_dismissals(kind,media_id,episode_id,dismissed_at) VALUES(?,?,?,unixepoch())
		ON CONFLICT(kind,media_id,episode_id) DO UPDATE SET dismissed_at=excluded.dismissed_at`,
		normalized.Kind, normalized.MediaID, normalized.EpisodeID)
	return err
}

// Restore removes a dismissal so the group is reviewed again.
func Restore(ctx context.Context, db *sql.DB, key Key) error {
	normalized, err := normalizeKey(key.Kind, key.MediaID, key.EpisodeID)
	if err != nil {
		return err
	}
	if db == nil {
		return fmt.Errorf("duplicates: nil database")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM duplicate_dismissals WHERE kind=? AND media_id=? AND episode_id=?`,
		normalized.Kind, normalized.MediaID, normalized.EpisodeID)
	return err
}

// dismissedGroups loads every remembered dismissal into a lookup keyed by the
// same identity the review groups on.
func dismissedGroups(ctx context.Context, tx *sql.Tx) (map[Key]bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT kind,media_id,episode_id FROM duplicate_dismissals`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[Key]bool{}
	for rows.Next() {
		var key Key
		if err = rows.Scan(&key.Kind, &key.MediaID, &key.EpisodeID); err != nil {
			return nil, err
		}
		out[key] = true
	}
	return out, rows.Err()
}

// groupKey is the lookup identity for one loaded review group.
func groupKey(g Group) Key {
	key := Key{Kind: g.Kind, MediaID: g.MediaID}
	if g.EpisodeID != nil {
		key.EpisodeID = *g.EpisodeID
	}
	return key
}
