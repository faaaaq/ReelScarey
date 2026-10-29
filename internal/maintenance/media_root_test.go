//go:build linux

package maintenance

import (
	"context"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/operations"
	"github.com/faaaaq/ReelScarey/internal/securestore"
	"os"
	"path/filepath"
	"testing"
)

func rootFixture(t *testing.T) (config.Config, string) {
	t.Helper()
	cfg := fixtureConfig(t)
	cfg.BackupsDir = filepath.Join(filepath.Dir(cfg.ConfigDir), "backups")
	target := filepath.Join(filepath.Dir(cfg.ConfigDir), "target", "library")
	for _, dir := range []string{cfg.MediaDir, cfg.DownloadsDir, cfg.BackupsDir, filepath.Dir(target), filepath.Join(cfg.MediaDir, "Anime", "Title", "Extras"), filepath.Join(cfg.MediaDir, ".reelscarey-subtitle-history")} {
		if e := os.MkdirAll(dir, 0750); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(context.Background(), cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = securestore.New(context.Background(), db, cfg.ConfigDir); e != nil {
		t.Fatal(e)
	}
	db.Close()
	for name, body := range map[string]string{"Anime/Title/owned.mkv": "generated test media bytes", "Anime/Title/owned.ass": "[Events]\nDialogue: generated subtitle", ".reelscarey-subtitle-history/old.previous": "retained previous version"} {
		if e = os.WriteFile(filepath.Join(cfg.MediaDir, filepath.FromSlash(name)), []byte(body), 0640); e != nil {
			t.Fatal(e)
		}
	}
	return cfg, target
}
func TestMediaRootMigrationBacksUpCopiesResumesAndRequiresMountActivation(t *testing.T) {
	ctx := context.Background()
	cfg, dest := rootFixture(t)
	lock, e := instance.Acquire(cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = PlanMediaRoot(ctx, cfg, dest); e == nil {
		t.Fatal("live config accepted")
	}
	lock.Close()
	p, e := PlanMediaRoot(ctx, cfg, dest)
	if e != nil {
		t.Fatal(e)
	}
	if p.Status().Files != 3 || p.Status().Directories < 3 {
		t.Fatal(p.Status())
	}
	if e = instance.CheckStart(cfg.ConfigDir); e != nil {
		t.Fatal("review blocked startup", e)
	}
	if _, e = CopyMediaRoot(ctx, cfg, "stale", "test"); e == nil {
		t.Fatal("stale digest accepted")
	}
	p, e = CopyMediaRoot(ctx, cfg, p.Digest, "test")
	if e != nil {
		t.Fatal(e)
	}
	if p.State != "copied" || instance.CheckStart(cfg.ConfigDir) == nil {
		t.Fatal(p.State, "did not fence startup")
	}
	if _, e = Verify(ctx, p.Backup); e != nil {
		t.Fatal(e)
	}
	if _, e = ActivateMediaRoot(ctx, cfg, p.Digest); e == nil {
		t.Fatal("old mount activated")
	}
	// Simulate a process stopping after directory publication but before saving
	// the final phase, then another stop while a private scratch copy is partial.
	stage := filepath.Join(filepath.Dir(dest), ".reelscarey-root-copy-"+p.ID)
	if e = os.Rename(dest, stage); e != nil {
		t.Fatal(e)
	}
	p.State = "copying"
	if e = saveRootPlan(cfg, p); e != nil {
		t.Fatal(e)
	}
	unknown := filepath.Join(p.Status().Scratch, "interrupted-unknown")
	if e = os.WriteFile(unknown, []byte("partial owned copy retained"), 0600); e != nil {
		t.Fatal(e)
	}
	p, e = CopyMediaRoot(ctx, cfg, p.Digest, "test")
	if e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(unknown); e != nil || len(b) == 0 {
		t.Fatal("unrecognized scratch removed", e)
	}
	if _, e = os.Stat(filepath.Join(dest, "interrupted-unknown")); !os.IsNotExist(e) {
		t.Fatal("scratch entered library")
	}
	original := cfg.MediaDir + "-original"
	if e = os.Rename(cfg.MediaDir, original); e != nil {
		t.Fatal(e)
	}
	if e = os.Rename(dest, cfg.MediaDir); e != nil {
		t.Fatal(e)
	}
	p, e = ActivateMediaRoot(ctx, cfg, p.Digest)
	if e != nil {
		t.Fatal(e)
	}
	if p.State != "completed" || instance.CheckStart(cfg.ConfigDir) != nil || !instance.RecoveryPending(cfg.ConfigDir) {
		t.Fatal(p.State, "activation/recovery state")
	}
	db, e := database.OpenCurrent(ctx, filepath.Join(cfg.ConfigDir, "reelscarey.db"))
	if e != nil {
		t.Fatal(e)
	}
	if e = operations.New(db, cfg).Resume(ctx, true); e != nil {
		db.Close()
		t.Fatal("migration recovery could not resume", e)
	}
	db.Close()
	for _, entry := range p.Entries {
		if entry.Directory {
			continue
		}
		a, e := os.ReadFile(filepath.Join(original, filepath.FromSlash(entry.Path)))
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(filepath.Join(cfg.MediaDir, filepath.FromSlash(entry.Path)))
		if e != nil || string(a) != string(b) {
			t.Fatal("source changed", entry.Path, e)
		}
	}
	if _, e = ActivateMediaRoot(ctx, cfg, p.Digest); e != nil {
		t.Fatal("activation was not idempotent", e)
	}
	if instance.RecoveryPending(cfg.ConfigDir) {
		t.Fatal("repeated activation reintroduced recovery hold")
	}
}
func TestMediaRootMigrationPreservesForeignDestinationAndCancelsSafely(t *testing.T) {
	ctx := context.Background()
	cfg, dest := rootFixture(t)
	p, e := PlanMediaRoot(ctx, cfg, dest)
	if e != nil {
		t.Fatal(e)
	}
	os.Mkdir(dest, 0700)
	foreign := filepath.Join(dest, "foreign.txt")
	os.WriteFile(foreign, []byte("keep foreign file"), 0600)
	if _, e = CopyMediaRoot(ctx, cfg, p.Digest, "test"); e == nil {
		t.Fatal("foreign root accepted")
	}
	if b, _ := os.ReadFile(foreign); string(b) != "keep foreign file" {
		t.Fatal("foreign data changed")
	}
	if _, e = CancelMediaRoot(ctx, cfg, p.Digest); e != nil {
		t.Fatal(e)
	}
	if e = instance.CheckStart(cfg.ConfigDir); e != nil {
		t.Fatal(e)
	}
}
func TestMediaRootMigrationRejectsChangedSourcesLinksAndProtectedRoots(t *testing.T) {
	ctx := context.Background()
	cfg, dest := rootFixture(t)
	p, e := PlanMediaRoot(ctx, cfg, dest)
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(cfg.MediaDir, "Anime/Title/owned.mkv"), []byte("changed by owner"), 0640)
	if _, e = CopyMediaRoot(ctx, cfg, p.Digest, "test"); e == nil {
		t.Fatal("changed source accepted")
	}
	if _, e = CancelMediaRoot(ctx, cfg, p.Digest); e != nil {
		t.Fatal("changed original root could not cancel", e)
	}
	if _, e = PlanMediaRoot(ctx, cfg, filepath.Join(cfg.BackupsDir, "library")); e == nil {
		t.Fatal("backup overlap accepted")
	}
	os.Symlink(cfg.ConfigDir, filepath.Join(cfg.MediaDir, "private-link"))
	if _, e = PlanMediaRoot(ctx, cfg, dest); e == nil {
		t.Fatal("symlink copied")
	}
}
