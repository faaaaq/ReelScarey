package organization

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFolderInventoryCapturesExtrasAndRejectsSharedTitle(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	if err := os.MkdirAll(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "extras", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.cfg.MediaDir, "Movies", "Film", "extras", "poster.jpg"), []byte("art"), 0600); err != nil {
		t.Fatal(err)
	}
	root, _, err := f.s.openRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	got, err := inventoryFolder(context.Background(), root, f.db, "Movies/Film", 1)
	if err != nil || len(got.Files) != 2 || len(got.Directories) != 3 || got.Files[0].SHA == "" {
		t.Fatalf("incomplete inventory: %+v %v", got, err)
	}
	f.movie(t, 2, "Movies/Film/other.mkv", []byte("other"))
	if _, err = inventoryFolder(context.Background(), root, f.db, "Movies/Film", 1); err == nil {
		t.Fatal("shared registered folder was accepted")
	}
}

func TestFolderInventoryStaysBoundedWithLargeCatalog(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	// Thousands of registered rows belonging to other folders must not scale
	// the folder query: the SQL-side prefix bounds (RS-0016) keep the read
	// proportional to the reviewed folder, not the catalog.
	if _, err := f.db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title,year,status) VALUES(500,'movie',500,'Bulk catalog',2020,'available')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3000; i++ {
		if _, err := f.db.Exec(`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(?,?,?,1,'x','{}',1,'existing','bulk.mkv')`, 100+i, 100+i, "Movies/Other/bulk"+fmt.Sprint(i)+".mkv"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	root, _, err := f.s.openRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	got, err := inventoryFolder(context.Background(), root, f.db, "Movies/Film", 1)
	if err != nil || len(got.Files) != 1 {
		t.Fatalf("large catalog broke the folder inventory: %d files %v", len(got.Files), err)
	}
	// The plan must not be a whole-table scan: the prefix bounds must reach
	// the storage layer through the media_file_path_folded NOCASE index.
	var id, parent, notused int
	var detail string
	if err = f.db.QueryRow(`EXPLAIN QUERY PLAN SELECT relative_path FROM media_files WHERE relative_path>=? COLLATE NOCASE AND relative_path<? COLLATE NOCASE`, "movies/film/", "movies/film0").Scan(&id, &parent, &notused, &detail); err != nil {
		t.Fatal(err)
	}
	t.Logf("query plan: %s", detail)
	if !strings.Contains(detail, "media_file_path_folded") {
		t.Fatalf("prefix bounds did not use the folded-path index: %s", detail)
	}
}

func TestFolderInventoryRejectsSymlink(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	if err := os.Symlink(f.cfg.DownloadsDir, filepath.Join(f.cfg.MediaDir, "Movies", "Film", "outside")); err != nil {
		t.Skip("symlink unavailable:", err)
	}
	root, _, err := f.s.openRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if _, err = inventoryFolder(context.Background(), root, f.db, "Movies/Film", 1); err == nil {
		t.Fatal("symlink accepted")
	}
}

func TestTitleFolderStaysBelowCollectionAndRequiresOneFolder(t *testing.T) {
	c := Config{Libraries: defaults("Movies", "TV Shows")}
	got, err := titleFolder(c, "series", []string{"TV Shows/A Show/Season 01/one.mkv", "TV Shows/A Show/Season 02/two.mkv"})
	if err != nil || got != "TV Shows/A Show" {
		t.Fatalf("unexpected title folder %q: %v", got, err)
	}
	for _, files := range [][]string{
		{"Movies/loose.mkv"},
		{"Movies/First/one.mkv", "Movies/Second/two.mkv"},
		{"Unconfigured/First/one.mkv"},
	} {
		if _, err = titleFolder(c, "movie", files); err == nil {
			t.Fatalf("accepted unsafe scope: %q", files)
		}
	}
}

func TestFolderInventoryDetectsChangedContentAndDirectoryIdentity(t *testing.T) {
	f := newReviewFixture(t)
	f.movie(t, 1, "Movies/Film/film.mkv", []byte("video"))
	dir := filepath.Join(f.cfg.MediaDir, "Movies", "Film", "extras")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, _, err := f.s.openRoot("")
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	first, err := inventoryFolder(context.Background(), root, f.db, "Movies/Film", 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(dir, dir+"-old"); err != nil {
		t.Fatal(err)
	}
	if err = os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(dir + "-old"); err != nil {
		t.Fatal(err)
	}
	second, err := inventoryFolder(context.Background(), root, f.db, "Movies/Film", 1)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(first, second) {
		t.Fatal("directory replacement was not reflected in inventory")
	}
	if err = os.WriteFile(filepath.Join(dir, "poster.jpg"), []byte("art"), 0600); err != nil {
		t.Fatal(err)
	}
	third, err := inventoryFolder(context.Background(), root, f.db, "Movies/Film", 1)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(second, third) {
		t.Fatal("new file was not reflected in inventory")
	}
}
