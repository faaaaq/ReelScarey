package organization

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func matroskaStubStreams(context.Context, *os.File) ([]TrackStream, error) {
	return []TrackStream{
		{Index: 1, Kind: "audio", Language: "eng"},
		{Index: 2, Kind: "audio", Language: "spa"},
		{Index: 3, Kind: "subtitle", Language: "eng"},
	}, nil
}

// Disabling hides Matroska tracks in place: the injected disabler runs on the
// reviewed stream indexes, no remux happens, external subtitle files are left
// in place, and the plan records the disabled tracks.
func TestTrackCleanupDisablesTracksInPlace(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	const rel = "Movies/Film/film.mkv"
	f.movie(t, 1, rel, []byte("registered video with tracks"))
	if _, err := f.db.Exec(`UPDATE media_items SET original_language='eng' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	sidecar := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "film.spa.srt")
	if err := os.WriteFile(sidecar, []byte("sidecar"), 0600); err != nil {
		t.Fatal(err)
	}
	f.s.SetTrackCleaner(matroskaStubStreams, func(context.Context, *os.File, *os.File, string, []int) (string, error) {
		t.Fatal("disable must not remux")
		return "", nil
	})
	var gotDrop []int
	f.s.SetMatroskaTrackDisabler(func(ctx context.Context, path, beforeProbe string, drop []int, beforeSize int64) (string, error) {
		gotDrop = append([]int(nil), drop...)
		if e := os.WriteFile(path, []byte("header edited in place"), 0600); e != nil {
			return "", e
		}
		return `{}`, nil
	})

	p, err := f.s.PreviewTrackCleanup(ctx, TrackCleanupRequest{Mode: "other_audio", Action: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	if p.TrackCleanup.Request.Action != "disable" || p.TrackCleanup.Permanent {
		t.Fatalf("request: %+v", p.TrackCleanup)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = f.s.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if len(gotDrop) != 1 || gotDrop[0] != 2 {
		t.Fatalf("disabler drop: %v", gotDrop)
	}
	p, err = f.s.Plan(ctx, p.ID)
	if err != nil || p.State != "completed" || p.TrackCleanup.Disabled != 1 || p.TrackCleanup.Dropped != 0 {
		t.Fatalf("plan: state=%s disabled=%d dropped=%d err=%v", p.State, p.TrackCleanup.Disabled, p.TrackCleanup.Dropped, err)
	}
	if _, err = os.Stat(sidecar); err != nil {
		t.Fatal("disable mode deleted an external subtitle file", err)
	}
	if got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))); err != nil || string(got) != "header edited in place" {
		t.Fatalf("file not edited in place: %q err=%v", got, err)
	}
}

// Disable is Matroska-only: a non-Matroska video is reported and left unchanged
// rather than rewritten by a tool that cannot hide tracks.
func TestTrackCleanupDisableSkipsNonMatroskaFiles(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	const rel = "Movies/Film/film.mp4"
	f.movie(t, 1, rel, []byte("registered mp4"))
	if _, err := f.db.Exec(`UPDATE media_items SET original_language='eng' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	f.s.SetTrackCleaner(matroskaStubStreams, func(context.Context, *os.File, *os.File, string, []int) (string, error) {
		t.Fatal("disable must not remux")
		return "", nil
	})
	called := false
	f.s.SetMatroskaTrackDisabler(func(context.Context, string, string, []int, int64) (string, error) {
		called = true
		return "", nil
	})
	p, err := f.s.PreviewTrackCleanup(ctx, TrackCleanupRequest{Mode: "other_audio", Action: "disable"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Apply(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err = f.s.Tick(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if called {
		t.Fatal("the disabler ran on a non-Matroska file")
	}
	p, err = f.s.Plan(ctx, p.ID)
	if err != nil || p.State != "completed" || p.TrackCleanup.Skipped != 1 || p.TrackCleanup.Disabled != 0 {
		t.Fatalf("plan: state=%s skipped=%d disabled=%d err=%v", p.State, p.TrackCleanup.Skipped, p.TrackCleanup.Disabled, err)
	}
	found := false
	for _, issue := range p.TrackCleanup.Examples {
		found = found || strings.Contains(issue, "Matroska")
	}
	if !found {
		t.Fatalf("expected a Matroska-scope issue, got %v", p.TrackCleanup.Examples)
	}
	if got, err := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))); err != nil || string(got) != "registered mp4" {
		t.Fatalf("the file changed: %q err=%v", got, err)
	}
}

func TestTrackCleanupRejectsAnUnknownAction(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	if _, err := f.s.PreviewTrackCleanup(ctx, TrackCleanupRequest{Mode: "all_subtitles", Action: "obliterate"}); err == nil {
		t.Fatal("unknown action accepted")
	}
}
