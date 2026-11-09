package transfer

import (
	"context"
	"database/sql"
	"github.com/faaaaq/ReelScarey/internal/quality"
)

// Additional is an explicit manual acquisition that preserves every registered
// file. HD and UHD are independently reserved targets in the opt-in policy.
func (s *Service) QueueMovieVersion(ctx context.Context, releaseID, version string) (Job, bool, error) {
	if version != "hd" && version != "uhd" && version != "additional" {
		return Job{}, false, problem("invalid_version", "Choose HD, 4K or an additional movie version.")
	}
	return s.queueVersion(ctx, releaseID, nil, 0, version, 0)
}
func (s *Service) QueueAutomaticVersion(ctx context.Context, releaseID, version string, guard QueueGuard) (Job, bool, error) {
	if guard == nil || (version != "hd" && version != "uhd") {
		return Job{}, false, problem("invalid_version", "Automatic movie versions require a current version policy.")
	}
	return s.queueVersion(ctx, releaseID, guard, 0, version, 0)
}

func (s *Service) checkReplacementSelection(ctx context.Context, job string, fileID int64) error {
	if fileID == 0 {
		return nil
	}
	var saved int64
	err := s.db.QueryRowContext(ctx, `SELECT media_file_id FROM acquisition_replacements WHERE source_kind='download' AND job_id=?`, job).Scan(&saved)
	if err != nil || saved != fileID {
		return problem("duplicate_release", "This release is already bound to another file. Open its existing download or choose a different release.")
	}
	return nil
}

func reserveMovieVersion(ctx context.Context, tx *sql.Tx, mediaID int64, version, title, exceptJob string, replacementID int64) error {
	if version == "hd" || version == "uhd" {
		policy, err := quality.ReadMovieVersions(ctx, tx, mediaID)
		if err != nil {
			return err
		}
		if !policy.Enabled {
			return problem("invalid_version", "Enable HD + 4K downloads on this movie before choosing a managed version.")
		}
		var target quality.Target
		if err = tx.QueryRowContext(ctx, `SELECT title,year FROM media_items WHERE id=?`, mediaID).Scan(&target.Title, &target.Year); err != nil {
			return err
		}
		d := quality.Evaluate(quality.Profile{}, target, quality.Release{Title: title})
		if !quality.MovieVersionMatches(version, d.Quality) {
			return problem("invalid_version", "Choose a release explicitly labeled with the selected version's resolution.")
		}
	}
	var active int
	// Legacy unversioned jobs remain exclusive. Additional manual acquisitions
	// have their own unique destination; managed bands may run simultaneously.
	err := tx.QueryRowContext(ctx, `SELECT
 (SELECT COUNT(*) FROM downloads WHERE movie_id=? AND kind='movie' AND id<>? AND state NOT IN ('completed','cancelled') AND (? IN ('','replacement') OR movie_version IN ('','additional','replacement') OR movie_version=?))+
 (SELECT COUNT(*) FROM youtube_jobs WHERE media_id=? AND target_kind='movie' AND state NOT IN ('completed','cancelled','failed'))+
 (SELECT COUNT(*) FROM media_files WHERE movie_id=? AND ? NOT IN ('additional','replacement') AND (?='' OR video_resolution=0 OR (?='hd' AND video_resolution BETWEEN 1 AND 1080) OR (?='uhd' AND video_resolution=2160)))`, mediaID, exceptJob, version, version, mediaID, mediaID, version, version, version, version).Scan(&active)
	if err != nil {
		return err
	}
	if active > 0 {
		return problem("movie_busy", "This movie version is already on disk or reserved. Choose an additional version or an explicit replacement file.")
	}
	if replacementID > 0 {
		var valid int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE id=? AND movie_id=?`, replacementID, mediaID).Scan(&valid); err != nil {
			return err
		}
		if valid != 1 {
			return problem("invalid_file", "Choose a registered file belonging to this movie.")
		}
	}
	return nil
}
