package organization

import (
	"context"
	"os"
	"testing"
)

func TestOrganizationReviewRefreshesMissingHDRFact(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Original/movie.mkv", []byte("original video"))
	if _, e := f.db.Exec(`UPDATE media_files SET probe_json='{"codec":"h264","width":1920,"height":1080}' WHERE id=1`); e != nil {
		t.Fatal(e)
	}
	calls := 0
	f.s.SetProbe(func(context.Context, *os.File) (string, error) {
		calls++
		return `{"codec":"h264","width":1920,"height":1080,"hdr":true}`, nil
	})
	p, e := f.s.Preview(ctx, []int64{1}, "anime")
	if e != nil || len(p.Files) != 1 || p.Files[0].Snapshot.Quality != "1080p HDR" {
		t.Fatal(p, e)
	}
	if calls != 1 {
		t.Fatal("probe calls", calls)
	}
	var hdr int
	if e = f.db.QueryRow(`SELECT video_hdr FROM media_files WHERE id=1`).Scan(&hdr); e != nil || hdr != 1 {
		t.Fatal(hdr, e)
	}
	// The refreshed fact is reused rather than probed again.
	if _, e = f.s.Preview(ctx, []int64{1}, "anime"); e != nil {
		t.Fatal(e)
	}
	if calls != 1 {
		t.Fatal("probe repeated despite a stored HDR fact", calls)
	}
}

func TestOrganizationReviewDoesNotProbeUnverifiedFiles(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Original/movie.mkv", []byte("original video"))
	calls := 0
	f.s.SetProbe(func(context.Context, *os.File) (string, error) {
		calls++
		return `{"codec":"h264","width":1920,"height":1080,"hdr":false}`, nil
	})
	p, e := f.s.Preview(ctx, []int64{1}, "anime")
	if e != nil || len(p.Files) != 1 || p.Files[0].Snapshot.Quality != "" {
		t.Fatal(p, e)
	}
	if calls != 0 {
		t.Fatal("unverified file was probed", calls)
	}
}
