package automation

import (
	"context"
	"database/sql"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
)

// A full provider response followed by a partial one must keep every
// previously valid fact: partial responses merge, they never erase.
func TestPartialMetadataRefreshPreservesExistingFacts(t *testing.T) {
	s, p, db, _ := refreshFixture(t)
	ctx := context.Background()
	store := library.New(db)

	// Movie: full refresh writes every field, partial refresh keeps them.
	p.movie = metadata.Movie{
		TMDbID: 1, Title: "Full title", Year: newInt(2020),
		Overview: "Full overview", PosterPath: "/poster.jpg",
		ReleaseDate: "2026-11-01", TheatricalReleaseDate: "2026-10-15",
		DigitalReleaseDate: "2026-11-01", PhysicalReleaseDate: "2026-12-01",
		RuntimeMinutes: newInt(104),
	}
	if e := s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}

	// Partial response: only the title arrives; every other field is blank.
	p.movie = metadata.Movie{TMDbID: 1, Title: "Renamed title"}
	if e := s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	got, e := store.Item(ctx, refreshItemID(t, db, 1))
	if e != nil {
		t.Fatal(e)
	}
	if got.Title != "Renamed title" {
		t.Fatalf("valid update lost: %q", got.Title)
	}
	if got.Overview != "Full overview" || got.PosterPath != "/poster.jpg" ||
		got.ReleaseDate != "2026-11-01" || got.TheatricalReleaseDate != "2026-10-15" ||
		got.DigitalReleaseDate != "2026-11-01" || got.PhysicalReleaseDate != "2026-12-01" ||
		got.Year == nil || *got.Year != 2020 || got.RuntimeMinutes == nil || *got.RuntimeMinutes != 104 {
		t.Fatalf("partial response erased stored facts: %+v", got)
	}

	// Series and episodes merge the same way.
	p.series = metadata.Series{
		Movie:        metadata.Movie{TMDbID: 2, Title: "Fresh show", Overview: "Show overview"},
		SeriesStatus: "Returning Series",
		Seasons:      []metadata.SeasonSummary{{TMDbID: 21, Number: 1, Title: "Season 1", AirDate: "2026-01-01", EpisodeCount: 1}},
	}
	p.movie = metadata.Movie{TMDbID: 2, Title: "Fresh show", Overview: "Show overview"}
	p.season = metadata.Season{SeasonSummary: p.series.Seasons[0], Episodes: []metadata.Episode{
		{TMDbID: 22, Number: 1, Title: "Pilot", Overview: "Pilot overview", AirDate: "2026-11-02", RuntimeMinutes: newInt(42)},
	}}
	item, _, e := store.AddSeries(ctx, p.series, true)
	if e != nil {
		t.Fatal(e)
	}
	show, e := store.TVShow(ctx, item.ID)
	if e != nil {
		t.Fatal(e)
	}
	if e := store.SaveSeason(ctx, item.ID, p.season, show.Revision); e != nil {
		t.Fatal(e)
	}
	if e := s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}

	// Partial season response: episode facts and season summary go blank and
	// the season listing disappears from the response entirely.
	p.series = metadata.Series{Movie: metadata.Movie{TMDbID: 2, Title: "Fresh show"}, SeriesStatus: ""}
	p.movie = metadata.Movie{TMDbID: 2, Title: "Fresh show"}
	p.season = metadata.Season{SeasonSummary: metadata.SeasonSummary{TMDbID: 21, Number: 1}, Episodes: []metadata.Episode{
		{TMDbID: 22, Number: 1},
	}}
	if e := s.Request(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ProcessNext(ctx); e != nil {
		t.Fatal(e)
	}
	season, e := store.TVSeason(ctx, item.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(season.Episodes) != 1 {
		t.Fatalf("missing provider entries must not be deleted: %+v", season.Episodes)
	}
	ep := season.Episodes[0]
	if ep.Title != "Pilot" || ep.Overview != "Pilot overview" || ep.AirDate != "2026-11-02" ||
		ep.RuntimeMinutes == nil || *ep.RuntimeMinutes != 42 {
		t.Fatalf("partial episode response erased stored facts: %+v", ep)
	}
	if season.Season.Title != "Season 1" || season.Season.AirDate != "2026-01-01" {
		t.Fatalf("partial season summary erased stored facts: %+v", season.Season)
	}
	got, e = store.Item(ctx, item.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Overview != "Show overview" {
		t.Fatalf("partial series response erased stored overview: %+v", got)
	}
	var status string
	if e := db.QueryRow(`SELECT series_status FROM series_details WHERE media_id=?`, item.ID).Scan(&status); e != nil {
		t.Fatal(e)
	}
	if status != "Returning Series" {
		t.Fatalf("partial response erased series status: %q", status)
	}
}

func refreshItemID(t *testing.T, db *sql.DB, tmdb int64) int64 {
	t.Helper()
	var id int64
	if e := db.QueryRow(`SELECT id FROM media_items WHERE tmdb_id=?`, tmdb).Scan(&id); e != nil {
		t.Fatal(e)
	}
	return id
}

func newInt(v int) *int { return &v }
