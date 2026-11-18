package organization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Stopping a running cleanup must halt the worker between videos, keep the
// completed work, and let the owner resume the remainder with a normal apply.
func TestTrackCleanupStopsBetweenVideosAndResumes(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/One/one.mkv", []byte("video one"))
	f.movie(t, 2, "Movies/Two/two.mkv", []byte("video two"))
	for _, stem := range []string{"Movies/One/one", "Movies/Two/two"} {
		for _, suffix := range []string{"eng.srt", "spa.srt", "spa.forced.srt"} {
			name := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(stem+"."+suffix))
			if err := os.WriteFile(name, []byte("sidecar"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.s.SetTrackCleaner(func(context.Context, *os.File) ([]TrackStream, error) { return nil, nil },
		func(context.Context, *os.File, *os.File, string, []int) (string, error) {
			t.Fatal("sidecar-only run attempted a remux")
			return "", nil
		})
	p, err := f.s.PreviewTrackCleanup(ctx, TrackCleanupRequest{Mode: "other_non_forced"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.TrackCleanup.Targets) != 2 {
		t.Fatalf("targets: %+v", p.TrackCleanup.Targets)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	// One tick processes exactly the first video.
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	// The owner stops the running job: a durable state change the worker
	// honors before claiming the next video.
	p, err = f.s.CancelQueued(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.State != "review" || p.TrackCleanup.Processed != 1 || p.TrackCleanup.Dropped != 1 {
		t.Fatalf("stopped plan: state=%s processed=%d dropped=%d", p.State, p.TrackCleanup.Processed, p.TrackCleanup.Dropped)
	}
	// The worker must not claim a stopped plan.
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	p, err = f.s.Plan(ctx, p.ID)
	if err != nil || p.TrackCleanup.Processed != 1 {
		t.Fatalf("worker continued after the stop: %+v %v", p.TrackCleanup, err)
	}
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, "Movies", "Two", "two.spa.srt")); err != nil {
		t.Fatal("the second video was touched before its turn", err)
	}
	// Resuming the review finishes the remainder.
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	p, err = f.s.Plan(ctx, p.ID)
	if err != nil || p.State != "completed" || p.TrackCleanup.Processed != 2 || p.TrackCleanup.Dropped != 2 {
		t.Fatalf("resumed cleanup: %+v %v", p, err)
	}
	for _, name := range []string{"Movies/One/one.spa.srt", "Movies/Two/two.spa.srt"} {
		if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(name))); !os.IsNotExist(err) {
			t.Fatalf("%s was retained: %v", name, err)
		}
	}
	for _, name := range []string{"Movies/One/one.eng.srt", "Movies/One/one.spa.forced.srt", "Movies/Two/two.eng.srt"} {
		if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
}
