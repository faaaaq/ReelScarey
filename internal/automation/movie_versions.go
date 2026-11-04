package automation

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

type MovieVersions struct {
	quality.MovieVersionPolicy
	HDFiles     int    `json:"hd_files"`
	UHDFiles    int    `json:"uhd_files"`
	HDDownload  string `json:"hd_download"`
	UHDDownload string `json:"uhd_download"`
}

func (s *Service) MovieVersions(ctx context.Context, id int64) (MovieVersions, error) {
	var out MovieVersions
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	out.MovieVersionPolicy, err = quality.ReadMovieVersions(ctx, tx, id)
	if err != nil {
		return out, err
	}
	err = tx.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM media_files WHERE movie_id=? AND video_resolution BETWEEN 1 AND 1080),
 (SELECT COUNT(*) FROM media_files WHERE movie_id=? AND video_resolution=2160),
 COALESCE((SELECT state FROM downloads WHERE movie_id=? AND movie_version='hd' AND state NOT IN ('completed','cancelled')),''),
 COALESCE((SELECT state FROM downloads WHERE movie_id=? AND movie_version='uhd' AND state NOT IN ('completed','cancelled')),'')`, id, id, id, id).Scan(&out.HDFiles, &out.UHDFiles, &out.HDDownload, &out.UHDDownload)
	return out, err
}

func (s *Service) SetMovieVersions(ctx context.Context, id int64, in quality.MovieVersionPolicy) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	old, err := quality.ReadMovieVersions(ctx, tx, id)
	if err != nil {
		return err
	}
	if old.Revision != in.Revision {
		return &Error{"revision_conflict", "Movie version settings changed. Reload before saving."}
	}
	for version, profileID := range map[string]int64{"hd": in.HDProfileID, "uhd": in.UHDProfileID} {
		p, e := quality.GetTx(ctx, tx, profileID)
		if e != nil {
			return invalid("Choose an existing quality profile for each version.")
		}
		if in.Enabled && !quality.MovieVersionMatches(version, p.Cutoff) {
			return invalid("The HD profile needs a cutoff of 1080p or lower; the 4K profile needs a 2160p cutoff.")
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO movie_version_settings(media_id,enabled,hd_profile_id,uhd_profile_id) VALUES(?,?,?,?) ON CONFLICT(media_id) DO UPDATE SET enabled=excluded.enabled,hd_profile_id=excluded.hd_profile_id,uhd_profile_id=excluded.uhd_profile_id,revision=movie_version_settings.revision+1`, id, in.Enabled, in.HDProfileID, in.UHDProfileID)
	if err != nil {
		return err
	}
	return tx.Commit()
}
