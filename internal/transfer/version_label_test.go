package transfer

import (
	"context"
	"strings"
	"testing"
)

// The owner can correct a file's version label when inspection's derived
// resolution label claims the wrong edition (for example a 2019 cut registered
// under a 1994 movie). The correction is an explicit, per-file owner write;
// clearing it returns to the derived label.

func registerUpload(t *testing.T, f *workflowFixture, name string) string {
	t.Helper()
	ctx := context.Background()
	scan, e := f.s.DirectUpload(ctx, DirectUploadRequest{
		Kind:     "movie",
		MediaID:  f.movie.ID,
		Name:     name,
		Reviewed: true,
	}, strings.NewReader("version label fixture"))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	page, e := f.s.DiscoveredFiles(ctx, scan.ID, 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if page.Items[0].State != "registered" || page.Items[0].MediaFileID == nil {
		t.Fatalf("the file was not registered: %+v", page.Items[0])
	}
	return page.Items[0].Path
}

func TestFileVersionLabelOverrideAndClear(t *testing.T) {
	f := setupWorkflow(t)
	registerUpload(t, f, "Test.Movie.2026.2160p.mkv")
	details, e := f.s.Details(context.Background(), f.movie.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(details.Files) != 1 {
		t.Fatal("the registered file is missing", details.Files)
	}
	fileID := details.Files[0].ID
	if details.Files[0].VersionLabel != "1080p" || details.Files[0].VersionManual {
		t.Fatalf("the derived label is not the resolution: %+v", details.Files[0])
	}
	// The owner corrects the edition explicitly.
	if e = f.s.SetFileVersionLabel(context.Background(), fileID, "2019"); e != nil {
		t.Fatal(e)
	}
	details, e = f.s.Details(context.Background(), f.movie.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	if details.Files[0].VersionLabel != "2019" || !details.Files[0].VersionManual {
		t.Fatalf("the corrected label was not applied: %+v", details.Files[0])
	}
	// Clearing returns to the derived resolution label.
	if e = f.s.SetFileVersionLabel(context.Background(), fileID, ""); e != nil {
		t.Fatal(e)
	}
	details, e = f.s.Details(context.Background(), f.movie.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	if details.Files[0].VersionLabel != "1080p" || details.Files[0].VersionManual {
		t.Fatalf("the cleared label did not return to the derived value: %+v", details.Files[0])
	}
	// Labels are bounded and control-free.
	if e = f.s.SetFileVersionLabel(context.Background(), fileID, strings.Repeat("x", 81)); e == nil {
		t.Fatal("an oversized label was accepted")
	}
	if e = f.s.SetFileVersionLabel(context.Background(), fileID, "bad\nlabel"); e == nil {
		t.Fatal("a control-character label was accepted")
	}
	if e = f.s.SetFileVersionLabel(context.Background(), 999999, "nope"); e == nil {
		t.Fatal("an unknown file accepted a label")
	}
}

func TestFileVersionLabelIsMovieOnly(t *testing.T) {
	f, id, targets := setupTV(t)
	ctx := context.Background()
	existingVideo(t, f, "TV Shows/Test_ Show (2026)/Season 01/Test_ Show - S01E01 - Pilot.mp4", []byte("episode fixture"))
	scan, e := f.s.QueueDirectImport(ctx, DirectImportRequest{
		Kind:     "series",
		MediaID:  id,
		Episodes: []int64{targets[0].EpisodeID},
		Relative: "TV Shows/Test_ Show (2026)/Season 01/Test_ Show - S01E01 - Pilot.mp4",
		Reviewed: true,
	})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	page, e := f.s.DiscoveredFiles(ctx, scan.ID, 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if page.Items[0].State != "registered" || page.Items[0].MediaFileID == nil {
		t.Fatalf("the episode file was not registered: %+v", page.Items[0])
	}
	details, e := f.s.Details(ctx, id, 1)
	if e != nil {
		t.Fatal(e)
	}
	if len(details.Files) != 1 {
		t.Fatal("the episode file is missing", details.Files)
	}
	if e = f.s.SetFileVersionLabel(ctx, details.Files[0].ID, "2019"); e == nil {
		t.Fatal("a series file accepted a version label")
	}
}
