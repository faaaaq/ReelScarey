package automation

import (
	"context"
	"database/sql"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"testing"
	"time"
)

type refreshProvider struct {
	movie   metadata.Movie
	series  metadata.Series
	season  metadata.Season
	onMovie func()
	err     error
	calls   int
}

func (p *refreshProvider) Movie(context.Context, int64) (metadata.Movie, error) {
	p.calls++
	if p.onMovie != nil {
		p.onMovie()
	}
	return p.movie, p.err
}
func (p *refreshProvider) Series(context.Context, int64) (metadata.Series, error) {
	return p.series, p.err
}
func (p *refreshProvider) Season(context.Context, int64, int) (metadata.Season, error) {
	return p.season, p.err
}
func (p *refreshProvider) Information(context.Context, string, int64) (metadata.Information, error) {
	return metadata.Information{Tagline: "Fresh facts"}, p.err
}
func refreshFixture(t *testing.T) (*MetadataRefresher, *refreshProvider, *sql.DB, library.Item) {
	t.Helper()
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	year := 2020
	p := &refreshProvider{movie: metadata.Movie{TMDbID: 1, Title: "Fresh title", Year: &year, ReleaseDate: "2026-11-01", TheatricalReleaseDate: "2026-10-15", DigitalReleaseDate: "2026-11-01", PhysicalReleaseDate: "2026-12-01"}}
	item, _, e := library.New(db).AddMovie(ctx, metadata.Movie{TMDbID: 1, Title: "Old title", Year: &year, ReleaseDate: "2020-01-01"}, true)
	if e != nil {
		t.Fatal(e)
	}
	return NewMetadataRefresher(db, p), p, db, item
}
func TestScheduledMetadataRefreshIsPausedBoundedAndPreservesCatalogChoices(t *testing.T) {
	s, p, db, item := refreshFixture(t)
	ctx := context.Background()
	if worked, e := s.ProcessNext(ctx); e != nil || worked || p.calls != 0 {
		t.Fatal(worked, e, p.calls)
	}
	schedule, e := s.Get(ctx)
	if e != nil || schedule.Enabled {
		t.Fatal(schedule, e)
	}
	schedule.Enabled = true
	schedule.Batch = 1
	if _, e = s.Save(ctx, schedule); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Save(ctx, schedule); !errors.Is(e, ErrLease) {
		t.Fatal("stale schedule accepted", e)
	}
	if e = s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if worked, e := s.ProcessNext(ctx); e != nil || !worked {
		t.Fatal(worked, e)
	}
	got, e := library.New(db).Item(ctx, item.ID)
	if e != nil || got.Title != "Fresh title" || !got.Monitored || got.Status != item.Status || got.TMDbID != item.TMDbID ||
		got.TheatricalReleaseDate != "2026-10-15" || got.DigitalReleaseDate != "2026-11-01" || got.PhysicalReleaseDate != "2026-12-01" {
		t.Fatal(got, e)
	}
	var stamp int64
	if e = db.QueryRow(`SELECT last_success FROM metadata_refresh_state WHERE media_id=?`, item.ID).Scan(&stamp); e != nil || stamp <= 0 {
		t.Fatal(stamp, e)
	}
	calendar, e := library.New(db).Calendar(ctx, library.PlanningFilter{Kind: "movie", Month: "2026-11"}, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC))
	if e != nil || len(calendar.Items) != 1 || calendar.Items[0].MetadataUpdatedAt == 0 || calendar.Items[0].ReleaseKind != "digital" || calendar.Items[0].Date != "2026-11-01" {
		t.Fatal(calendar, e)
	}
}
func TestPauseDuringMetadataFetchFencesAllWritesAndUnavailableProviderWaits(t *testing.T) {
	s, p, db, item := refreshFixture(t)
	ctx := context.Background()
	p.onMovie = func() {
		v, e := s.Get(ctx)
		if e != nil {
			t.Fatal(e)
		}
		v.Enabled = false
		if _, e = s.Save(ctx, v); e != nil {
			t.Fatal(e)
		}
	}
	if e := s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	got, e := library.New(db).Item(ctx, item.ID)
	if e != nil || got.Title != "Old title" {
		t.Fatal("paused worker published stale metadata", got, e)
	}
	p.onMovie = nil
	p.err = metadata.ErrNotConfigured
	if e = s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	state, e := s.Get(ctx)
	if e != nil || state.State != "waiting_provider" || state.NextRun <= time.Now().Unix() {
		t.Fatal(state, e)
	}
}
func TestSeriesRefreshPreservesEpisodeIDsAndRejectsProviderReassignment(t *testing.T) {
	s, p, db, _ := refreshFixture(t)
	ctx := context.Background()
	p.series = metadata.Series{Movie: metadata.Movie{TMDbID: 2, Title: "Fresh show"}, SeriesStatus: "Returning Series", Seasons: []metadata.SeasonSummary{{TMDbID: 21, Number: 1, Title: "Season 1", AirDate: "2026-01-01", EpisodeCount: 1}}}
	store := library.New(db)
	item, _, e := store.AddSeries(ctx, p.series, true)
	if e != nil {
		t.Fatal(e)
	}
	show, e := store.TVShow(ctx, item.ID)
	if e != nil {
		t.Fatal(e)
	}
	p.season = metadata.Season{SeasonSummary: p.series.Seasons[0], Episodes: []metadata.Episode{{TMDbID: 22, Number: 1, Title: "Fresh episode", AirDate: "2026-11-02"}}}
	if e = store.SaveSeason(ctx, item.ID, p.season, show.Revision); e != nil {
		t.Fatal(e)
	}
	season, e := store.TVSeason(ctx, item.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	eid := season.Episodes[0].ID
	zero := 1
	if e = store.MonitorTV(ctx, item.ID, season.Revision, &zero, &zero, false); e != nil {
		t.Fatal(e)
	}
	if e = s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	season, e = store.TVSeason(ctx, item.ID, 1)
	if e != nil || season.Episodes[0].ID != eid || season.Episodes[0].Monitored {
		t.Fatal(season, e)
	}
	p.series.Title = "Should not publish"
	p.season.Episodes[0].TMDbID = 999
	if e = s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	got, e := store.Item(ctx, item.ID)
	if e != nil || got.Title == "Should not publish" {
		t.Fatal("provider reassignment was not atomic", got, e)
	}
}
