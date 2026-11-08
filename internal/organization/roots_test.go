package organization

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
)

func rootByKey(t *testing.T, config RootsConfig, key string) LibraryRoots {
	t.Helper()
	for _, library := range config.Libraries {
		if library.Key == key {
			return library
		}
	}
	t.Fatalf("missing %s roots", key)
	return LibraryRoots{}
}

func TestScanRootsPersistAcrossRestartAndKeepDefaultsSeparate(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	for _, folder := range []string{"Movies", "Movies 4K", "TV Shows", "Anime", "Music", "Music Archive"} {
		if err := os.MkdirAll(filepath.Join(f.cfg.MediaDir, folder), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	defaults, err := f.s.GetRoots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	movies := rootByKey(t, defaults, "movies")
	if len(movies.Roots) != 1 || movies.Roots[0].Path != "Movies" || !movies.Roots[0].Available || movies.Configured {
		t.Fatal("unexpected default movie root", movies)
	}
	music := rootByKey(t, defaults, "music")
	if len(music.Roots) != 1 || music.Roots[0].Path != "Music" || !music.Roots[0].Available {
		t.Fatal("unexpected default music root", music)
	}
	updated, err := f.s.UpdateRoots(ctx, RootsUpdate{Revision: defaults.Revision, Libraries: []RootUpdate{
		{Key: "movies", Roots: []string{"Movies", "Movies 4K"}},
		{Key: "music", Roots: []string{"Music", "Music Archive"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := rootByKey(t, updated, "movies"); len(got.Roots) != 2 || !got.Configured {
		t.Fatal("movie roots were not saved", got)
	}

	// A restart reads the same persisted roots rather than reverting to defaults.
	f.db.Close()
	db, err := database.Open(ctx, f.cfg.ConfigDir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	reopened := New(db, f.cfg)
	after, err := reopened.GetRoots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := rootByKey(t, after, "music"); len(got.Roots) != 2 || got.Roots[1].Path != "Music Archive" {
		t.Fatal("music roots did not persist", got)
	}
	effective, err := reopened.EffectiveRoots(ctx, "movies", "movie")
	if err != nil || len(effective) != 2 || effective[1] != "Movies 4K" {
		t.Fatal(effective, err)
	}
}

func TestScanRootsRejectUnsafeOverlappingAndMissingFolders(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	for _, folder := range []string{"Movies", "Movies/4K", "TV Shows", "Anime", "Music"} {
		if err := os.MkdirAll(filepath.Join(f.cfg.MediaDir, folder), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	current, err := f.s.GetRoots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, roots := range [][]string{
		{"Movies", "Movies"},
		{"Movies", "Movies/4K"},
		{"/Movies"},
		{"../Movies"},
		{"Missing"},
		{"Movies:bad"},
	} {
		if _, err := f.s.UpdateRoots(ctx, RootsUpdate{Revision: current.Revision, Libraries: []RootUpdate{{Key: "movies", Roots: roots}}}); err == nil {
			t.Fatalf("unsafe roots accepted: %v", roots)
		}
	}
	// Roots cannot overlap another library's effective root either.
	if _, err := f.s.UpdateRoots(ctx, RootsUpdate{Revision: current.Revision, Libraries: []RootUpdate{{Key: "music", Roots: []string{"Movies/4K"}}}}); err == nil {
		t.Fatal("cross-library overlapping roots accepted")
	}
	// A stale revision cannot overwrite newer roots.
	if _, err := f.s.UpdateRoots(ctx, RootsUpdate{Revision: current.Revision, Libraries: []RootUpdate{{Key: "tv", Roots: []string{"TV Shows"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.UpdateRoots(ctx, RootsUpdate{Revision: current.Revision, Libraries: []RootUpdate{{Key: "tv", Roots: []string{"TV Shows"}}}}); err == nil {
		t.Fatal("stale roots revision accepted")
	}
}

func TestDefaultMusicRootResolvesExistingCaseOrReportsUnavailable(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	if err := os.MkdirAll(filepath.Join(f.cfg.MediaDir, "music"), 0o755); err != nil {
		t.Fatal(err)
	}
	config, err := f.s.GetRoots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	music := rootByKey(t, config, "music")
	if len(music.Roots) != 1 || music.Roots[0].Path != "music" || !music.Roots[0].Available {
		t.Fatal("lowercase music folder was not detected exactly", music)
	}
	effective, err := f.s.EffectiveRoots(ctx, "music", "music")
	if err != nil || len(effective) != 1 || effective[0] != "music" {
		t.Fatal(effective, err)
	}

	empty := newReviewFixture(t)
	missing, err := empty.s.GetRoots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	music = rootByKey(t, missing, "music")
	if len(music.Roots) != 1 || music.Roots[0].Path != "Music" || music.Roots[0].Available || music.Roots[0].Message == "" {
		t.Fatal("missing music folder must be shown as unavailable, not guessed", music)
	}
}

func TestScanRootsRejectSymbolicLinks(t *testing.T) {
	ctx := context.Background()
	f := newReviewFixture(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(f.cfg.MediaDir, "Movies"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(f.cfg.MediaDir, "Linked Music")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	current, err := f.s.GetRoots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.UpdateRoots(ctx, RootsUpdate{Revision: current.Revision, Libraries: []RootUpdate{{Key: "music", Roots: []string{"Linked Music"}}}}); err == nil {
		t.Fatal("symbolic-link root accepted")
	}
}
