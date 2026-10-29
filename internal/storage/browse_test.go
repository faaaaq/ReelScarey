package storage

import (
	"context"
	"fmt"
	"github.com/faaaaq/ReelScarey/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestBrowserMountedScopesPaginationAndLinks(t *testing.T) {
	base := t.TempDir()
	cfg := config.Config{MediaDir: filepath.Join(base, "media"), DownloadsDir: filepath.Join(base, "downloads"), ConfigDir: filepath.Join(base, "config"), BackupsDir: filepath.Join(base, "backups"), BrowseRoots: map[string]string{"archive": filepath.Join(base, "archive")}}
	for _, dir := range []string{cfg.MediaDir, cfg.DownloadsDir, cfg.ConfigDir, cfg.BackupsDir, cfg.BrowseRoots["archive"], filepath.Join(cfg.MediaDir, "Series")} {
		os.MkdirAll(dir, 0700)
	}
	for n := 0; n < 105; n++ {
		os.WriteFile(filepath.Join(cfg.MediaDir, fmt.Sprintf("file-%03d.mkv", n)), []byte("owned"), 0600)
	}
	os.WriteFile(filepath.Join(cfg.MediaDir, ".secret"), []byte("hidden"), 0600)
	out, e := Browse(context.Background(), cfg, "media", "", 1)
	if e != nil || out.Total != 106 || len(out.Items) != 100 || out.Items[0].Kind != "directory" {
		t.Fatal(out, e)
	}
	out, e = Browse(context.Background(), cfg, "media", "", 2)
	if e != nil || len(out.Items) != 6 {
		t.Fatal(out, e)
	}
	for _, item := range out.Items {
		if item.Name == ".secret" {
			t.Fatal("hidden item returned")
		}
	}
	for _, p := range []string{"../config", "/config", "Series/../..", "Series\\..", "."} {
		if _, e = Browse(context.Background(), cfg, "media", p, 1); e == nil {
			t.Fatal("traversal accepted", p)
		}
	}
	cfg.BrowseRoots["private"] = cfg.ConfigDir
	if _, e = Browse(context.Background(), cfg, "private", "", 1); e == nil {
		t.Fatal("private config browsed")
	}
	if e = os.Symlink(cfg.ConfigDir, filepath.Join(cfg.MediaDir, "link")); e == nil {
		if _, e = Browse(context.Background(), cfg, "media", "link", 1); e == nil {
			t.Fatal("symlink followed")
		}
	}
	if _, e = Browse(context.Background(), cfg, "archive", "", 1); e != nil {
		t.Fatal(e)
	}
	if b, _ := os.ReadFile(filepath.Join(cfg.MediaDir, "file-001.mkv")); string(b) != "owned" {
		t.Fatal("browser changed file")
	}
}
func TestBrowserBoundAndCancellation(t *testing.T) {
	dir := t.TempDir()
	for n := 0; n < 10001; n++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d", n)), nil, 0600)
	}
	cfg := config.Config{MediaDir: dir}
	if _, e := Browse(context.Background(), cfg, "media", "", 1); e != ErrLimit {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := Browse(ctx, cfg, "media", "", 1); e != context.Canceled {
		t.Fatal(e)
	}
}
