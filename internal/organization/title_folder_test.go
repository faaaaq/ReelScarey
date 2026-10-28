package organization

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateTitleFolderUsesNamingAndPreservesExistingFiles(t *testing.T) {
	f := newReviewFixture(t)
	ctx := context.Background()
	if _, e := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(1,'movie',123,'Test Movie',2020,'missing'),(2,'series',456,'Test Show',2021,'missing')`); e != nil {
		t.Fatal(e)
	}
	movie, e := f.s.CreateTitleFolder(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	want := filepath.Join(f.cfg.MediaDir, "Movies", "Test Movie (2020) [tmdb-123]")
	if movie != want {
		t.Fatalf("got %q want %q", movie, want)
	}
	file := filepath.Join(movie, "keep.txt")
	if e = os.WriteFile(file, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.CreateTitleFolder(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(file); e != nil || string(b) != "keep" {
		t.Fatal("existing file changed", e)
	}
	series, e := f.s.CreateTitleFolder(ctx, 2)
	if e != nil {
		t.Fatal(e)
	}
	if series != filepath.Join(f.cfg.MediaDir, "TV Shows", "Test Show (2021) [tmdb-456]") {
		t.Fatal(series)
	}
}
