package transfer

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/library"
	"github.com/faaaaq/ReelScarey/internal/metadata"
	"os"
	"path/filepath"
	"testing"
)

func TestBulkMatchSuggestionsAtomicReviewAndExistingFileVerification(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	name1 := existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mkv", []byte("first owned fixture"))
	year := 2025
	other, _, e := library.New(f.db).AddMovie(ctx, metadata.Movie{TMDbID: 9001, Title: "Other Movie", Year: &year}, false)
	if e != nil {
		t.Fatal(e)
	}
	name2 := existingVideo(t, f, "Movies/Other Movie (2025)/Other.Movie.2025.mkv", []byte("second owned fixture"))
	scan, e := f.s.StartLibraryScan(ctx, "movie")
	if e != nil {
		t.Fatal(e)
	}
	page := finishScan(t, f, scan.ID)
	if len(page.Items) != 2 {
		t.Fatal(page)
	}
	choices := []BulkMatchChoice{}
	for _, file := range page.Items {
		suggestions, e := f.s.LocalMatchSuggestions(ctx, page.Scan, file)
		if e != nil || len(suggestions.Candidates) == 0 {
			t.Fatal(suggestions, e)
		}
		mediaID := f.movie.ID
		if filepath.Base(file.Path) == "Other.Movie.2025.mkv" {
			mediaID = other.ID
		}
		choices = append(choices, BulkMatchChoice{FileID: file.ID, MediaID: mediaID, Episodes: []int64{}})
	}
	review, e := f.s.ReviewMatches(ctx, scan.ID, choices)
	if e != nil || len(review.Items) != 2 {
		t.Fatal(review, e)
	}
	var n int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM library_scan_files WHERE state='queued'`).Scan(&n); e != nil || n != 0 {
		t.Fatal("review queued work", n, e)
	}
	if e = f.s.ApplyMatches(ctx, scan.ID, review.ID, false); e == nil {
		t.Fatal("consent bypassed")
	}
	if e = f.s.ApplyMatches(ctx, scan.ID, review.ID, true); e != nil {
		t.Fatal(e)
	}
	if e = f.s.ApplyMatches(ctx, scan.ID, review.ID, true); e == nil {
		t.Fatal("review replayed")
	}
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM library_scan_files WHERE state='queued'`).Scan(&n); e != nil || n != 2 {
		t.Fatal("batch was not atomic", n, e)
	}
	for range 2 {
		if _, e = f.s.ProcessLibraryNext(ctx); e != nil {
			t.Fatal(e)
		}
	}
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE origin='existing'`).Scan(&n); e != nil || n != 2 {
		t.Fatal(n, e)
	}
	for _, name := range []string{name1, name2} {
		if _, e = os.Stat(name); e != nil {
			t.Fatal("existing file moved", e)
		}
	}
}
func TestBulkMatchStaleFileOrTargetRefusesEntireBatch(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	name := existingVideo(t, f, "Movies/Test Movie (2026)/Test.Movie.2026.mkv", []byte("owned fixture"))
	scan, e := f.s.StartLibraryScan(ctx, "movie")
	if e != nil {
		t.Fatal(e)
	}
	page := finishScan(t, f, scan.ID)
	choice := BulkMatchChoice{FileID: page.Items[0].ID, MediaID: f.movie.ID, Episodes: []int64{}}
	review, e := f.s.ReviewMatches(ctx, scan.ID, []BulkMatchChoice{choice})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.db.Exec(`UPDATE media_items SET title='Changed identity' WHERE id=?`, f.movie.ID); e != nil {
		t.Fatal(e)
	}
	if e = f.s.ApplyMatches(ctx, scan.ID, review.ID, true); e == nil {
		t.Fatal("changed identity accepted")
	}
	var n int
	if e = f.db.QueryRow(`SELECT COUNT(*) FROM library_scan_files WHERE state='queued'`).Scan(&n); e != nil || n != 0 {
		t.Fatal(n, e)
	}
	if _, e = f.db.Exec(`UPDATE media_items SET title='Test Movie' WHERE id=?`, f.movie.ID); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(name, []byte("replaced fixture bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = f.s.ApplyMatches(ctx, scan.ID, review.ID, true); e == nil {
		t.Fatal("changed file accepted")
	}
	if _, e = f.s.ReviewMatches(ctx, scan.ID, []BulkMatchChoice{choice, choice}); e == nil {
		t.Fatal("duplicate file accepted")
	}
}
