package library

import (
	"context"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

type monitorFixture struct {
	s     *Store
	id    int64
	rev   int64
	dates map[string]string
}

func newMonitorFixture(t *testing.T) monitorFixture {
	t.Helper()
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Now().UTC()
	dates := map[string]string{
		"past":   now.AddDate(0, 0, -3).Format("2006-01-02"),
		"today":  now.Format("2006-01-02"),
		"future": now.AddDate(0, 0, 3).Format("2006-01-02"),
	}
	s := New(db)
	show := metadata.Series{
		Movie:        metadata.Movie{TMDbID: 900, Title: "Monitor fixture"},
		SeriesStatus: "Returning Series",
		Seasons: []metadata.SeasonSummary{
			{TMDbID: 901, Number: 0, Title: "Specials", AirDate: dates["past"]},
			{TMDbID: 902, Number: 1, Title: "Season 1", AirDate: dates["past"], EpisodeCount: 2},
			{TMDbID: 903, Number: 2, Title: "Season 2", AirDate: dates["today"], EpisodeCount: 3},
			{TMDbID: 904, Number: 3, Title: "Season 3", AirDate: dates["future"], EpisodeCount: 1},
		},
	}
	item, _, e := s.AddSeries(ctx, show, true)
	if e != nil {
		t.Fatal(e)
	}
	detail, e := s.TVShow(ctx, item.ID)
	if e != nil {
		t.Fatal(e)
	}
	f := monitorFixture{s: s, id: item.ID, rev: detail.Revision, dates: dates}
	f.loadSeason(t, show.Seasons[1], metadata.Episode{TMDbID: 910, Number: 1, Title: "S1E1", AirDate: dates["past"]}, metadata.Episode{TMDbID: 911, Number: 2, Title: "S1E2", AirDate: dates["past"]})
	f.loadSeason(t, show.Seasons[2],
		metadata.Episode{TMDbID: 920, Number: 1, Title: "S2E1", AirDate: dates["past"]},
		metadata.Episode{TMDbID: 921, Number: 2, Title: "S2E2", AirDate: dates["today"]},
		metadata.Episode{TMDbID: 922, Number: 3, Title: "S2E3", AirDate: dates["future"]},
	)
	f.loadSeason(t, show.Seasons[3], metadata.Episode{TMDbID: 930, Number: 1, Title: "S3E1", AirDate: dates["future"]})
	return f
}

func (f *monitorFixture) loadSeason(t *testing.T, summary metadata.SeasonSummary, episodes ...metadata.Episode) {
	t.Helper()
	season := metadata.Season{SeasonSummary: summary, Episodes: episodes}
	if e := f.s.SaveSeason(context.Background(), f.id, season, f.rev); e != nil {
		t.Fatal(e)
	}
	detail, e := f.s.TVShow(context.Background(), f.id)
	if e != nil {
		t.Fatal(e)
	}
	f.rev = detail.Revision
}

func (f monitorFixture) state(t *testing.T) (bool, map[int]int, map[string]bool) {
	t.Helper()
	var showMonitored int
	if e := f.s.db.QueryRow(`SELECT monitored FROM media_items WHERE id=?`, f.id).Scan(&showMonitored); e != nil {
		t.Fatal(e)
	}
	rows, e := f.s.db.Query(`SELECT number,monitored FROM seasons WHERE series_id=?`, f.id)
	if e != nil {
		t.Fatal(e)
	}
	seasons := map[int]int{}
	for rows.Next() {
		var number, monitored int
		if e = rows.Scan(&number, &monitored); e != nil {
			t.Fatal(e)
		}
		seasons[number] = monitored
	}
	rows.Close()
	rows, e = f.s.db.Query(`SELECT e.title,e.monitored FROM episodes e JOIN seasons s ON s.id=e.season_id WHERE s.series_id=?`, f.id)
	if e != nil {
		t.Fatal(e)
	}
	episodes := map[string]bool{}
	for rows.Next() {
		var title string
		var monitored int
		if e = rows.Scan(&title, &monitored); e != nil {
			t.Fatal(e)
		}
		episodes[title] = monitored == 1
	}
	rows.Close()
	return showMonitored == 1, seasons, episodes
}

func TestSeriesMonitorModesApplyOnePass(t *testing.T) {
	f := newMonitorFixture(t)
	ctx := context.Background()

	if e := f.s.SetSeriesMonitorMode(ctx, f.id, string(SeriesMonitorNone)); e != nil {
		t.Fatal(e)
	}
	show, seasons, episodes := f.state(t)
	if show || seasons[0] != 0 || seasons[1] != 0 || seasons[2] != 0 || seasons[3] != 0 {
		t.Fatal("none left monitoring on", show, seasons)
	}
	for title, monitored := range episodes {
		if monitored {
			t.Fatal("none left an episode monitored", title)
		}
	}

	if e := f.s.SetSeriesMonitorMode(ctx, f.id, string(SeriesMonitorMissing)); e != nil {
		t.Fatal(e)
	}
	show, seasons, episodes = f.state(t)
	if !show || seasons[0] != 0 || seasons[1] != 1 || seasons[2] != 1 || seasons[3] != 0 {
		t.Fatal("missing selected the wrong seasons", show, seasons)
	}
	if !episodes["S1E1"] || !episodes["S1E2"] || !episodes["S2E1"] || !episodes["S2E2"] || episodes["S2E3"] || episodes["S3E1"] {
		t.Fatal("missing selected the wrong episodes", episodes)
	}

	if e := f.s.SetSeriesMonitorMode(ctx, f.id, string(SeriesMonitorCurrent)); e != nil {
		t.Fatal(e)
	}
	show, seasons, episodes = f.state(t)
	if !show || seasons[0] != 0 || seasons[1] != 0 || seasons[2] != 1 || seasons[3] != 0 {
		t.Fatal("current selected the wrong seasons", show, seasons)
	}
	if !episodes["S2E1"] || !episodes["S2E2"] || !episodes["S2E3"] || episodes["S1E1"] || episodes["S3E1"] {
		t.Fatal("current selected the wrong episodes", episodes)
	}

	if e := f.s.SetSeriesMonitorMode(ctx, f.id, string(SeriesMonitorFuture)); e != nil {
		t.Fatal(e)
	}
	show, seasons, episodes = f.state(t)
	if !show || seasons[0] != 0 || seasons[1] != 0 || seasons[2] != 1 || seasons[3] != 1 {
		t.Fatal("future selected the wrong seasons", show, seasons)
	}
	if !episodes["S2E2"] || !episodes["S2E3"] || !episodes["S3E1"] || episodes["S1E1"] || episodes["S2E1"] {
		t.Fatal("future selected the wrong episodes", episodes)
	}

	if e := f.s.SetSeriesMonitorMode(ctx, f.id, "invalid"); e == nil {
		t.Fatal("invalid mode accepted")
	}
}
