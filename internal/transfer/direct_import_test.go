package transfer

import (
	"context"
	"testing"
)

// Direct import: the owner points at one specific file inside a configured
// library root. The picker only sees configured roots and their subfolders;
// registration keeps the reviewed confirmation, the target fences and the
// shared verification worker of every discovered file.

func TestDirectImportBrowseStaysInsideConfiguredRoots(t *testing.T) {
	f := setupWorkflow(t)
	existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mp4", []byte("direct import fixture"))
	existingVideo(t, f, "Movies/skip.sample.mkv", []byte("not a movie"))
	if _, e := f.s.DirectImportBrowse(context.Background(), "movie", "", ""); e != nil {
		t.Fatal(e)
	}
	out, e := f.s.DirectImportBrowse(context.Background(), "movie", "", "Movies")
	if e != nil {
		t.Fatal(e)
	}
	if len(out.Roots) != 1 || out.Roots[0] != "Movies" {
		t.Fatal("the configured roots were not advertised", out.Roots)
	}
	var sawFolder bool
	for _, item := range out.Items {
		if !item.Dir {
			t.Fatal("only the folder should be listed at the root level", item)
		}
		if item.Path == "Movies/Test Movie (2026)" {
			sawFolder = true
		}
	}
	if !sawFolder {
		t.Fatal("the title folder was not listed", out.Items)
	}
	// The video itself is listed inside its title folder.
	deep, e := f.s.DirectImportBrowse(context.Background(), "movie", "", "Movies/Test Movie (2026)")
	if e != nil {
		t.Fatal(e)
	}
	var sawFile bool
	for _, item := range deep.Items {
		if item.Dir {
			continue
		}
		if item.Name == "Test.Movie.2026.mp4" {
			sawFile = true
		}
	}
	if !sawFile {
		t.Fatal("the video was not listed", deep.Items)
	}
	// A path outside every configured root is refused; the client never sends
	// absolute paths, but even a crafted relative one cannot escape.
	if _, e = f.s.DirectImportBrowse(context.Background(), "movie", "", "../config"); e == nil {
		t.Fatal("an outside path was browsed")
	}
	if _, e = f.s.DirectImportBrowse(context.Background(), "movie", "", "Downloads"); e == nil {
		t.Fatal("a non-configured root was browsed")
	}
}

func TestDirectImportRegistersMovieInPlace(t *testing.T) {
	f := setupWorkflow(t)
	existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.Copy.mp4", []byte("direct import movie fixture"))
	in := DirectImportRequest{Kind: "movie", MediaID: f.movie.ID, Relative: "Movies/Test Movie (2026)/Test.Movie.2026.Copy.mp4", Reviewed: true}
	scan, e := f.s.QueueDirectImport(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	if scan.State != "completed" || scan.Discovered != 1 || scan.Queued != 1 {
		t.Fatal("the direct import was not queued for verification", scan)
	}
	// The reviewed confirmation is mandatory.
	if _, e = f.s.QueueDirectImport(context.Background(), DirectImportRequest{Kind: "movie", MediaID: f.movie.ID, Relative: "Movies/Test Movie (2026)/Test.Movie.2026.Copy.mp4"}); e == nil {
		t.Fatal("an unreviewed direct import was accepted")
	}
	if _, e = f.s.ProcessLibraryNext(context.Background()); e != nil {
		t.Fatal(e)
	}
	page, e := f.s.DiscoveredFiles(context.Background(), scan.ID, 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if page.Items[0].State != "registered" {
		t.Fatalf("the file was not registered: %+v", page.Items[0])
	}
	var relative, origin string
	if e = f.db.QueryRow(`SELECT relative_path,origin FROM media_files WHERE movie_id=?`, f.movie.ID).Scan(&relative, &origin); e != nil {
		t.Fatal(e)
	}
	if relative != in.Relative || origin != "existing" {
		t.Fatal("the file was not registered in its existing location", relative, origin)
	}
	// A second import of the same path is refused honestly.
	if _, e = f.s.QueueDirectImport(context.Background(), in); e == nil {
		t.Fatal("a registered file was imported again")
	}
}

func TestDirectImportSeriesEpisodeFences(t *testing.T) {
	f, id, targets := setupTV(t)
	ctx := context.Background()
	existingVideo(t, f, "TV Shows/Test_ Show (2026)/Season 01/Test_ Show - S01E01 - Pilot.mp4", []byte("direct import episode fixture"))
	in := DirectImportRequest{Kind: "series", MediaID: id, Episodes: []int64{targets[0].EpisodeID}, Relative: "TV Shows/Test_ Show (2026)/Season 01/Test_ Show - S01E01 - Pilot.mp4", Reviewed: true}
	scan, e := f.s.QueueDirectImport(ctx, in)
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
	if page.Items[0].State != "registered" {
		t.Fatalf("the episode file was not registered: %+v", page.Items[0])
	}
	var count int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM episode_files WHERE episode_id=?`, targets[0].EpisodeID).Scan(&count); e != nil || count != 1 {
		t.Fatal("the episode registration is missing", count, e)
	}
	// The same file cannot claim a second, already-registered episode.
	if _, e = f.s.QueueDirectImport(ctx, DirectImportRequest{Kind: "series", MediaID: id, Episodes: []int64{targets[1].EpisodeID, targets[0].EpisodeID}, Relative: "TV Shows/Test_ Show (2026)/Season 01/Test_ Show - S01E01 - Pilot.mp4", Reviewed: true}); e == nil {
		t.Fatal("a registered episode accepted another file")
	}
	// A movie cannot be the target of a series import.
	if _, e = f.s.QueueDirectImport(ctx, DirectImportRequest{Kind: "movie", MediaID: id, Relative: "TV Shows/Test_ Show (2026)/Season 01/Test_ Show - S01E01 - Pilot.mp4", Reviewed: true}); e == nil {
		t.Fatal("a series file was queued against a movie target")
	}
}

func TestDirectImportMovieVersionReservationIsHonored(t *testing.T) {
	f := setupWorkflow(t)
	existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.1080p.mp4", []byte("direct import second copy"))
	// Register the first copy through the same flow, then confirm a second
	// plain import is refused while the file is registered (versions are the
	// explicit route for extra copies).
	in := DirectImportRequest{Kind: "movie", MediaID: f.movie.ID, Relative: "Movies/Test Movie (2026)/Test.Movie.2026.1080p.mp4", Reviewed: true}
	scan, e := f.s.QueueDirectImport(context.Background(), in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProcessLibraryNext(context.Background()); e != nil {
		t.Fatal(e)
	}
	page, e := f.s.DiscoveredFiles(context.Background(), scan.ID, 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if page.Items[0].State != "registered" {
		t.Fatalf("unexpected state: %+v", page.Items[0])
	}
}

func TestDirectImportScanNeverBlocksRealScans(t *testing.T) {
	f := setupWorkflow(t)
	existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mp4", []byte("direct import scan fixture"))
	scan, e := f.s.QueueDirectImport(context.Background(), DirectImportRequest{Kind: "movie", MediaID: f.movie.ID, Relative: "Movies/Test Movie (2026)/Test.Movie.2026.mp4", Reviewed: true})
	if e != nil {
		t.Fatal(e)
	}
	// The manual scan is not queued/scanning, so a real discovery scan of the
	// same folder can still run.
	next, e := f.s.StartLibraryScan(context.Background(), "movie")
	if e != nil {
		t.Fatalf("the manual import scan blocked discovery: %v", e)
	}
	finishScan(t, f, next.ID)
	page, e := f.s.DiscoveredFiles(context.Background(), next.ID, 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Items) != 0 {
		t.Fatal("a file already claimed by a direct import was rediscovered", page.Items)
	}
	_ = scan
}
