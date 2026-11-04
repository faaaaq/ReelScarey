package organization

import (
	"context"
	"fmt"
)

// Include a stable per-file suffix whenever multiple movie files coexist. It
// remains part of the review snapshot, so a changed version set invalidates it.
func (s *Service) captureFile(ctx context.Context, q query, mediaID, fileID int64, target string) (Snapshot, error) {
	v, err := s.capture(ctx, q, mediaID, target)
	if err != nil || v.Content.Kind != "movie" {
		return v, err
	}
	var count, resolution int
	var managed string
	err = q.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM media_files WHERE movie_id=?),f.video_resolution,COALESCE(d.movie_version,'') FROM media_files f LEFT JOIN downloads d ON d.id=f.download_id WHERE f.id=? AND f.movie_id=?`, mediaID, fileID, mediaID).Scan(&count, &resolution, &managed)
	if err != nil {
		return v, err
	}
	if count > 1 || managed != "" {
		v.MovieVersion = fmt.Sprintf("Version %d", fileID)
		if resolution > 0 {
			v.MovieVersion = fmt.Sprintf("%dp - Version %d", resolution, fileID)
		}
	}
	return v, nil
}
