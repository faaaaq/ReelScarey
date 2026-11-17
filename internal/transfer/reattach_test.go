package transfer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"github.com/faaaaq/ReelScarey/internal/organization"
)

// Re-attaching a registered file moves it between catalog titles and then
// queues the canonical move/rename through the reviewed organization plan.
// The file keeps its row id, observations and subtitle records; nothing is
// overwritten and the old title's status follows the loss honestly.

func addMovie(t *testing.T, f *workflowFixture, title string, year int) library.Item {
	t.Helper()
	ctx := context.Background()
	item, _, e := library.New(f.db).AddMovie(ctx, metadata.Movie{TMDbID: 4242, Title: title, Year: &year}, true)
	if e != nil {
		t.Fatal(e)
	}
	return item
}

func runOrganizationTick(t *testing.T, f *workflowFixture, planID string) organization.Plan {
	t.Helper()
	org := organization.New(f.db, f.cfg)
	for range 200 {
		plan, e := org.Plan(context.Background(), planID)
		if e != nil {
			t.Fatal(e)
		}
		if plan.State == "completed" || plan.State == "failed" {
			return plan
		}
		if e = org.Tick(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	t.Fatal("the organization plan never finished")
	return organization.Plan{}
}

func TestReattachMovesRegisteredFileBetweenTitles(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	registerUpload(t, f, "Test.Movie.2026.1080p.mkv")
	details, e := f.s.Details(ctx, f.movie.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	oldRelative := details.Files[0].Path[len(f.cfg.MediaDir)+1:]
	oldRelative = strings.ReplaceAll(oldRelative, "\\", "/")
	_, right := library.Item{}, addMovie(t, f, "Proper Entry", 2026) // The direct import refuses a registered file and names its owner.
	if _, e = f.s.QueueDirectImport(ctx, DirectImportRequest{Kind: "movie", MediaID: right.ID, Relative: oldRelative, Reviewed: true}); e == nil || !strings.Contains(e.Error(), "already registered to") {
		t.Fatalf("the direct import did not name the owning title: %v", e)
	}
	// Re-attach requires the reviewed confirmation.
	if _, e = f.s.ReattachFile(ctx, ReattachRequest{Kind: "movie", MediaID: right.ID, Relative: oldRelative}); e == nil {
		t.Fatal("an unreviewed re-attach was accepted")
	}
	plan, e := f.s.ReattachFile(ctx, ReattachRequest{Kind: "movie", MediaID: right.ID, Relative: oldRelative, Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	if plan.State != "queued" && plan.State != "review" {
		t.Fatalf("the move plan was not queued: %+v", plan)
	}
	if len(plan.Files) != 1 || plan.Files[0].Source != oldRelative {
		t.Fatalf("the plan does not move the re-attached file: %+v", plan.Files)
	}
	finished := runOrganizationTick(t, f, plan.ID)
	if finished.State != "completed" {
		t.Fatalf("the move failed: %+v", finished)
	}
	var owner int64
	var relative string
	if e = f.db.QueryRow(`SELECT movie_id,relative_path FROM media_files WHERE id=?`, details.Files[0].ID).Scan(&owner, &relative); e != nil {
		t.Fatal(e)
	}
	if owner != right.ID {
		t.Fatalf("the file is registered to the wrong title: %d", owner)
	}
	if relative != plan.Files[0].Destination {
		t.Fatalf("the registration does not point at the moved file: %q", relative)
	}
	body, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative)))
	if e != nil {
		t.Fatal(e)
	}
	if string(body) != "version label fixture" {
		t.Fatal("the moved bytes do not match the original file")
	}
	if _, e = os.Stat(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(oldRelative))); !os.IsNotExist(e) {
		t.Fatal("the old copy was left behind", e)
	}
	var status string
	if e = f.db.QueryRow(`SELECT status FROM media_items WHERE id=?`, f.movie.ID).Scan(&status); e != nil {
		t.Fatal(e)
	}
	if status != "missing" {
		t.Fatalf("the old title's status was not recalculated: %q", status)
	}
	if e = f.db.QueryRow(`SELECT status FROM media_items WHERE id=?`, right.ID).Scan(&status); e != nil {
		t.Fatal(e)
	}
	if status != "available" {
		t.Fatalf("the new title is not available: %q", status)
	}
	// Re-attaching to the title that already owns it is refused honestly.
	if _, e = f.s.ReattachFile(ctx, ReattachRequest{Kind: "movie", MediaID: right.ID, Relative: relative, Reviewed: true}); e == nil {
		t.Fatal("a re-attach to the owning title was accepted")
	}
}
