package operations

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func fixture(t *testing.T) (*Service, *sql.DB, config.Config) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(root, "config"), MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads")}
	for _, dir := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.Mkdir(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	db, e := database.Open(context.Background(), cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, cfg), db, cfg
}
func TestChecksUsePrivateFilesAndLeaveDownloadContentsUnchanged(t *testing.T) {
	s, _, cfg := fixture(t)
	ctx := context.Background()
	for _, dir := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		if e := os.WriteFile(filepath.Join(dir, "owner-file"), []byte("keep unchanged"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if s.Last() != nil {
		t.Fatal("checks ran without explicit request")
	}
	report, e := s.Run(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, c := range report.Checks {
		if c.ID != "ffprobe" && c.State != "ready" {
			t.Fatalf("%s: %s", c.ID, c.Message)
		}
	}
	for _, dir := range []string{cfg.MediaDir, cfg.DownloadsDir} {
		entries, e := os.ReadDir(dir)
		if e != nil || len(entries) != 1 {
			t.Fatal("probe was not cleaned", entries, e)
		}
		body, e := os.ReadFile(filepath.Join(dir, "owner-file"))
		if e != nil || string(body) != "keep unchanged" {
			t.Fatal("existing file changed", e)
		}
	}
	if report.Resources.HeapBytes == 0 || report.Resources.CPUs == 0 || s.Last() == nil {
		t.Fatal("missing current resources")
	}
	s.mu.Lock()
	_, e = s.Run(ctx)
	s.mu.Unlock()
	if e != ErrBusy {
		t.Fatal("concurrent check accepted", e)
	}
}
func TestRecoveryRequiresReviewValidMarkerAndKeepsSchedulesPaused(t *testing.T) {
	s, db, cfg := fixture(t)
	ctx := context.Background()
	marker := filepath.Join(cfg.ConfigDir, "recovery-mode")
	if e := os.WriteFile(marker, []byte(`{"format":1}`), 0600); e != nil {
		t.Fatal(e)
	}
	if s.Resume(ctx, false) == nil || !instance.RecoveryPending(cfg.ConfigDir) {
		t.Fatal("unreviewed recovery resumed")
	}
	if e := s.Resume(ctx, true); e != nil {
		t.Fatal(e)
	}
	if instance.RecoveryPending(cfg.ConfigDir) {
		t.Fatal("reviewed recovery stayed held")
	}
	if e := os.WriteFile(marker, []byte(`{"format":1}`), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Remove(cfg.DownloadsDir); e != nil {
		t.Fatal(e)
	}
	if s.Resume(ctx, true) == nil || !instance.RecoveryPending(cfg.ConfigDir) {
		t.Fatal("unresolved storage allowed recovery")
	}
	if e := os.Mkdir(cfg.DownloadsDir, 0700); e != nil {
		t.Fatal(e)
	}
	var enabled bool
	for _, table := range []string{"search_schedule", "subtitle_preferences"} {
		if e := db.QueryRow("SELECT enabled FROM " + table).Scan(&enabled); e != nil || enabled {
			t.Fatal("schedule enabled during resume", e)
		}
	}
	if e := os.WriteFile(marker, []byte(`{"format":1,"unexpected":true}`), 0600); e != nil {
		t.Fatal(e)
	}
	if s.Resume(ctx, true) == nil || !instance.RecoveryPending(cfg.ConfigDir) {
		t.Fatal("invalid recovery marker accepted")
	}
}
func TestMissingCredentialsAndOverlappingRootsFailClosed(t *testing.T) {
	s, db, cfg := fixture(t)
	ctx := context.Background()
	store, e := securestore.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.SetNamed(ctx, "opensubtitles", []byte("fixture-private-secret")); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(filepath.Join(cfg.ConfigDir, "secrets.key")); e != nil {
		t.Fatal(e)
	}
	s.cfg.MediaDir = cfg.DownloadsDir
	report, e := s.Run(ctx)
	if e != nil {
		t.Fatal(e)
	}
	found := map[string]string{}
	for _, c := range report.Checks {
		found[c.ID] = c.State
	}
	if found["secrets"] != "attention" || found["paths"] != "attention" {
		t.Fatal(found)
	}
	if _, e = os.Stat(filepath.Join(cfg.ConfigDir, "secrets.key")); !os.IsNotExist(e) {
		t.Fatal("missing key recreated")
	}
	body, _ := json.Marshal(report)
	if strings.Contains(string(body), "fixture-private-secret") {
		t.Fatal("secret exposed in report")
	}
	guide, e := s.Guide(ctx)
	if e != nil {
		t.Fatal(e)
	}
	for _, step := range guide.Steps {
		if step.ID == "torznab" && (!step.Optional || step.State != "optional") {
			t.Fatal("missing indexer blocks setup")
		}
	}
}
