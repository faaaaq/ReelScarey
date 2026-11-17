package transfer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Browser-picked import: the owner's native file picker supplies one video,
// which ReelScarey copies into the title's library folder and registers
// through the exact reviewed flow a discovered file uses. The copy is
// explicit, bounded, never overwrites anything and cleans up its staging
// file when it fails.

func TestDirectUploadPreparesAndCopiesIntoTitleFolder(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	destination, e := f.s.DirectUploadPrepare(ctx, "movie", "", f.movie.ID)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(destination, "Movies/") {
		t.Fatalf("the destination is not inside the configured root: %q", destination)
	}
	scan, e := f.s.DirectUpload(ctx, DirectUploadRequest{
		Kind:     "movie",
		MediaID:  f.movie.ID,
		Name:     "Test.Movie.2026.2160p.mkv",
		Reviewed: true,
	}, strings.NewReader("browser-picked fixture bytes"))
	if e != nil {
		t.Fatal(e)
	}
	if scan.State != "completed" || scan.Discovered != 1 || scan.Queued != 1 {
		t.Fatal("the uploaded copy was not queued for verification", scan)
	}
	if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
		t.Fatal(e)
	}
	page, e := f.s.DiscoveredFiles(ctx, scan.ID, 1, "")
	if e != nil {
		t.Fatal(e)
	}
	if page.Items[0].State != "registered" {
		t.Fatalf("the copy was not registered: %+v", page.Items[0])
	}
	relative := destination + "/Test.Movie.2026.2160p.mkv"
	body, e := os.ReadFile(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(relative)))
	if e != nil {
		t.Fatal(e)
	}
	if string(body) != "browser-picked fixture bytes" {
		t.Fatal("the copied bytes do not match the upload")
	}
	// No staging file is left behind.
	entries, e := os.ReadDir(filepath.Join(f.cfg.MediaDir, filepath.FromSlash(destination)))
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".uploading-") {
			t.Fatal("a staging file survived a successful upload", entry.Name())
		}
	}
	var origin string
	if e = f.db.QueryRow(`SELECT origin FROM media_files WHERE movie_id=?`, f.movie.ID).Scan(&origin); e != nil {
		t.Fatal(e)
	}
	if origin != "existing" {
		t.Fatal("the copy was not registered like an existing file", origin)
	}
}

func TestDirectUploadFailsClosedWithoutCopyingAnythingRegistered(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	destination, e := f.s.DirectUploadPrepare(ctx, "movie", "", f.movie.ID)
	if e != nil {
		t.Fatal(e)
	}
	name := existingVideo(t, f, destination+"/Test.Movie.2026.mp4", []byte("already on disk"))
	if _, e = f.s.DirectUpload(ctx, DirectUploadRequest{
		Kind:     "movie",
		MediaID:  f.movie.ID,
		Name:     "Test.Movie.2026.mp4",
		Reviewed: true,
	}, strings.NewReader("second copy")); e == nil {
		t.Fatal("an upload colliding with an existing file was accepted")
	}
	body, e := os.ReadFile(name)
	if e != nil {
		t.Fatal(e)
	}
	if string(body) != "already on disk" {
		t.Fatal("the collision check modified the existing file")
	}
	entries, e := os.ReadDir(filepath.Dir(name))
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 1 {
		t.Fatal("a staging file survived a refused upload", entries)
	}
	// The reviewed confirmation is mandatory even for a browser copy.
	if _, e = f.s.DirectUpload(ctx, DirectUploadRequest{
		Kind:    "movie",
		MediaID: f.movie.ID,
		Name:    "Test.Movie.2026.other.mkv",
	}, strings.NewReader("unreviewed")); e == nil {
		t.Fatal("an unreviewed upload was accepted")
	}
	// An empty body is refused without leaving anything behind.
	if _, e = f.s.DirectUpload(ctx, DirectUploadRequest{
		Kind:     "movie",
		MediaID:  f.movie.ID,
		Name:     "Test.Movie.2026.empty.mkv",
		Reviewed: true,
	}, strings.NewReader("")); e == nil {
		t.Fatal("an empty upload was accepted")
	}
	entries, e = os.ReadDir(filepath.Dir(name))
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) != 1 || entries[0].Name() != filepath.Base(name) {
		t.Fatal("refused uploads left files behind", entries)
	}
}

func TestDirectUploadKindMustMatchTheTitle(t *testing.T) {
	f, id, _ := setupTV(t)
	ctx := context.Background()
	if _, e := f.s.DirectUploadPrepare(ctx, "movie", "", id); e == nil {
		t.Fatal("a series title accepted a movie upload")
	}
	if _, e := f.s.DirectUpload(ctx, DirectUploadRequest{
		Kind:     "movie",
		MediaID:  id,
		Name:     "Test.Movie.2026.mkv",
		Reviewed: true,
	}, strings.NewReader("mismatched")); e == nil {
		t.Fatal("a series title accepted a movie upload")
	}
}
