package automation

import (
	"context"
	"errors"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"testing"
)

func TestSeriesRulesPersistAndRejectStaleUpdates(t *testing.T) {
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(1,'series',1,'Example','missing'),(2,'movie',2,'Movie','missing')`); e != nil {
		t.Fatal(e)
	}
	s := &Service{db: db}
	r, e := s.SeriesRules(ctx, 1)
	if e != nil || r.Revision != 0 {
		t.Fatal(r, e)
	}
	saved, e := s.SaveSeriesRules(ctx, 1, SeriesRules{Codec: "h265", Group: "MeGusta", Revision: 0})
	if e != nil || saved.Revision != 1 {
		t.Fatal(saved, e)
	}
	if _, e = s.SaveSeriesRules(ctx, 1, SeriesRules{Codec: "h264", Revision: 0}); e == nil {
		t.Fatal("stale write accepted")
	}
	if _, e = s.SaveSeriesRules(ctx, 1, SeriesRules{Codec: "h266", Revision: 1}); e == nil {
		t.Fatal("unknown codec accepted")
	}
	if _, e = s.SaveSeriesRules(ctx, 2, SeriesRules{}); e == nil {
		t.Fatal("movie rules accepted")
	}
	got, e := s.SeriesRules(ctx, 1)
	if e != nil || got.Codec != "h265" || got.Group != "MeGusta" || got.Revision != 1 {
		t.Fatal(got, e)
	}
}
func TestRuleChangeFencesQueuedAutomaticSelection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	_, e := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,monitored,release_date) VALUES(5,'series',5,'Show',2020,1,'2020-01-01');INSERT INTO series_details(media_id) VALUES(5);INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(1,5,10,1,'Season','','2020-01-01',1,1,'2020-01-01');INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,monitored,air_date) VALUES(1,1,11,1,'One','',1,'2020-01-01')`)
	if e != nil {
		t.Fatal(e)
	}
	configure(t, f.s, true, "download", 10)
	c, e := f.s.claim(ctx)
	if e != nil {
		t.Fatal(e)
	}
	targets, e := f.s.targets(ctx, "", 10, "", "")
	if e != nil || len(targets) == 0 || !targets[0].TV {
		t.Fatal(targets, e)
	}
	old := targets[0]
	profile, e := quality.New(f.db).Get(ctx, old.ProfileID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.SaveSeriesRules(ctx, 5, SeriesRules{Codec: "h265", Revision: 0}); e != nil {
		t.Fatal(e)
	}
	tx, e := f.db.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	e = f.s.guard(c, old, profile)(ctx, tx, transfer.Candidate{})
	if !errors.Is(e, ErrLease) {
		t.Fatal("changed rules did not fence selection", e)
	}
}
