package organization

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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
