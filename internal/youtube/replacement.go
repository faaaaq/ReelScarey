package youtube

import (
	"context"
	"database/sql"
)

func replacementTarget(ctx context.Context, q queryer, t Target) (int64, string, error) {
	if t.Kind == "custom" {
		return 0, "", nil
	}
	var id int64
	var sha string
	var err error
	if t.Kind == "movie" {
		err = q.QueryRowContext(ctx, `SELECT id,sha256 FROM media_files WHERE movie_id=?`, t.MediaID).Scan(&id, &sha)
	} else {
		err = q.QueryRowContext(ctx, `SELECT f.id,f.sha256 FROM media_files f JOIN episode_files e ON e.media_file_id=f.id WHERE e.episode_id=?`, t.EpisodeID).Scan(&id, &sha)
	}
	if err == sql.ErrNoRows {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	if t.Kind == "episode" {
		var n int
		if err = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM episode_files WHERE media_file_id=?`, id).Scan(&n); err != nil {
			return 0, "", err
		}
		if n != 1 {
			return 0, "", ErrBusy
		}
	}
	return id, sha, nil
}
