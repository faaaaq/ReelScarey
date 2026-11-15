package youtube

import (
	"context"
	"errors"
	"fmt"
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

func TestJobsFiltersAndCounts(t *testing.T) {
	ctx := context.Background()
	db, e := database.Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	s := NewStore(db)
	c, e := s.CreateCollection(ctx, "XG")
	if e != nil {
		t.Fatal(e)
	}
	ids := []string{"abcdefghijk", "bcdefghijkl", "cdefghijklm"}
	for n, id := range ids {
		r, e := s.Preview(ctx, Video{ID: id, Title: fmt.Sprintf("Video %d", n), Channel: "XG"}, Target{Kind: "custom", CollectionID: c.ID})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.Queue(ctx, r.ID); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = db.Exec(`UPDATE youtube_jobs SET state='downloading' WHERE video_id='abcdefghijk'`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE youtube_jobs SET state='failed' WHERE video_id='cdefghijklm'`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE youtube_jobs SET series_source_id='src-1' WHERE video_id='bcdefghijkl'`); e != nil {
		t.Fatal(e)
	}
	items, counts, e := s.JobsFiltered(ctx, JobsFilter{})
	if e != nil || len(items) != 3 {
		t.Fatal(items, counts, e)
	}
	if counts.Queued != 1 || counts.Downloading != 1 || counts.Failed != 1 || counts.AwaitingReview != 0 || counts.Completed != 0 {
		t.Fatal("default counts are wrong", counts)
	}
	items, counts, e = s.JobsFiltered(ctx, JobsFilter{States: []string{"failed", "queued"}})
	if e != nil || len(items) != 2 {
		t.Fatal(items, counts, e)
	}
	if counts.Queued != 1 || counts.Failed != 1 || counts.Downloading != 0 {
		t.Fatal("state counts are wrong", counts)
	}
	items, _, e = s.JobsFiltered(ctx, JobsFilter{SeriesSourceID: "src-1"})
	if e != nil || len(items) != 1 || items[0].Video.ID != "bcdefghijkl" {
		t.Fatal(items, e)
	}
	items, counts, e = s.JobsFiltered(ctx, JobsFilter{States: []string{"queued"}, SeriesSourceID: "src-1"})
	if e != nil || len(items) != 1 || counts.Queued != 1 || counts.Failed != 0 {
		t.Fatal("filters must combine", items, counts, e)
	}
	if _, e = db.Exec(`UPDATE youtube_jobs SET state='completed' WHERE video_id='cdefghijklm'`); e != nil {
		t.Fatal(e)
	}
	items, counts, e = s.JobsFiltered(ctx, JobsFilter{})
	if e != nil || counts.Completed != 1 || counts.Failed != 0 {
		t.Fatal("completed counts are wrong", counts, e)
	}
	if _, _, e = s.JobsFiltered(ctx, JobsFilter{States: []string{"bogus"}}); !errors.Is(e, ErrInput) {
		t.Fatal("an unknown state must be refused", e)
	}
	// The unfiltered feed is unchanged.
	feed, e := s.Jobs(ctx)
	if e != nil || len(feed) != 3 {
		t.Fatal(feed, e)
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
