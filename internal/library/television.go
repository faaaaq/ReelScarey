package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

var ErrRevision = errors.New("This show changed. Refresh it before saving your choice.")

type TVSeason struct {
	ID int64 `json:"id"`
	metadata.SeasonSummary
	Monitored         bool   `json:"monitored"`
	LoadedAt          string `json:"loaded_at"`
	KnownEpisodes     int    `json:"known_episodes"`
	SelectedEpisodes  int    `json:"selected_episodes"`
	AvailableEpisodes int    `json:"available_episodes"`
}
type TVEpisode struct {
	ID int64 `json:"id"`
	metadata.Episode
	Monitored          bool   `json:"monitored"`
	EffectiveMonitored bool   `json:"effective_monitored"`
	Available          bool   `json:"available"`
	FileID             *int64 `json:"file_id,omitempty"`
	FilePath           string `json:"file_path"`
}
type TVSeries struct {
	Item
	SeriesStatus string     `json:"series_status"`
	Revision     int64      `json:"revision"`
	UpdatedAt    string     `json:"updated_at"`
	Seasons      []TVSeason `json:"seasons"`
}
type TVSeasonDetails struct {
	Season          TVSeason    `json:"season"`
	Episodes        []TVEpisode `json:"episodes"`
	Revision        int64       `json:"revision"`
	SeriesMonitored bool        `json:"series_monitored"`
}

func (s *Store) Series(ctx context.Context, tmdbID int64) (Item, error) {
	return scan(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM media_items WHERE kind='series' AND tmdb_id=?", tmdbID))
}

const seasonColumns = `s.id,s.tmdb_id,s.number,s.title,s.overview,s.air_date,s.episode_count,s.monitored,s.loaded_at,(SELECT COUNT(*) FROM episodes e WHERE e.season_id=s.id),(SELECT COUNT(*) FROM episodes e WHERE e.season_id=s.id AND e.monitored=1),(SELECT COUNT(*) FROM episodes e JOIN episode_files ef ON ef.episode_id=e.id WHERE e.season_id=s.id)`

func scanSeason(row scanner) (TVSeason, error) {
	var v TVSeason
	e := row.Scan(&v.ID, &v.TMDbID, &v.Number, &v.Title, &v.Overview, &v.AirDate, &v.EpisodeCount, &v.Monitored, &v.LoadedAt, &v.KnownEpisodes, &v.SelectedEpisodes, &v.AvailableEpisodes)
	return v, e
}
func (s *Store) TVShow(ctx context.Context, id int64) (TVSeries, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return TVSeries{}, e
	}
	defer tx.Rollback()
	item, e := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM media_items WHERE kind='series' AND id=?", id))
	if e != nil {
		return TVSeries{}, e
	}
	out := TVSeries{Item: item, Seasons: []TVSeason{}}
	if e = tx.QueryRowContext(ctx, `SELECT series_status,revision,updated_at FROM series_details WHERE media_id=?`, id).Scan(&out.SeriesStatus, &out.Revision, &out.UpdatedAt); e != nil {
		return TVSeries{}, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+seasonColumns+` FROM seasons s WHERE s.series_id=? ORDER BY s.number`, id)
	if e != nil {
		return TVSeries{}, e
	}
	for rows.Next() {
		v, e := scanSeason(rows)
		if e != nil {
			rows.Close()
			return TVSeries{}, e
		}
		out.Seasons = append(out.Seasons, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return TVSeries{}, e
	}
	return out, tx.Commit()
}
func (s *Store) TVSeason(ctx context.Context, id int64, number int) (TVSeasonDetails, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return TVSeasonDetails{}, e
	}
	defer tx.Rollback()
	out := TVSeasonDetails{Episodes: []TVEpisode{}}
	if e = tx.QueryRowContext(ctx, `SELECT d.revision,m.monitored FROM series_details d JOIN media_items m ON m.id=d.media_id WHERE d.media_id=? AND m.kind='series'`, id).Scan(&out.Revision, &out.SeriesMonitored); e != nil {
		return out, e
	}
	out.Season, e = scanSeason(tx.QueryRowContext(ctx, `SELECT `+seasonColumns+` FROM seasons s WHERE s.series_id=? AND s.number=?`, id, number))
	if e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT e.id,e.tmdb_id,e.number,e.title,e.overview,e.air_date,e.runtime_minutes,e.monitored,ef.media_file_id IS NOT NULL,COALESCE(mf.relative_path,''),ef.media_file_id FROM episodes e LEFT JOIN episode_files ef ON ef.episode_id=e.id LEFT JOIN media_files mf ON mf.id=ef.media_file_id WHERE e.season_id=? ORDER BY e.number`, out.Season.ID)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v TVEpisode
		if e = rows.Scan(&v.ID, &v.TMDbID, &v.Number, &v.Title, &v.Overview, &v.AirDate, &v.RuntimeMinutes, &v.Monitored, &v.Available, &v.FilePath, &v.FileID); e != nil {
			rows.Close()
			return out, e
		}
		v.EffectiveMonitored = out.SeriesMonitored && out.Season.Monitored && v.Monitored
		out.Episodes = append(out.Episodes, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func putSeasons(ctx context.Context, tx *sql.Tx, id int64, seasons []metadata.SeasonSummary) error {
	for _, v := range seasons {
		var prior int64
		err := tx.QueryRowContext(ctx, `SELECT tmdb_id FROM seasons WHERE series_id=? AND number=?`, id, v.Number).Scan(&prior)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && prior != v.TMDbID {
			return &metadata.Error{Code: "tmdb_invalid_response", Message: "Season identity changed. Saved seasons were retained for review."}
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO seasons(series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(series_id,number) DO UPDATE SET tmdb_id=excluded.tmdb_id,title=excluded.title,overview=excluded.overview,air_date=excluded.air_date,episode_count=excluded.episode_count`, id, v.TMDbID, v.Number, v.Title, v.Overview, v.AirDate, v.EpisodeCount, v.Number != 0); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) AddSeries(ctx context.Context, show metadata.Series, monitored bool, category ...string) (Item, bool, error) {
	return s.AddSeriesAuthorized(ctx, show, monitored, nil, category...)
}

func (s *Store) AddSeriesAuthorized(ctx context.Context, show metadata.Series, monitored bool, authorize func(*sql.Tx) error, category ...string) (Item, bool, error) {
	section, err := addCategory(category)
	if err != nil {
		return Item{}, false, err
	}
	if show.TMDbID < 1 || show.Title == "" {
		return Item{}, false, errors.New("invalid series metadata")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return Item{}, false, e
	}
	defer tx.Rollback()
	if authorize != nil {
		if err := authorize(tx); err != nil {
			return Item{}, false, err
		}
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO media_items(kind,tmdb_id,title,year,monitored,overview,poster_path,release_date,category) VALUES('series',?,?,?,?,?,?,?,?) ON CONFLICT(kind,tmdb_id) DO NOTHING`, show.TMDbID, show.Title, show.Year, monitored, show.Overview, show.PosterPath, show.ReleaseDate, section)
	if e != nil {
		return Item{}, false, e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return Item{}, false, e
	}
	item, e := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM media_items WHERE kind='series' AND tmdb_id=?", show.TMDbID))
	if e != nil {
		return Item{}, false, e
	}
	if n == 1 {
		if _, e = tx.ExecContext(ctx, `INSERT INTO series_details(media_id,series_status) VALUES(?,?)`, item.ID, show.SeriesStatus); e != nil {
			return Item{}, false, e
		}
		if e = putSeasons(ctx, tx, item.ID, show.Seasons); e != nil {
			return Item{}, false, e
		}
		if e = QueueMetadataRefreshTx(ctx, tx, item.ID, false); e != nil {
			return Item{}, false, e
		}
	}
	return item, n == 1, tx.Commit()
}
func bumpSeries(ctx context.Context, tx *sql.Tx, id, revision int64) error {
	r, e := tx.ExecContext(ctx, `UPDATE series_details SET revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE media_id=? AND revision=?`, id, revision)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		var exists int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM series_details WHERE media_id=?`, id).Scan(&exists); e != nil {
			return e
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
		return ErrRevision
	}
	return nil
}
func (s *Store) RefreshSeries(ctx context.Context, id int64, show metadata.Series, revision int64) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var tmdbID int64
	if e = tx.QueryRowContext(ctx, `SELECT tmdb_id FROM media_items WHERE id=? AND kind='series'`, id).Scan(&tmdbID); e != nil {
		return e
	}
	if tmdbID != show.TMDbID {
		return errors.New("series identity mismatch")
	}
	if e = bumpSeries(ctx, tx, id, revision); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_items SET title=?,year=?,overview=?,poster_path=?,release_date=? WHERE id=?`, show.Title, show.Year, show.Overview, show.PosterPath, show.ReleaseDate, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE series_details SET series_status=? WHERE media_id=?`, show.SeriesStatus, id); e != nil {
		return e
	}
	if e = putSeasons(ctx, tx, id, show.Seasons); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) SaveSeason(ctx context.Context, id int64, season metadata.Season, revision int64) error {
	return s.SaveSeasonAuthorized(ctx, id, season, revision, nil)
}

func (s *Store) SaveSeasonAuthorized(ctx context.Context, id int64, season metadata.Season, revision int64, authorize func(*sql.Tx) error) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if authorize != nil {
		if err := authorize(tx); err != nil {
			return err
		}
	}
	var seasonID, tmdbID int64
	if e = tx.QueryRowContext(ctx, `SELECT id,tmdb_id FROM seasons WHERE series_id=? AND number=?`, id, season.Number).Scan(&seasonID, &tmdbID); e != nil {
		return e
	}
	if tmdbID != season.TMDbID {
		return &metadata.Error{Code: "tmdb_invalid_response", Message: "This season's identity changed. Refresh the show before loading its episodes."}
	}
	if e = bumpSeries(ctx, tx, id, revision); e != nil {
		return e
	}
	for _, v := range season.Episodes {
		var prior int64
		err := tx.QueryRowContext(ctx, `SELECT tmdb_id FROM episodes WHERE season_id=? AND number=?`, seasonID, v.Number).Scan(&prior)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && prior != v.TMDbID {
			return &metadata.Error{Code: "tmdb_invalid_response", Message: "Episode identity changed. Saved episodes and file associations were retained for review."}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO episodes(season_id,tmdb_id,number,title,overview,air_date,runtime_minutes) VALUES(?,?,?,?,?,?,?) ON CONFLICT(season_id,number) DO UPDATE SET tmdb_id=excluded.tmdb_id,title=excluded.title,overview=excluded.overview,air_date=excluded.air_date,runtime_minutes=excluded.runtime_minutes`, seasonID, v.TMDbID, v.Number, v.Title, v.Overview, v.AirDate, v.RuntimeMinutes); e != nil {
			return e
		}
	}
	if _, e = tx.ExecContext(ctx, `UPDATE seasons SET title=?,overview=?,air_date=?,episode_count=?,loaded_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, season.Title, season.Overview, season.AirDate, len(season.Episodes), seasonID); e != nil {
		return e
	}
	return tx.Commit()
}

// Parent switches pause monitoring; they never overwrite individual selections.
func (s *Store) MonitorTV(ctx context.Context, id, revision int64, season *int, episode *int, monitored bool) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = bumpSeries(ctx, tx, id, revision); e != nil {
		return e
	}
	var result sql.Result
	if season == nil {
		result, e = tx.ExecContext(ctx, `UPDATE media_items SET monitored=? WHERE id=? AND kind='series'`, monitored, id)
	} else if episode == nil {
		result, e = tx.ExecContext(ctx, `UPDATE seasons SET monitored=? WHERE series_id=? AND number=?`, monitored, id, *season)
	} else {
		result, e = tx.ExecContext(ctx, `UPDATE episodes SET monitored=? WHERE number=? AND season_id=(SELECT id FROM seasons WHERE series_id=? AND number=?)`, monitored, *episode, id, *season)
	}
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// MonitorEpisodes updates only the explicitly selected episode IDs in one
// season, under the same series revision fence used by individual edits.
func (s *Store) MonitorEpisodes(ctx context.Context, id, revision int64, season int, episodeIDs []int64, monitored bool) error {
	if len(episodeIDs) < 1 || len(episodeIDs) > 100 || revision < 1 {
		return ErrMonitoringInput
	}
	seen := make(map[int64]bool, len(episodeIDs))
	for _, episodeID := range episodeIDs {
		if episodeID < 1 || seen[episodeID] {
			return ErrMonitoringInput
		}
		seen[episodeID] = true
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, episodeID := range episodeIDs {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE e.id=? AND s.series_id=? AND s.number=? AND s.loaded_at<>''`, episodeID, id, season).Scan(&n); e != nil {
			return e
		}
		if n != 1 {
			return sql.ErrNoRows
		}
	}
	if e = bumpSeries(ctx, tx, id, revision); e != nil {
		return e
	}
	for _, episodeID := range episodeIDs {
		if _, e = tx.ExecContext(ctx, `UPDATE episodes SET monitored=? WHERE id=?`, monitored, episodeID); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// monitorDatePredicate accepts only a complete civil date, matching the
// Calendar and scheduler rule. Dates are compared to SQLite's UTC today.
const monitorDatePredicate = `length(%s)=10 AND substr(%s,1,4) BETWEEN '0001' AND '9999' AND date(%s,'+0 days')=%s AND %s>=date('now','localtime')`

func monitorDateSQL(column string) string {
	return fmt.Sprintf(monitorDatePredicate, column, column, column, column, column)
}

// MonitorAllEpisodes turns monitoring on for the show and for regular episodes
// airing today or later (UTC), and explicitly unmonitors regular episodes and
// seasons with no episode releasing today or later. Special episodes keep their
// explicit choices. The whole change is one revision-fenced transaction.
func (s *Store) MonitorAllEpisodes(ctx context.Context, id, revision int64) error {
	if revision < 1 {
		return ErrMonitoringInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = bumpSeries(ctx, tx, id, revision); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_items SET monitored=1 WHERE id=? AND kind='series'`, id); e != nil {
		return e
	}
	seasonDate := monitorDateSQL("s.air_date")
	episodeDate := monitorDateSQL("e.air_date")
	// Clear regular items that have nothing releasing today or later. This is
	// what makes the action future-only even when a past episode was already
	// monitored.
	if _, e = tx.ExecContext(ctx, `UPDATE episodes AS e SET monitored=0 WHERE e.monitored=1 AND e.season_id IN (SELECT s.id FROM seasons s WHERE s.series_id=? AND s.number>0) AND NOT (`+episodeDate+`)`, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE seasons AS s SET monitored=0 WHERE s.series_id=? AND s.number>0 AND s.monitored=1 AND NOT ((`+seasonDate+`) OR EXISTS(SELECT 1 FROM episodes e WHERE e.season_id=s.id AND `+episodeDate+`))`, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE seasons AS s SET monitored=1 WHERE s.series_id=? AND s.number>0 AND s.monitored=0 AND ((`+seasonDate+`) OR EXISTS(SELECT 1 FROM episodes e WHERE e.season_id=s.id AND `+episodeDate+`))`, id); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE episodes AS e SET monitored=1 WHERE e.monitored=0 AND e.season_id IN (SELECT s.id FROM seasons s WHERE s.series_id=? AND s.number>0) AND `+episodeDate, id); e != nil {
		return e
	}
	return tx.Commit()
}

// MonitorAllShows applies MonitorAllEpisodes to every show in one scope. It is
// an explicit bulk owner action, so it does not require per-show revisions; the
// category is fixed to the three saved collections. As with the per-show action,
// only today's or future regular episodes and their seasons are monitored, and
// past or undated regular episodes are explicitly unmonitored.
func (s *Store) MonitorAllShows(ctx context.Context, category string) (int, error) {
	if category != "" && category != "standard" && category != "anime" {
		return 0, ErrMonitoringInput
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	scope := `SELECT id FROM media_items WHERE kind='series'`
	args := []any{}
	if category != "" {
		scope += ` AND category=?`
		args = append(args, category)
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+scope+`)`, args...).Scan(&count); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE media_items SET monitored=1 WHERE id IN (`+scope+`)`, args...); e != nil {
		return 0, e
	}
	seasonDate := monitorDateSQL("s.air_date")
	episodeDate := monitorDateSQL("e.air_date")
	if _, e = tx.ExecContext(ctx, `UPDATE episodes AS e SET monitored=0 WHERE e.monitored=1 AND e.season_id IN (SELECT s.id FROM seasons s WHERE s.number>0 AND s.series_id IN (`+scope+`)) AND NOT (`+episodeDate+`)`, args...); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE seasons AS s SET monitored=0 WHERE s.number>0 AND s.monitored=1 AND s.series_id IN (`+scope+`) AND NOT ((`+seasonDate+`) OR EXISTS(SELECT 1 FROM episodes e WHERE e.season_id=s.id AND `+episodeDate+`))`, args...); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE seasons AS s SET monitored=1 WHERE s.number>0 AND s.monitored=0 AND s.series_id IN (`+scope+`) AND ((`+seasonDate+`) OR EXISTS(SELECT 1 FROM episodes e WHERE e.season_id=s.id AND `+episodeDate+`))`, args...); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE episodes AS e SET monitored=1 WHERE e.monitored=0 AND e.season_id IN (SELECT s.id FROM seasons s WHERE s.number>0 AND s.series_id IN (`+scope+`)) AND `+episodeDate, args...); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE series_details SET revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE media_id IN (`+scope+`)`, args...); e != nil {
		return 0, e
	}
	return count, tx.Commit()
}

// MonitorSeasons changes explicit season choices without altering episode choices.
func (s *Store) MonitorSeasons(ctx context.Context, id, revision int64, seasonIDs []int64, monitored bool) error {
	if len(seasonIDs) < 1 || len(seasonIDs) > 100 || revision < 1 {
		return ErrMonitoringInput
	}
	seen := make(map[int64]bool, len(seasonIDs))
	for _, seasonID := range seasonIDs {
		if seasonID < 1 || seen[seasonID] {
			return ErrMonitoringInput
		}
		seen[seasonID] = true
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, seasonID := range seasonIDs {
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM seasons WHERE id=? AND series_id=?`, seasonID, id).Scan(&count); e != nil {
			return e
		}
		if count != 1 {
			return sql.ErrNoRows
		}
	}
	if e = bumpSeries(ctx, tx, id, revision); e != nil {
		return e
	}
	for _, seasonID := range seasonIDs {
		if _, e = tx.ExecContext(ctx, `UPDATE seasons SET monitored=? WHERE id=?`, monitored, seasonID); e != nil {
			return e
		}
	}
	return tx.Commit()
}
