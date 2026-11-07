package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

// The owner-supplied torrent is the most explicit release selection possible:
// it targets one title, carries its own metainfo and never involves an
// indexer. It still flows through the same reservation, reconcile-by-hash,
// client submission and verified import as every searched release.
const manualCandidateTTL = 1800 // seconds, matching searched selections

func (s *Service) storeCandidate(ctx context.Context, movieID int64, v Candidate) (string, error) {
	b, e := json.Marshal(v)
	if e != nil {
		return "", e
	}
	sealed, e := s.secrets.Seal("release", b)
	if e != nil {
		return "", e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	if e = organization.EnsureMediaAvailableTx(ctx, tx); e != nil {
		return "", e
	}
	now := time.Now().Unix()
	if _, e = tx.ExecContext(ctx, `INSERT INTO release_candidates(id,movie_id,title,size_bytes,seeders,peers,published_at,info_url,payload,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, v.ID, movieID, v.Title, v.Size, v.Seeders, v.Peers, v.Published, v.InfoURL, sealed, now+manualCandidateTTL); e != nil {
		return "", e
	}
	return v.ID, tx.Commit()
}

// QueueManualTorrent registers an uploaded .torrent file as the selected
// release for a movie and queues it immediately. The optional version selects
// the managed HD/4K band or an additional edition, exactly like a searched
// release. The upload itself is the review: the owner chose both the file and
// the target.
func (s *Service) QueueManualTorrent(ctx context.Context, movieID int64, version string, data []byte) (Job, bool, error) {
	if version != "" && version != "hd" && version != "uhd" && version != "additional" {
		return Job{}, false, problem("invalid_version", "Choose a managed version, an additional edition, or leave the version empty.")
	}
	if _, e := s.MovieByID(ctx, movieID); e != nil {
		return Job{}, false, e
	}
	mt, e := TorrentInfo(data)
	if e != nil {
		return Job{}, false, e
	}
	if e = s.refuseKnownTorrent(ctx, mt.InfoHash); e != nil {
		return Job{}, false, e
	}
	releaseID, e := s.storeCandidate(ctx, movieID, Candidate{
		Release: Release{
			ID: ID(), MovieID: movieID, Title: mt.Name, Size: mt.Size,
			Published:   time.Now().UTC().Format(time.RFC3339),
			IndexerName: "Manual upload",
		},
		Hash:    mt.InfoHash,
		Kind:    "movie",
		Manual:  true,
		Torrent: data,
	})
	if e != nil {
		return Job{}, false, e
	}
	return s.queueVersion(ctx, releaseID, nil, 0, version)
}

// QueueManualSeriesTorrent registers an uploaded .torrent file for a series
// season. With no episode numbers the torrent is treated as a season pack
// covering the season's selected missing episodes; with numbers it covers
// exactly those episodes. The import still requires the explicit per-file
// episode mapping review before anything is registered.
func (s *Service) QueueManualSeriesTorrent(ctx context.Context, seriesID int64, season int, episodes []int, data []byte) (Job, bool, error) {
	if season < 0 || season > 9999 {
		return Job{}, false, problem("invalid_season", "Choose the season this torrent covers.")
	}
	if _, e := library.New(s.db).TVShow(ctx, seriesID); e != nil {
		return Job{}, false, e
	}
	var loaded string
	if e := s.db.QueryRowContext(ctx, `SELECT loaded_at FROM seasons WHERE series_id=? AND number=?`, seriesID, season).Scan(&loaded); e != nil {
		return Job{}, false, e
	}
	if loaded == "" {
		return Job{}, false, problem("episodes_not_loaded", "Load this season's episodes before attaching a torrent.")
	}
	targets, e := s.manualTargets(ctx, seriesID, season, episodes)
	if e != nil {
		return Job{}, false, e
	}
	mt, e := TorrentInfo(data)
	if e != nil {
		return Job{}, false, e
	}
	if e = s.refuseKnownTorrent(ctx, mt.InfoHash); e != nil {
		return Job{}, false, e
	}
	releaseID, e := s.storeCandidate(ctx, seriesID, Candidate{
		Release: Release{
			ID: ID(), MovieID: seriesID, Title: mt.Name, Size: mt.Size,
			Published:   time.Now().UTC().Format(time.RFC3339),
			IndexerName: "Manual upload",
		},
		Hash:    mt.InfoHash,
		Kind:    "series",
		Targets: targets,
		Manual:  true,
		Torrent: data,
	})
	if e != nil {
		return Job{}, false, e
	}
	return s.queueVersion(ctx, releaseID, nil, 0, "")
}

// manualTargets resolves the episodes a manual torrent covers, mirroring the
// season search's selection rules and bounds.
func (s *Service) manualTargets(ctx context.Context, seriesID int64, season int, episodes []int) ([]TVTarget, error) {
	if len(episodes) > 100 {
		return nil, problem("too_many_episodes", "A season download supports up to 100 selected missing episodes. Choose individual episodes or reduce the season's selections.")
	}
	query := `SELECT e.id,e.tmdb_id,s.number,e.number,e.title,a.absolute_number FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id LEFT JOIN anime_episode_numbers a ON a.episode_id=e.id WHERE s.series_id=? AND s.number=? AND (m.monitored=1 AND s.monitored=1 AND e.monitored=1) AND NOT EXISTS(SELECT 1 FROM episode_files f WHERE f.episode_id=e.id)`
	args := []any{seriesID, season}
	if len(episodes) > 0 {
		places := make([]string, len(episodes))
		for i, n := range episodes {
			if n < 0 || n > 9999 {
				return nil, problem("invalid_season", "Choose valid episode numbers.")
			}
			places[i] = "?"
			args = append(args, n)
		}
		query += " AND e.number IN (" + strings.Join(places, ",") + ")"
	}
	query += " ORDER BY e.number LIMIT 101"
	rows, e := s.db.QueryContext(ctx, query, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	targets := []TVTarget{}
	for rows.Next() {
		var t TVTarget
		if e = rows.Scan(&t.EpisodeID, &t.TMDbID, &t.Season, &t.Number, &t.Title, &t.AbsoluteNumber); e != nil {
			return nil, e
		}
		targets = append(targets, t)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(targets) == 0 {
		return nil, problem("no_episode_targets", "There are no selected missing episodes for this torrent. Load episodes and check monitoring, or choose episodes that are still missing.")
	}
	if len(targets) > 100 {
		return nil, problem("too_many_episodes", "A season download supports up to 100 selected missing episodes. Choose individual episodes or reduce the season's selections.")
	}
	return targets, nil
}

// refuseKnownTorrent gives the owner the same early duplicate message a
// searched release would produce at queue time.
func (s *Service) refuseKnownTorrent(ctx context.Context, hash string) error {
	if _, e := s.jobBy(ctx, "d.info_hash", hash); e == nil {
		return problem("duplicate_hash", "This torrent is already attached to an Activity job. Open it there or upload a different file.")
	} else if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	return nil
}
