package transfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

// Re-attaching a registered file moves it between catalog titles: the owner
// points at a file that is already registered somewhere else and confirms it
// belongs to the selected title instead. The registration is re-parented in
// one transaction — the same media_files row keeps its id, observations,
// checksums and subtitle records — and then the reviewed organization plan
// moves and renames the file into the new title's canonical layout with the
// same journaled machinery every organization batch uses. Nothing is
// overwritten and a refusal never changes the library.

type ReattachRequest struct {
	Kind     string  `json:"kind"`
	MediaID  int64   `json:"media_id"`
	Episodes []int64 `json:"episode_ids"`
	Relative string  `json:"relative"`
	Reviewed bool    `json:"reviewed"`
}

// ReattachFile re-parents one registered file to a different catalog title
// and queues its canonical move. The returned plan shows exactly what will
// move; the background organization worker executes it with journaling.
func (s *Service) ReattachFile(ctx context.Context, in ReattachRequest) (organization.Plan, error) {
	defer organization.ReadMedia()()
	if e := organization.EnsureMediaAvailable(ctx, s.db); e != nil {
		return organization.Plan{}, e
	}
	if !in.Reviewed || in.MediaID < 1 || len(in.Episodes) > 20 {
		return organization.Plan{}, problem("invalid_mapping", "Confirm the title and episode match before re-attaching this file.")
	}
	relative := strings.TrimSpace(strings.ReplaceAll(in.Relative, "\\", "/"))
	if relative == "" {
		return organization.Plan{}, problem("invalid_selection", "Choose a registered file to re-attach.")
	}
	if _, e := safeRelative(relative); e != nil {
		return organization.Plan{}, e
	}
	var (
		fileID, oldOwner  int64
		oldKind, oldTitle string
		size              int64
	)
	e := s.db.QueryRowContext(ctx, `SELECT f.id,COALESCE(f.movie_id,f.series_id),m.kind,m.title,f.size_bytes FROM media_files f JOIN media_items m ON m.id=COALESCE(f.movie_id,f.series_id) WHERE f.relative_path=? COLLATE NOCASE`, relative).Scan(&fileID, &oldOwner, &oldKind, &oldTitle, &size)
	if errors.Is(e, sql.ErrNoRows) {
		return organization.Plan{}, problem("invalid_selection", "This file is not registered in the library.")
	}
	if e != nil {
		return organization.Plan{}, e
	}
	if oldKind != in.Kind {
		return organization.Plan{}, problem("invalid_mapping", "Choose a catalog title of the same type as the file's current registration.")
	}
	var targetKind, targetTitle string
	if e = s.db.QueryRowContext(ctx, `SELECT kind,title FROM media_items WHERE id=?`, in.MediaID).Scan(&targetKind, &targetTitle); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return organization.Plan{}, problem("invalid_mapping", "Choose a catalog title for this file.")
		}
		return organization.Plan{}, e
	}
	if targetKind != in.Kind {
		return organization.Plan{}, problem("invalid_mapping", "Choose a catalog title of the same type as this import.")
	}
	if oldOwner == in.MediaID {
		return organization.Plan{}, problem("duplicate_file", "This file is already registered for this title.")
	}
	// An active acquisition still depends on the file; finished tracking rows
	// no longer hold it.
	var replacements int
	if e = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM acquisition_replacements ar
		WHERE ar.media_file_id=? AND (
			(ar.source_kind='download' AND EXISTS(SELECT 1 FROM downloads d WHERE d.id=ar.job_id AND d.state NOT IN ('completed','cancelled')))
			OR
			(ar.source_kind='youtube' AND EXISTS(SELECT 1 FROM youtube_jobs y WHERE y.id=ar.job_id AND y.state NOT IN ('completed','cancelled','failed')))
		)`, fileID).Scan(&replacements); e != nil {
		return organization.Plan{}, e
	}
	if replacements > 0 {
		return organization.Plan{}, problem("replacement_pending", "A tracked replacement still depends on this file. Finish or cancel it first.")
	}
	// The file must still be present and unchanged before anything moves.
	root, e := s.existingRoot(ctx, nil)
	if e != nil {
		return organization.Plan{}, e
	}
	defer root.Close()
	if e = segments(root, relative, false); e != nil {
		return organization.Plan{}, problem("source_unavailable", "The registered file is missing or inaccessible.")
	}
	info, e := root.Lstat(filepath.FromSlash(relative))
	if e != nil || !info.Mode().IsRegular() || info.Size() != size {
		return organization.Plan{}, problem("source_changed", "The registered file changed on disk. Review it before re-attaching.")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return organization.Plan{}, e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return organization.Plan{}, e
	}
	targets := []TVTarget{}
	if in.Kind == "series" {
		seen := map[int64]bool{}
		for _, id := range in.Episodes {
			if seen[id] {
				return organization.Plan{}, mappingError()
			}
			seen[id] = true
			var t TVTarget
			if e = tx.QueryRowContext(ctx, `SELECT e.id,e.tmdb_id,s.number,e.number,e.title FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE e.id=? AND s.series_id=? AND s.loaded_at<>''`, id, in.MediaID).Scan(&t.EpisodeID, &t.TMDbID, &t.Season, &t.Number, &t.Title); e != nil {
				return organization.Plan{}, problem("invalid_mapping", "Load the selected show's season metadata and choose its episodes.")
			}
			targets = append(targets, t)
		}
		sort.Slice(targets, func(i, j int) bool {
			if targets[i].Season != targets[j].Season {
				return targets[i].Season < targets[j].Season
			}
			return targets[i].Number < targets[j].Number
		})
		if len(targets) == 0 {
			return organization.Plan{}, mappingError()
		}
		for n, t := range targets {
			if n > 0 && (t.Season != targets[0].Season || t.Number != targets[n-1].Number+1) {
				return organization.Plan{}, problem("invalid_mapping", "One file can cover consecutive episodes from one season, up to 20 episodes.")
			}
		}
		if e = existingTargetFree(ctx, tx, in.MediaID, "series", targets); e != nil {
			return organization.Plan{}, e
		}
	} else if len(in.Episodes) != 0 {
		return organization.Plan{}, problem("invalid_mapping", "A movie file cannot be mapped to episodes.")
	}
	if e = existingTargetFree(ctx, tx, in.MediaID, "movie", nil); e != nil && in.Kind == "movie" {
		return organization.Plan{}, e
	}
	var movieID, seriesID any
	if in.Kind == "movie" {
		movieID = in.MediaID
	} else {
		seriesID = in.MediaID
	}
	res, e := tx.ExecContext(ctx, `UPDATE media_files SET movie_id=?,series_id=? WHERE id=? AND relative_path=?`, movieID, seriesID, fileID, relative)
	if e != nil {
		return organization.Plan{}, e
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return organization.Plan{}, problem("source_changed", "The file's registration changed. Reload and review it again.")
	}
	if _, e = tx.ExecContext(ctx, `DELETE FROM episode_files WHERE media_file_id=?`, fileID); e != nil {
		return organization.Plan{}, e
	}
	for _, t := range targets {
		if _, e = tx.ExecContext(ctx, `INSERT INTO episode_files(episode_id,media_file_id) VALUES(?,?)`, t.EpisodeID, fileID); e != nil {
			return organization.Plan{}, e
		}
	}
	// The old title loses the file; a movie with nothing left and nothing
	// reserved goes back to missing, a show recalculates per episode.
	if oldKind == "series" {
		if e = refreshTVStatus(ctx, tx, oldOwner); e != nil {
			return organization.Plan{}, e
		}
	} else if _, e = tx.ExecContext(ctx, `UPDATE media_items SET status='missing' WHERE id=? AND kind='movie' AND NOT EXISTS(SELECT 1 FROM media_files WHERE movie_id=?) AND NOT EXISTS(SELECT 1 FROM downloads WHERE movie_id=? AND state NOT IN ('completed','cancelled'))`, oldOwner, oldOwner, oldOwner); e != nil {
		return organization.Plan{}, e
	}
	if in.Kind == "series" {
		if e = refreshTVStatus(ctx, tx, in.MediaID); e != nil {
			return organization.Plan{}, e
		}
	} else if _, e = tx.ExecContext(ctx, `UPDATE media_items SET status='available' WHERE id=?`, in.MediaID); e != nil {
		return organization.Plan{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',?,?,?)`, oldOwner, oldTitle, fmt.Sprintf("File re-attached to %q.", targetTitle)); e != nil {
		return organization.Plan{}, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',?,?,?)`, in.MediaID, targetTitle, fmt.Sprintf("File re-attached from %q.", oldTitle)); e != nil {
		return organization.Plan{}, e
	}
	if e = library.QueueMetadataRefreshTx(ctx, tx, in.MediaID, false); e != nil {
		return organization.Plan{}, e
	}
	if e = tx.Commit(); e != nil {
		return organization.Plan{}, e
	}
	// The reviewed move uses the standard organization plan: canonical naming,
	// sidecar companions, subtitle reference switches and crash recovery.
	org := organization.New(s.db, s.cfg)
	plan, e := org.Preview(ctx, []int64{fileID}, "")
	if e != nil {
		return plan, e
	}
	return org.Apply(ctx, plan.ID)
}
