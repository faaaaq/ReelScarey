package library

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"testing"
)

func TestTVChoicesSurviveRefreshPauseAndReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { db.Close() }()
	s := New(db)
	show := metadata.Series{Movie: metadata.Movie{TMDbID: 42, Title: "Test series"}, SeriesStatus: "Returning Series", Seasons: []metadata.SeasonSummary{{TMDbID: 43, Number: 0, Title: "Specials"}, {TMDbID: 44, Number: 1, Title: "Season 1", EpisodeCount: 2}}}
	// A film and series may share a provider ID without sharing catalog identity.
	movie, _, e := s.AddMovie(ctx, show.Movie, true)
	if e != nil {
		t.Fatal(e)
	}
	item, created, e := s.AddSeries(ctx, show, true)
	if e != nil || !created || item.ID == movie.ID {
		t.Fatal(item, created, e)
	}
	detail, e := s.TVShow(ctx, item.ID)
	if e != nil || len(detail.Seasons) != 2 || detail.Seasons[0].Monitored || !detail.Seasons[1].Monitored {
		t.Fatal(detail, e)
	}
	duplicate, created, e := s.AddSeries(ctx, show, false)
	if e != nil || created || !duplicate.Monitored {
		t.Fatal("duplicate changed selection", e)
	}
	season := metadata.Season{SeasonSummary: show.Seasons[1], Episodes: []metadata.Episode{{TMDbID: 45, Number: 1, Title: "Pilot"}, {TMDbID: 46, Number: 2, Title: "Second"}}}
	if e = s.SaveSeason(ctx, item.ID, season, detail.Revision); e != nil {
		t.Fatal(e)
	}
	detail, _ = s.TVShow(ctx, item.ID)
	number, episode := 1, 2
	if e = s.MonitorTV(ctx, item.ID, detail.Revision, &number, &episode, false); e != nil {
		t.Fatal(e)
	}
	if e = s.MonitorTV(ctx, item.ID, detail.Revision, nil, nil, false); !errors.Is(e, ErrRevision) {
		t.Fatal("stale edit accepted", e)
	}
	detail, _ = s.TVShow(ctx, item.ID)
	if e = s.MonitorTV(ctx, item.ID, detail.Revision, nil, nil, false); e != nil {
		t.Fatal(e)
	}
	saved, e := s.TVSeason(ctx, item.ID, 1)
	if e != nil || !saved.Episodes[0].Monitored || saved.Episodes[0].EffectiveMonitored || saved.Episodes[1].Monitored {
		t.Fatal("pause destroyed choices", saved, e)
	}
	detail, _ = s.TVShow(ctx, item.ID)
	season.Episodes[0].Title = "Updated pilot"
	season.Episodes = append(season.Episodes, metadata.Episode{TMDbID: 47, Number: 3, Title: "New episode"})
	if e = s.SaveSeason(ctx, item.ID, season, detail.Revision); e != nil {
		t.Fatal(e)
	}
	detail, _ = s.TVShow(ctx, item.ID)
	show.Seasons = append(show.Seasons, metadata.SeasonSummary{TMDbID: 48, Number: 2, Title: "Season 2"})
	if e = s.RefreshSeries(ctx, item.ID, show, detail.Revision); e != nil {
		t.Fatal(e)
	}
	detail, _ = s.TVShow(ctx, item.ID)
	if detail.Monitored || detail.Seasons[0].Monitored || len(detail.Seasons) != 3 {
		t.Fatal("refresh changed choices", detail)
	}
	if e = s.MonitorTV(ctx, item.ID, detail.Revision, nil, nil, true); e != nil {
		t.Fatal(e)
	}
	db.Close()
	db, e = database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	s = New(db)
	saved, e = s.TVSeason(ctx, item.ID, 1)
	if e != nil || len(saved.Episodes) != 3 || saved.Episodes[0].Title != "Updated pilot" || !saved.Episodes[0].EffectiveMonitored || saved.Episodes[1].Monitored || !saved.Episodes[2].Monitored {
		t.Fatal("choices or metadata lost", saved, e)
	}
	before := saved.Revision
	missing := 99
	if e = s.MonitorTV(ctx, item.ID, before, &number, &missing, true); !errors.Is(e, sql.ErrNoRows) {
		t.Fatal("missing episode accepted", e)
	}
	saved, _ = s.TVSeason(ctx, item.ID, 1)
	if saved.Revision != before {
		t.Fatal("failed edit changed revision")
	}
}
