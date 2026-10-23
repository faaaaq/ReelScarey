package maintenance

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/auth"
	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

func fixtureConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	return config.Config{ConfigDir: filepath.Join(root, "config"), MediaDir: filepath.Join(root, "media"), DownloadsDir: filepath.Join(root, "downloads")}
}
func TestBackupRestorePreservesAccountEncryptedProvidersAndPreferences(t *testing.T) {
	ctx := context.Background()
	cfg := fixtureConfig(t)
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a, e := auth.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := securestore.SecretFile(filepath.Join(cfg.ConfigDir, "setup-token"), 32, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Setup(ctx, "testadmin", "fixture-password-only", base64.RawURLEncoding.EncodeToString(raw)); e != nil {
		t.Fatal(e)
	}
	secrets, e := securestore.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	if e = secrets.SetNamed(ctx, "opensubtitles", []byte(`{"api_key":"fixture-private-key"}`)); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE settings SET library_name='Recovered cinema'; UPDATE subtitle_preferences SET enabled=1; UPDATE search_schedule SET enabled=1`); e != nil {
		t.Fatal(e)
	}
	root := filepath.Dir(cfg.ConfigDir)
	backup := filepath.Join(root, "snapshot")
	m, e := Backup(ctx, cfg, backup, "0.8.0-test")
	if e != nil {
		t.Fatal(e)
	}
	if m.Schema != 7 || len(m.Files) != 2 {
		t.Fatal(m)
	}
	if _, e = Verify(ctx, backup); e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(root, "recovered")
	if _, e = Restore(ctx, cfg, backup, dest); e != nil {
		t.Fatal(e)
	}
	restored, e := database.Open(ctx, dest)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	saved, e := securestore.New(ctx, restored, dest)
	if e != nil {
		t.Fatal(e)
	}
	b, e := saved.GetNamed(ctx, "opensubtitles")
	if e != nil || !bytes.Contains(b, []byte("fixture-private-key")) {
		t.Fatal("provider secret lost", e)
	}
	accounts, e := auth.New(ctx, restored, dest)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = accounts.Login(ctx, "testadmin", "fixture-password-only"); e != nil {
		t.Fatal("restored login failed", e)
	}
	var name string
	var enabled bool
	if e = restored.QueryRow(`SELECT library_name FROM settings`).Scan(&name); e != nil || name != "Recovered cinema" {
		t.Fatal(name, e)
	}
	if e = restored.QueryRow(`SELECT enabled FROM subtitle_preferences`).Scan(&enabled); e != nil || enabled {
		t.Fatal(enabled, e)
	}
	if !instance.RecoveryPending(dest) {
		t.Fatal("restored jobs were not held for review")
	}
	if e = restored.QueryRow(`SELECT enabled FROM search_schedule`).Scan(&enabled); e != nil || enabled {
		t.Fatal("restored schedule was not paused", e)
	}
	if _, e = Restore(ctx, cfg, backup, dest); e == nil {
		t.Fatal("existing config was replaced")
	}
	if _, e = Backup(ctx, cfg, backup, "test"); e == nil {
		t.Fatal("existing backup was replaced")
	}
	if instance.CheckStart(backup) == nil {
		t.Fatal("backup folder could run directly")
	}
	if e = instance.CheckStart(dest); e != nil {
		t.Fatal(e)
	}
	if _, e = Verify(ctx, backup); e != nil {
		t.Fatal("restore altered its source backup", e)
	}
	var sessions int
	// Login above creates one new session; all sessions copied from the backup
	// must have been removed before that login.
	if e = restored.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&sessions); e != nil || sessions != 1 {
		t.Fatal("restored old sessions survived", sessions, e)
	}
	manifest, _ := os.ReadFile(filepath.Join(backup, "backup-manifest.json"))
	if bytes.Contains(manifest, []byte("fixture-private-key")) {
		t.Fatal("secret in manifest")
	}
}
func TestBackupFailsClosedForMissingKeyLocksCorruptionAndUnsafeEntries(t *testing.T) {
	ctx := context.Background()
	cfg := fixtureConfig(t)
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	store, e := securestore.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	if e = store.SetNamed(ctx, "opensubtitles", []byte("fixture secret")); e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(filepath.Dir(cfg.ConfigDir), "backup")
	lock, e := instance.Acquire(cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Backup(ctx, cfg, dest, "test"); e == nil {
		t.Fatal("backup accepted a running server")
	}
	lock.Close()
	if _, e = Backup(ctx, cfg, dest, "test"); e != nil {
		t.Fatal(e)
	}
	key := filepath.Join(cfg.ConfigDir, "secrets.key")
	if e = os.Remove(key); e != nil {
		t.Fatal(e)
	}
	if _, e = Backup(ctx, cfg, dest+"-missing-key", "test"); e == nil {
		t.Fatal("backup silently replaced a missing key")
	}
	if _, e = os.Stat(key); !os.IsNotExist(e) {
		t.Fatal("missing key was recreated", e)
	}
	body, e := os.ReadFile(filepath.Join(dest, "secrets.key"))
	if e != nil {
		t.Fatal(e)
	}
	body[0] ^= 1
	if e = os.WriteFile(filepath.Join(dest, "secrets.key"), body, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Verify(ctx, dest); e == nil {
		t.Fatal("changed backup key accepted")
	}
	root, e := os.OpenRoot(dest)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	m := Manifest{Format: 1, Version: "test", Schema: 7, Files: []File{{Name: "../escaped.db", Size: 1, SHA: "00"}}}
	payload, _ := json.Marshal(m)
	if e = os.WriteFile(filepath.Join(dest, "backup-manifest.json"), payload, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadManifest(root); e == nil {
		t.Fatal("manifest path escape accepted")
	}
}
