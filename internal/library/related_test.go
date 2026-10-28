package library

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/database"
	"testing"
)

func TestRelatedTitlesAreExplicitSymmetricAndIdempotent(t *testing.T) {
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,status) VALUES(1,'movie',1,'Film','missing'),(2,'series',2,'Show','missing')`); e != nil {
		t.Fatal(e)
	}
	s := New(db)
	if e = s.LinkRelated(ctx, 1, 2); e != nil {
		t.Fatal(e)
	}
	if e = s.LinkRelated(ctx, 2, 1); e != nil {
		t.Fatal(e)
	}
	a, e := s.Related(ctx, 1)
	if e != nil || len(a) != 1 || a[0].Kind != "series" {
		t.Fatal(a, e)
	}
	b, e := s.Related(ctx, 2)
	if e != nil || len(b) != 1 || b[0].Kind != "movie" {
		t.Fatal(b, e)
	}
	if e = s.LinkRelated(ctx, 1, 1); e == nil {
		t.Fatal("self link accepted")
	}
	if e = s.UnlinkRelated(ctx, 2, 1); e != nil {
		t.Fatal(e)
	}
	a, e = s.Related(ctx, 1)
	if e != nil || len(a) != 0 {
		t.Fatal(a, e)
	}
}
