package transfer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/settings"
)

type TVTarget struct {
	EpisodeID int64  `json:"episode_id"`
	TMDbID    int64  `json:"tmdb_id"`
	Season    int    `json:"season"`
	Number    int    `json:"number"`
	Title     string `json:"title"`
	Skipped   bool   `json:"skipped"`
	Imported  bool   `json:"imported"`
}

func (s *Service) SearchTV(ctx context.Context, seriesID int64, season, episode int, query string) ([]Release, error) {
	show, e := library.New(s.db).TVShow(ctx, seriesID)
	if e != nil {
		return nil, e
	}
	var loaded string
	if e = s.db.QueryRowContext(ctx, `SELECT loaded_at FROM seasons WHERE series_id=? AND number=?`, seriesID, season).Scan(&loaded); e != nil {
		return nil, e
	}
	if loaded == "" {
		return nil, problem("episodes_not_loaded", "Load this season's episodes before searching for releases.")
	}
	rows, e := s.db.QueryContext(ctx, `SELECT e.id,e.tmdb_id,s.number,e.number,e.title FROM episodes e JOIN seasons s ON s.id=e.season_id JOIN media_items m ON m.id=s.series_id WHERE s.series_id=? AND s.number=? AND (?=0 OR e.number=?) AND (?<>0 OR (m.monitored=1 AND s.monitored=1 AND e.monitored=1)) AND NOT EXISTS(SELECT 1 FROM episode_files f WHERE f.episode_id=e.id) ORDER BY e.number LIMIT 101`, seriesID, season, episode, episode, episode)
	if e != nil {
		return nil, e
	}
	targets := []TVTarget{}
	for rows.Next() {
		var t TVTarget
		if e = rows.Scan(&t.EpisodeID, &t.TMDbID, &t.Season, &t.Number, &t.Title); e != nil {
			rows.Close()
			return nil, e
		}
		targets = append(targets, t)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(targets) == 0 {
		return nil, problem("no_episode_targets", "There are no selected missing episodes for this search. Load episodes and check monitoring, or choose an individual missing episode.")
	}
	if len(targets) > 100 {
		return nil, problem("too_many_episodes", "A season download supports up to 100 selected missing episodes. Choose individual episodes or reduce the season's selections.")
	}
	query = strings.TrimSpace(query)
	if query == "" {
		query = fmt.Sprintf("%s S%02d", show.Title, season)
		if episode > 0 {
			query += fmt.Sprintf("E%02d", episode)
		}
	}
	if !textOK(query, 200) {
		return nil, problem("invalid_search", "Enter a release search with 1–200 characters.")
	}
	c, e := s.indexerConfig(ctx)
	if e != nil {
		return nil, e
	}
	c.Category = "5000"
	candidates, e := s.indexer.Search(ctx, c, query)
	if e != nil {
		return nil, e
	}
	if len(candidates) > 50 {
		candidates = candidates[:50]
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	scope := fmt.Sprintf("tv:%d:%d", season, episode)
	if _, e = tx.ExecContext(ctx, `DELETE FROM release_candidates WHERE id NOT IN (SELECT release_id FROM downloads) AND ((movie_id=? AND scope=?) OR expires_at<?)`, seriesID, scope, now); e != nil {
		return nil, e
	}
	out := []Release{}
	for _, v := range candidates {
		v.ID = ID()
		v.MovieID = seriesID
		v.Kind = "series"
		v.Targets = targets
		v.Generation = c.Generation
		b, e := json.Marshal(v)
		if e != nil {
			return nil, e
		}
		sealed, e := s.secrets.Seal("release", b)
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO release_candidates(id,movie_id,title,size_bytes,seeders,peers,published_at,payload,expires_at,scope) VALUES(?,?,?,?,?,?,?,?,?,?)`, v.ID, seriesID, v.Title, v.Size, v.Seeders, v.Peers, v.Published, sealed, now+1800, scope); e != nil {
			return nil, e
		}
		out = append(out, v.Release)
	}
	return out, tx.Commit()
}
func (s *Service) targets(ctx context.Context, id string) ([]TVTarget, error) {
	rows, e := s.db.QueryContext(ctx, `SELECT t.episode_id,t.tmdb_id,t.season_number,t.episode_number,t.title,t.skipped,EXISTS(SELECT 1 FROM episode_files ef JOIN media_files mf ON mf.id=ef.media_file_id WHERE ef.episode_id=t.episode_id AND mf.download_id=t.download_id) FROM download_targets t WHERE t.download_id=? ORDER BY t.season_number,t.episode_number`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []TVTarget{}
	for rows.Next() {
		var t TVTarget
		if e = rows.Scan(&t.EpisodeID, &t.TMDbID, &t.Season, &t.Number, &t.Title, &t.Skipped, &t.Imported); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func sameTargets(a, b []TVTarget) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[int64]bool{}
	for _, t := range a {
		seen[t.EpisodeID] = true
	}
	for _, t := range b {
		if !seen[t.EpisodeID] {
			return false
		}
	}
	return true
}
func validateTVTarget(ctx context.Context, tx *sql.Tx, seriesID int64, t TVTarget) error {
	var count int
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE e.id=? AND e.tmdb_id=? AND e.number=? AND s.series_id=? AND s.number=?`, t.EpisodeID, t.TMDbID, t.Number, seriesID, t.Season).Scan(&count); e != nil {
		return e
	}
	if count != 1 {
		return problem("episode_changed", "An episode's metadata identity changed. Review the show before continuing this download.")
	}
	return nil
}
func reserveTV(ctx context.Context, tx *sql.Tx, id string, seriesID int64, targets []TVTarget) error {
	for _, t := range targets {
		if t.Skipped {
			continue
		}
		if e := validateTVTarget(ctx, tx, seriesID, t); e != nil {
			return e
		}
		var busy int
		if e := tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM episode_downloads WHERE episode_id=? AND download_id<>?)+(SELECT COUNT(*) FROM episode_files ef JOIN media_files mf ON mf.id=ef.media_file_id WHERE ef.episode_id=? AND mf.download_id<>?)`, t.EpisodeID, id, t.EpisodeID, id).Scan(&busy); e != nil {
			return e
		}
		if busy > 0 {
			return problem("episode_busy", "An episode is already imported or reserved by another download. Open Activity to review it.")
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO episode_downloads(episode_id,download_id) VALUES(?,?) ON CONFLICT(episode_id) DO NOTHING`, t.EpisodeID, id); e != nil {
			return e
		}
	}
	return nil
}
func refreshTVStatus(ctx context.Context, tx *sql.Tx, id int64) error {
	_, e := tx.ExecContext(ctx, `UPDATE media_items SET status=CASE WHEN EXISTS(SELECT 1 FROM downloads WHERE movie_id=? AND kind='series' AND state NOT IN ('completed','cancelled')) THEN 'downloading' WHEN EXISTS(SELECT 1 FROM media_files WHERE series_id=?) THEN 'available' ELSE 'missing' END WHERE id=? AND kind='series'`, id, id, id)
	return e
}
func (s *Service) queueTV(ctx context.Context, v Candidate, releaseID string, sealed []byte, hash string, client ClientConfig) (Job, bool, error) {
	if len(v.Targets) == 0 || len(v.Targets) > 100 {
		return Job{}, false, problem("invalid_release", "This TV selection has invalid episode targets. Search again.")
	}
	show, e := library.New(s.db).TVShow(ctx, v.MovieID)
	if e != nil {
		return Job{}, false, e
	}
	prefs, e := settings.New(s.db).Get(ctx)
	if e != nil {
		return Job{}, false, e
	}
	id := ID()
	now := time.Now().Unix()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Job{}, false, e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at,kind) VALUES(?,?,?,?,?,?,?,?,'queued',?,?,'series')`, id, v.MovieID, releaseID, v.Title, hash, sealed, client.URL, path.Join(client.DownloadPath, "ReelScarey", id), now, now); e != nil {
		return Job{}, false, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO download_tv(download_id,series_tmdb_id,series_title,series_year,season_number,series_folder,naming) VALUES(?,?,?,?,?,?,?)`, id, show.TMDbID, show.Title, show.Year, v.Targets[0].Season, prefs.SeriesFolder, prefs.EpisodeNaming); e != nil {
		return Job{}, false, e
	}
	seen := map[int64]bool{}
	for _, t := range v.Targets {
		if seen[t.EpisodeID] || t.Season != v.Targets[0].Season || t.Skipped || t.Imported {
			return Job{}, false, problem("invalid_release", "This TV selection contains invalid episode targets.")
		}
		seen[t.EpisodeID] = true
		if _, e = tx.ExecContext(ctx, `INSERT INTO download_targets(download_id,episode_id,tmdb_id,season_number,episode_number,title) VALUES(?,?,?,?,?,?)`, id, t.EpisodeID, t.TMDbID, t.Season, t.Number, t.Title); e != nil {
			return Job{}, false, e
		}
	}
	if e = reserveTV(ctx, tx, id, v.MovieID, v.Targets); e != nil {
		return Job{}, false, e
	}
	if e = refreshTVStatus(ctx, tx, v.MovieID); e != nil {
		return Job{}, false, e
	}
	if e = tx.Commit(); e != nil {
		return Job{}, false, e
	}
	j, e := s.Job(ctx, id)
	return j, true, e
}
