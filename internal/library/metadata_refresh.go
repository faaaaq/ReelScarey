package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"time"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type MetadataRefresh struct {
	Movie        metadata.Movie
	Series       *metadata.Series
	Seasons      []metadata.Season
	Information  metadata.Information
	SeasonCursor int
}

// ApplyMetadataRefresh atomically applies fetched facts only while the catalog
// snapshot and scheduling authorization still match. Monitoring, files and
// canonical IDs are retained. Missing provider entries are never deleted.
func (s *Store) ApplyMetadataRefresh(ctx context.Context, expected Item, revision int64, fresh MetadataRefresh, authorize func(*sql.Tx) error) error {
	if fresh.Movie.TMDbID != expected.TMDbID || (expected.Kind == "series" && (fresh.Series == nil || fresh.Series.TMDbID != expected.TMDbID)) {
		return ErrRevision
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = authorize(tx); e != nil {
		return e
	}
	current, e := scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM media_items WHERE id=?", expected.ID))
	if e != nil {
		return e
	}
	current.QualityProfile = nil
	expected.QualityProfile = nil
	// Availability can change while metadata is fetched without invalidating identity.
	current.Status = expected.Status
	if !reflect.DeepEqual(current, expected) {
		return ErrRevision
	}
	if expected.Kind == "series" {
		if e = bumpSeries(ctx, tx, expected.ID, revision); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE series_details SET series_status=? WHERE media_id=?`, fresh.Series.SeriesStatus, expected.ID); e != nil {
			return e
		}
		if e = putSeasons(ctx, tx, expected.ID, fresh.Series.Seasons); e != nil {
			return e
		}
		for _, season := range fresh.Seasons {
			var seasonID, providerID int64
			if e = tx.QueryRowContext(ctx, `SELECT id,tmdb_id FROM seasons WHERE series_id=? AND number=?`, expected.ID, season.Number).Scan(&seasonID, &providerID); e != nil {
				return e
			}
			if providerID != season.TMDbID {
				return ErrRevision
			}
			for _, ep := range season.Episodes {
				var prior int64
				e = tx.QueryRowContext(ctx, `SELECT tmdb_id FROM episodes WHERE season_id=? AND number=?`, seasonID, ep.Number).Scan(&prior)
				if e != nil && e != sql.ErrNoRows {
					return e
				}
				if e == nil && prior != ep.TMDbID {
					return ErrRevision
				}
				if _, e = tx.ExecContext(ctx, `INSERT INTO episodes(season_id,tmdb_id,number,title,overview,air_date,runtime_minutes) VALUES(?,?,?,?,?,?,?) ON CONFLICT(season_id,number) DO UPDATE SET title=excluded.title,overview=excluded.overview,air_date=excluded.air_date,runtime_minutes=excluded.runtime_minutes`, seasonID, ep.TMDbID, ep.Number, ep.Title, ep.Overview, ep.AirDate, ep.RuntimeMinutes); e != nil {
					return e
				}
			}
			if _, e = tx.ExecContext(ctx, `UPDATE seasons SET title=?,overview=?,air_date=?,episode_count=?,loaded_at=strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id=?`, season.Title, season.Overview, season.AirDate, len(season.Episodes), seasonID); e != nil {
				return e
			}
		}
	}
	m := fresh.Movie
	if _, e = tx.ExecContext(ctx, `UPDATE media_items SET title=?,year=?,overview=?,poster_path=?,release_date=?,theatrical_release_date=?,digital_release_date=?,physical_release_date=?,runtime_minutes=? WHERE id=?`, m.Title, m.Year, m.Overview, m.PosterPath, m.ReleaseDate, m.TheatricalReleaseDate, m.DigitalReleaseDate, m.PhysicalReleaseDate, m.RuntimeMinutes, expected.ID); e != nil {
		return e
	}
	payload, e := json.Marshal(fresh.Information)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO media_enrichment(media_id,payload,updated_at) VALUES(?,?,?) ON CONFLICT(media_id) DO UPDATE SET payload=excluded.payload,updated_at=excluded.updated_at`, expected.ID, string(payload), time.Now().Unix()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO metadata_refresh_state(media_id,last_success,season_cursor,state,message) VALUES(?,?,?,'ready','Metadata refreshed; monitoring and files preserved.') ON CONFLICT(media_id) DO UPDATE SET last_success=excluded.last_success,season_cursor=excluded.season_cursor,state=excluded.state,message=excluded.message`, expected.ID, time.Now().Unix(), fresh.SeasonCursor); e != nil {
		return e
	}
	return tx.Commit()
}
