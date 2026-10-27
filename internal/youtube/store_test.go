package youtube

import (
	"context"
	"errors"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func TestReviewQueueAndReopen(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, e := database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	s := NewStore(db)
	c, e := s.CreateCollection(ctx, "XG Days")
	if e != nil {
		t.Fatal(e)
	}
	v := Video{ID: "abcdefghijk", Title: "Episode one", Channel: "XG"}
	target := Target{Kind: "custom", CollectionID: c.ID}
	r, e := s.Preview(ctx, v, target)
	if e != nil {
		t.Fatal(e)
	}
	j, e := s.Queue(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Queue(ctx, r.ID); !errors.Is(e, ErrChanged) {
		t.Fatalf("replay: %v", e)
	}
	r2, e := s.Preview(ctx, v, target)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Queue(ctx, r2.ID); !errors.Is(e, ErrBusy) {
		t.Fatalf("duplicate: %v", e)
	}
	db.Close()
	db, e = database.Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	j2, e := NewStore(db).Job(ctx, j.ID)
	if e != nil || j2.State != "queued" || j2.Video != v || j2.Target != target {
		t.Fatalf("reopen: %+v %v", j2, e)
	}
}

func TestStaleAndUnavailableTargets(t *testing.T) {
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := NewStore(db)
	c, e := s.CreateCollection(ctx, "Xtra XG")
	if e != nil {
		t.Fatal(e)
	}
	v := Video{ID: "abcdefghijk", Title: "One"}
	r, e := s.Preview(ctx, v, Target{Kind: "custom", CollectionID: c.ID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE youtube_collections SET title='changed',revision=revision+1 WHERE id=?`, c.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Queue(ctx, r.ID); !errors.Is(e, ErrChanged) {
		t.Fatalf("stale target: %v", e)
	}
	if _, e = s.Preview(ctx, Video{ID: "../not-valid", Title: "x"}, Target{Kind: "custom", CollectionID: c.ID}); !errors.Is(e, ErrInput) {
		t.Fatalf("bad ID: %v", e)
	}
	if _, e = s.Preview(ctx, v, Target{Kind: "movie", MediaID: 999}); !errors.Is(e, ErrChanged) {
		t.Fatalf("missing target: %v", e)
	}
}

func TestCancelReleasesCustomVideoReservation(t *testing.T) {
	ctx := context.Background()
	db, err := database.Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := NewStore(db)
	c, err := s.CreateCollection(ctx, "Generated")
	if err != nil {
		t.Fatal(err)
	}
	v := Video{ID: "abcdefghijk", Title: "Generated"}
	target := Target{Kind: "custom", CollectionID: c.ID}
	r, err := s.Preview(ctx, v, target)
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Queue(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Cancel(ctx, j.ID); err != nil {
		t.Fatal(err)
	}
	r, err = s.Preview(ctx, v, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Queue(ctx, r.ID); err != nil {
		t.Fatalf("cancelled reservation remained: %v", err)
	}
}
