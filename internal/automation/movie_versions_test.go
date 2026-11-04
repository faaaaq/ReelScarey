package automation

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/quality"
	"github.com/faaaaq/ReelScarey/internal/transfer"
	"testing"
	"time"
)

type dualMovieIndexer struct{ onFetch func() }

func (*dualMovieIndexer) Test(context.Context, transfer.IndexerConfig) error { return nil }
func (*dualMovieIndexer) Search(context.Context, transfer.IndexerConfig, string) ([]transfer.Candidate, error) {
	n := 5
	return []transfer.Candidate{{Release: transfer.Release{Title: "A.Test.Movie.2020.1080p.English.WEB", Size: 2 << 30, Seeders: &n}}, {Release: transfer.Release{Title: "A.Test.Movie.2020.2160p.English.WEB", Size: 8 << 30, Seeders: &n}}}, nil
}
func (i *dualMovieIndexer) Fetch(_ context.Context, _ transfer.IndexerConfig, v transfer.Candidate) (transfer.Payload, string, error) {
	if i.onFetch != nil {
		i.onFetch()
	}
	h := fmt.Sprintf("%x", sha1.Sum([]byte(v.Title)))
	return transfer.Payload{Magnet: "magnet:?xt=urn:btih:" + h}, h, nil
}
func enableMovieVersions(t *testing.T, f *fixture) *dualMovieIndexer {
	t.Helper()
	ctx := context.Background()
	i := &dualMovieIndexer{}
	f.w = transfer.New(f.db, f.secrets, f.cfg, &client{}, i)
	f.s = New(f.db, f.w)
	if err := f.s.SetMovieVersions(ctx, 1, quality.MovieVersionPolicy{Enabled: true, HDProfileID: 1, UHDProfileID: 2}); err != nil {
		t.Fatal(err)
	}
	configure(t, f.s, false, "download", 10)
	return i
}
func TestMovieVersionsScheduleQueuesBothBandsIndependently(t *testing.T) {
	f := setup(t)
	enableMovieVersions(t, &f)
	ctx := context.Background()
	if worked, err := f.s.ProcessNext(ctx); worked || err != nil {
		t.Fatal("version opt-in started paused schedule", worked, err)
	}
	if err := f.s.Request(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	var total, hd, uhd int
	if err := f.db.QueryRow(`SELECT COUNT(*),SUM(movie_version='hd'),SUM(movie_version='uhd') FROM downloads`).Scan(&total, &hd, &uhd); err != nil || total != 2 || hd != 1 || uhd != 1 {
		t.Fatal(total, hd, uhd, err)
	}
	facts, err := f.s.MovieVersions(ctx, 1)
	if err != nil || facts.HDDownload != "queued" || facts.UHDDownload != "queued" {
		t.Fatal(facts, err)
	}
	if err = f.s.Request(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&total); err != nil || total != 2 {
		t.Fatal("duplicate acquisitions", total, err)
	}
}
func TestMovieVersionsExistingHDLeavesOnly4KWanted(t *testing.T) {
	f := setup(t)
	enableMovieVersions(t, &f)
	ctx := context.Background()
	for _, name := range []string{"Movies/HD.mkv", "Movies/HD-directors.mkv"} {
		if _, err := f.db.Exec(`INSERT INTO media_files(movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,?,10,'fixture','{"width":1920,"height":800}',1,'existing')`, name); err != nil {
			t.Fatal(err)
		}
	}
	page, err := library.New(f.db).Wanted(ctx, library.PlanningFilter{Kind: "movie", Page: 1, Status: "missing"}, time.Now())
	if err != nil || page.Total != 1 || page.Items[0].MovieVersion != "uhd" {
		t.Fatal(page, err)
	}
	if err = f.s.Request(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProcessNext(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	var version string
	if err = f.db.QueryRow(`SELECT COUNT(*),movie_version FROM downloads`).Scan(&count, &version); err != nil || count != 1 || version != "uhd" {
		t.Fatal(count, version, err)
	}
	p, err := f.s.MovieVersions(ctx, 1)
	if err != nil || p.HDFiles != 2 || p.UHDFiles != 0 {
		t.Fatal(p, err)
	}
}
func TestMovieVersionsPolicyAndProfileChangesFenceFetchedRelease(t *testing.T) {
	for _, mode := range []string{"policy", "profile", "monitoring"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			i := enableMovieVersions(t, &f)
			ctx := context.Background()
			i.onFetch = func() {
				var err error
				switch mode {
				case "policy":
					err = f.s.SetMovieVersions(ctx, 1, quality.MovieVersionPolicy{Enabled: false, HDProfileID: 1, UHDProfileID: 2, Revision: 1})
				case "profile":
					_, err = f.db.Exec(`UPDATE quality_profiles SET revision=revision+1 WHERE id=1`)
				case "monitoring":
					_, err = f.db.Exec(`UPDATE media_items SET monitored=0 WHERE id=1`)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := f.s.Request(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := f.s.ProcessNext(ctx); err != nil && !errors.Is(err, ErrLease) {
				t.Fatal(err)
			}
			var count int
			if err := f.db.QueryRow(`SELECT COUNT(*) FROM downloads`).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}
func TestMovieVersionsRejectStaleAndIncompatibleProfiles(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if err := f.s.SetMovieVersions(ctx, 1, quality.MovieVersionPolicy{Enabled: true, HDProfileID: 2, UHDProfileID: 1}); err == nil {
		t.Fatal("incompatible profile bands accepted")
	}
	enableMovieVersions(t, &f)
	if err := f.s.SetMovieVersions(ctx, 1, quality.MovieVersionPolicy{HDProfileID: 1, UHDProfileID: 2}); err == nil {
		t.Fatal("stale settings accepted")
	}
}
