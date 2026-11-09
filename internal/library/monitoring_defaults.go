package library

import (
	"context"
	"time"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

// futureOrToday accepts only a complete civil date on or after today's date in
// the configured server timezone.
func futureOrToday(date string) bool {
	if len(date) != 10 {
		return false
	}
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil || parsed.Year() < 1800 {
		return false
	}
	today, err := time.Parse("2006-01-02", time.Now().Format("2006-01-02"))
	if err != nil {
		return false
	}
	return !parsed.Before(today)
}

// DefaultMovieMonitored is the default when a movie is added without an
// explicit monitoring choice: only a movie with a release today or later is
// monitored. A movie already out in full, or with no usable date, is paused.
func DefaultMovieMonitored(movie metadata.Movie) bool {
	for _, date := range []string{movie.ReleaseDate, movie.TheatricalReleaseDate, movie.DigitalReleaseDate, movie.PhysicalReleaseDate} {
		if futureOrToday(date) {
			return true
		}
	}
	return false
}

// DefaultSeriesMonitored is the default when a show is added without an
// explicit monitoring choice: only a show with a season or first-air date today
// or later is monitored. A fully aired show, or one with no usable date, is
// paused.
func DefaultSeriesMonitored(show metadata.Series) bool {
	if futureOrToday(show.ReleaseDate) {
		return true
	}
	for _, season := range show.Seasons {
		if futureOrToday(season.AirDate) {
			return true
		}
	}
	return false
}

// ApplyFutureOnlySeries turns the show on and monitors only regular episodes
// and seasons releasing today or later. It is used after a default add (no
// explicit monitoring choice) so an already-aired show does not start fully
// monitored.
func (s *Store) ApplyFutureOnlySeries(ctx context.Context, id int64) error {
	var revision int64
	if err := s.db.QueryRowContext(ctx, `SELECT revision FROM series_details WHERE media_id=?`, id).Scan(&revision); err != nil {
		return err
	}
	return s.MonitorAllEpisodes(ctx, id, revision)
}
