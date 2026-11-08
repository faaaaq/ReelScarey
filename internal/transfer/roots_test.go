package transfer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/organization"
)

func configureScanRoots(t *testing.T, f *workflowFixture, key string, roots []string) {
	t.Helper()
	config, err := organization.New(f.db, f.cfg).GetRoots(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = organization.New(f.db, f.cfg).UpdateRoots(context.Background(), organization.RootsUpdate{
		Revision:  config.Revision,
		Libraries: []organization.RootUpdate{{Key: key, Roots: roots}},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestVideoDiscoveryScansConfiguredRootsOrOneSelectedRoot(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	for _, folder := range []string{"Movies", "Movies 4K", "TV Shows", "Anime"} {
		if err := os.MkdirAll(filepath.Join(f.cfg.MediaDir, folder), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	existingVideo(t, f, "Movies/First Film (2026)/First.Film.2026.mp4", []byte("owned first root fixture"))
	existingVideo(t, f, "Movies 4K/Second Film (2026)/Second.Film.2026.mp4", []byte("owned second root fixture"))
	configureScanRoots(t, f, "movies", []string{"Movies", "Movies 4K"})

	scan, err := f.s.StartLibraryScan(ctx, "movie")
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Roots) != 2 {
		t.Fatal("scan did not snapshot both roots", scan)
	}
	all := finishScan(t, f, scan.ID)
	if all.Total != 2 {
		t.Fatal(all)
	}
	paths := map[string]bool{}
	for _, item := range all.Items {
		paths[item.Path] = true
	}
	if !paths["Movies/First Film (2026)/First.Film.2026.mp4"] || !paths["Movies 4K/Second Film (2026)/Second.Film.2026.mp4"] {
		t.Fatal(all)
	}

	one, err := f.s.StartLibraryScanRoots(ctx, "movie", "movies", "Movies 4K", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(one.Roots) != 1 || one.Roots[0] != "Movies 4K" {
		t.Fatal(one)
	}
	selected := finishScan(t, f, one.ID)
	if selected.Total != 1 || selected.Items[0].Path != "Movies 4K/Second Film (2026)/Second.Film.2026.mp4" {
		t.Fatal(selected)
	}
	if _, err = f.s.StartLibraryScanRoots(ctx, "movie", "movies", "Not Configured", false); err == nil {
		t.Fatal("unconfigured caller-supplied root accepted")
	}
	if _, err = f.s.StartLibraryScanRoots(ctx, "movie", "movies", "/Movies", false); err == nil {
		t.Fatal("absolute caller-supplied root accepted")
	}
}

func TestVideoDiscoveryFailsClosedWhenOneRootDisappears(t *testing.T) {
	f := setupWorkflow(t)
	ctx := context.Background()
	for _, folder := range []string{"Movies", "Movies 4K", "TV Shows", "Anime"} {
		if err := os.MkdirAll(filepath.Join(f.cfg.MediaDir, folder), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	existingVideo(t, f, "Movies/First Film (2026)/First.Film.2026.mp4", []byte("owned first root fixture"))
	config, err := organization.New(f.db, f.cfg).GetRoots(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = organization.New(f.db, f.cfg).UpdateRoots(ctx, organization.RootsUpdate{
		Revision:  config.Revision,
		Libraries: []organization.RootUpdate{{Key: "movies", Roots: []string{"Movies", "Movies 4K"}}},
	}); err != nil {
		t.Fatal(err)
	}
	scan, err := f.s.StartLibraryScan(ctx, "movie")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.RemoveAll(filepath.Join(f.cfg.MediaDir, "Movies 4K")); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if _, err = f.s.ProcessLibraryNext(ctx); err != nil {
			t.Fatal(err)
		}
		current, err := f.s.LibraryScan(ctx, scan.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.State == "completed" {
			t.Fatal("partial discovery was reported as complete")
		}
		if current.State == "failed" {
			if current.Message == "" {
				t.Fatal("failed scan has no message")
			}
			return
		}
	}
	t.Fatal("scan did not settle")
}
