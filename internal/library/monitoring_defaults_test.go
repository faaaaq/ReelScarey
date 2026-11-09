package library

import (
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/metadata"
)

func TestDefaultMonitoringUsesFutureReleaseDates(t *testing.T) {
	now := time.Now()
	today := now.Format("2006-01-02")
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	past := now.AddDate(0, 0, -1).Format("2006-01-02")

	if DefaultMovieMonitored(metadata.Movie{ReleaseDate: past}) {
		t.Fatal("past movie was monitored")
	}
	if !DefaultMovieMonitored(metadata.Movie{ReleaseDate: tomorrow}) {
		t.Fatal("future movie was not monitored")
	}
	if !DefaultMovieMonitored(metadata.Movie{DigitalReleaseDate: today}) {
		t.Fatal("today's digital release was not monitored")
	}
	if DefaultMovieMonitored(metadata.Movie{}) {
		t.Fatal("undated movie was monitored")
	}

	if DefaultSeriesMonitored(metadata.Series{Movie: metadata.Movie{ReleaseDate: past}, Seasons: []metadata.SeasonSummary{{Number: 1, AirDate: past}}}) {
		t.Fatal("fully aired show was monitored")
	}
	if !DefaultSeriesMonitored(metadata.Series{Movie: metadata.Movie{ReleaseDate: past}, Seasons: []metadata.SeasonSummary{{Number: 1, AirDate: past}, {Number: 2, AirDate: tomorrow}}}) {
		t.Fatal("show with a future season was not monitored")
	}
	if !DefaultSeriesMonitored(metadata.Series{Movie: metadata.Movie{ReleaseDate: tomorrow}}) {
		t.Fatal("show with a future first-air date was not monitored")
	}
	if DefaultSeriesMonitored(metadata.Series{}) {
		t.Fatal("undated show was monitored")
	}
}
