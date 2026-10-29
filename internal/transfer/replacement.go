package transfer

import (
	"context"
	"encoding/json"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

func (s *Service) restrictReplacementCandidates(ctx context.Context, releases []Release, fileID int64) ([]Release, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT episode_id FROM episode_files WHERE media_file_id=? ORDER BY episode_id`, fileID)
	if e != nil {
		return nil, e
	}
	ids := map[int64]bool{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids[id] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(ids) == 0 || len(ids) > 20 {
		return nil, problem("invalid_file", "Choose a registered episode file covering at most 20 episodes.")
	}
	for _, release := range releases {
		var sealed []byte
		if e = s.db.QueryRowContext(ctx, `SELECT payload FROM release_candidates WHERE id=?`, release.ID).Scan(&sealed); e != nil {
			return nil, e
		}
		plain, e := s.secrets.Open("release", sealed)
		if e != nil {
			return nil, e
		}
		var c Candidate
		if e = json.Unmarshal(plain, &c); e != nil {
			return nil, e
		}
		targets := []TVTarget{}
		for _, t := range c.Targets {
			if ids[t.EpisodeID] {
				targets = append(targets, t)
			}
		}
		if len(targets) != len(ids) {
			return nil, problem("invalid_file", "The replacement file must belong entirely to the selected season.")
		}
		c.Targets = targets
		plain, e = json.Marshal(c)
		if e != nil {
			return nil, e
		}
		sealed, e = s.secrets.Seal("release", plain)
		if e != nil {
			return nil, e
		}
		if _, e = s.db.ExecContext(ctx, `UPDATE release_candidates SET payload=? WHERE id=?`, sealed, c.ID); e != nil {
			return nil, e
		}
	}
	return releases, nil
}

func (s *Service) ensureReplacementMutation(ctx context.Context, job string) error {
	var count int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements WHERE source_kind='download' AND job_id=? AND plan_id<>''`, job).Scan(&count); e != nil {
		return e
	}
	if count > 0 {
		return problem("download_busy", "Manage this acquisition through its saved organization replacement plan.")
	}
	return organization.EnsureMediaAvailable(ctx, s.db)
}
