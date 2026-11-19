package organization

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/filecheck"
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
	var sha, raw string
	if err = f.db.QueryRow(`SELECT sha256,verification_json FROM media_files WHERE id=1`).Scan(&sha, &raw); err != nil {
		t.Fatal(err)
	}
	var observation filecheck.Snapshot
	if err = json.Unmarshal([]byte(raw), &observation); err != nil || sha != "" || observation.Sample == "" {
		t.Fatalf("bounded edit must invalidate the old full checksum and save a separate observation: %q %s %v", sha, raw, err)
	}
	if err := filecheck.Verify(ctx, mustOpenObservation(t, filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))), raw); err != nil {
		t.Fatal(err)
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

func mustOpenObservation(t *testing.T, path string) *os.File {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func TestTrackCleanupCanKeepFullChecksumVerification(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("registered video"))
	f.s.SetTrackCleaner(matroskaStubStreams, nil)
	f.s.SetMatroskaTrackDisabler(func(_ context.Context, path, _ string, _ []int, _ int64) (string, error) {
		return `{}`, os.WriteFile(path, []byte("edited video"), 0600)
	})
	p, err := f.s.PreviewTrackCleanup(ctx, TrackCleanupRequest{Mode: "all_subtitles", Action: "disable", Verification: "full"})
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
	var sha, raw string
	if err = f.db.QueryRow(`SELECT sha256,verification_json FROM media_files WHERE id=1`).Scan(&sha, &raw); err != nil {
		t.Fatal(err)
	}
	if len(sha) != 64 || raw != "{}" {
		t.Fatalf("strict verification lost full digest: %q %s", sha, raw)
	}
}

func TestBoundedJournalDetectsReplacementAndContentChange(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	name := "Movies/Film/film.mkv"
	f.movie(t, 1, name, []byte("original content"))
	root, _, err := f.s.openRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	original, err := inspect(root, name)
	if err != nil {
		t.Fatal(err)
	}
	input, err := root.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	observation, err := filecheck.Capture(ctx, input)
	input.Close()
	if err != nil {
		t.Fatal(err)
	}
	original.Observation = &observation
	if err = checkSource(ctx, root, original, true); err != nil {
		t.Fatal(err)
	}
	full := filepath.Join(f.cfg.MediaDir, filepath.FromSlash(name))
	if err = os.WriteFile(full, []byte("modified content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = checkSource(ctx, root, original, true); err == nil {
		t.Fatal("changed file accepted")
	}
	if err = os.Remove(full); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(full, []byte("original content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = checkSource(ctx, root, original, true); err == nil {
		t.Fatal("replaced file accepted")
	}
}

func TestProgressIsDurableWithoutChangingOwnerStop(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	_, err := f.db.Exec(`INSERT INTO organization_plans(id,state,snapshot,created_at,updated_at) VALUES('progress','review','{}',1,1)`)
	if err != nil {
		t.Fatal(err)
	}
	p := Plan{ID: "progress", TrackCleanup: &TrackCleanup{Targets: []cleanupTarget{{Path: "Movies/Film.mkv"}}}}
	ctx = f.s.withWorkProgress(ctx, &p)
	reportWork(ctx, "Reading full checksum", 100, 200)
	var state, raw string
	if err = f.db.QueryRow(`SELECT state,snapshot FROM organization_plans WHERE id='progress'`).Scan(&state, &raw); err != nil {
		t.Fatal(err)
	}
	var saved Plan
	if err = json.Unmarshal([]byte(raw), &saved); err != nil {
		t.Fatal(err)
	}
	if state != "review" || saved.Progress == nil || saved.Progress.BytesDone != 100 || saved.Progress.File != "Movies/Film.mkv" {
		t.Fatalf("progress %s %s", state, raw)
	}
}

func TestCleanupRetainsEverySkipBeyondTwenty(t *testing.T) {
	s := newReviewFixture(t).s
	p := Plan{TrackCleanup: &TrackCleanup{Targets: make([]cleanupTarget, 30)}}
	for i := range 30 {
		s.noteCleanupIssue(&p, cleanupTarget{ID: int64(i), Path: fmt.Sprintf("Movies/%d.mkv", i)}, problem("invalid_media", "Unreadable video"))
	}
	if p.TrackCleanup.Skipped != 30 || len(p.TrackCleanup.Examples) != 30 {
		t.Fatal(p.TrackCleanup)
	}
}
