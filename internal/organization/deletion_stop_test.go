package organization

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The owner can stop a running permanent deletion: the plan returns to review,
// the files already removed stay removed, and the remaining files are left in
// place for a fresh apply to finish.
func TestCancelStopsRunningPermanentDeletion(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	const rel = "Movies/Gone/film.mkv"
	f.movie(t, 1, rel, []byte("film"))
	plan, err := f.s.PreviewDeletion(ctx, DeletionRequest{Selection: "titles", IDs: []int64{1}, Mode: "delete"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Removal == nil || plan.Removal.Mode != "delete" || plan.Removal.Selection == "" {
		t.Fatalf("reviewed deletion: %+v", plan.Removal)
	}
	// Simulate the worker mid-run: the plan is executing.
	if _, err = f.db.Exec(`UPDATE organization_plans SET state='running' WHERE id=?`, plan.ID); err != nil {
		t.Fatal(err)
	}
	stopped, err := f.s.CancelQueued(ctx, plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != "review" || !strings.Contains(stopped.Message, "Stopped by the owner") {
		t.Fatalf("stop did not return the plan to review: %s %q", stopped.State, stopped.Message)
	}
	// The stop changes no media: the reviewed file is untouched until a fresh
	// apply resumes the deletion.
	if _, err = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(rel))); err != nil {
		t.Fatal("a stopped deletion removed a file", err)
	}
	// The worker never claims a stopped plan.
	if err = f.s.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	var state string
	if err = f.db.QueryRow(`SELECT state FROM organization_plans WHERE id=?`, plan.ID).Scan(&state); err != nil || state != "review" {
		t.Fatalf("worker advanced a stopped plan: %s err=%v", state, err)
	}
}

// A running track cleanup and a running permanent deletion are stoppable; a
// running organization move is refused because it may already have moved files.
func TestCancelStopsOnlyInterruptibleRunningJobs(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/One/one.mkv", []byte("one"))
	f.movie(t, 2, "Movies/Two/two.mkv", []byte("two"))

	f.s.SetTrackCleaner(func(context.Context, *os.File) ([]TrackStream, error) { return nil, nil },
		func(context.Context, *os.File, *os.File, string, []int) (string, error) { return "", nil })
	cleanup, err := f.s.PreviewTrackCleanup(ctx, TrackCleanupRequest{Mode: "other_non_forced"})
	if err != nil {
		t.Fatal(err)
	}
	move, err := f.s.Preview(ctx, []int64{1}, "anime")
	if err != nil {
		t.Fatal(err)
	}
	// Only one work plan may be active at a time, so the two stop attempts run
	// in sequence.
	if _, err = f.db.Exec(`UPDATE organization_plans SET state='running' WHERE id=?`, cleanup.ID); err != nil {
		t.Fatal(err)
	}
	if p, err := f.s.CancelQueued(ctx, cleanup.ID); err != nil || p.State != "review" {
		t.Fatalf("cleanup stop: %+v err=%v", p.State, err)
	}
	if _, err = f.db.Exec(`UPDATE organization_plans SET state='running' WHERE id=?`, move.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.CancelQueued(ctx, move.ID); err == nil {
		t.Fatal("a running organization move was stopped")
	}
}
