package maintenance

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	clientID := strings.Repeat("a", 32)
	if _, e = db.Exec(`INSERT INTO download_clients(id,name,kind,enabled,created_at,updated_at) VALUES(?,'Owned NZBGet','nzbget',1,1,1); UPDATE download_client_preferences SET usenet=?`, clientID, clientID); e != nil {
		t.Fatal(e)
	}
	clientConfig := []byte(`{"url":"http://owned.invalid","username":"fixture","password":"private-nzbget-password","download_path":"/completed","local_folder":"nzbget"}`)
	if e = secrets.SetNamed(ctx, "download-client:"+clientID, clientConfig); e != nil {
		t.Fatal(e)
	}
	indexerCipher, e := secrets.Seal("indexer:fixture", []byte(`{"cookie":"fixture-session"}`))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO indexers(id,name,kind,enabled,generation,ciphertext,updated_at) VALUES('fixture','Fixture','iptorrents',1,'one',?,1)`, indexerCipher); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE settings SET library_name='Recovered cinema'; UPDATE subtitle_preferences SET enabled=1,download_format='original'; UPDATE search_schedule SET enabled=1`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO library_scans(id,kind,media_root,resolved_root,folder,state,created_at,updated_at) VALUES('automatic-fixture','movie','/media','/media','Movies','completed',1,1); INSERT INTO library_match_jobs(scan_id,state,lease_owner,lease_until,created_at,updated_at) VALUES('automatic-fixture','running','previous-process',9999999999,1,1)`); e != nil {
		t.Fatal(e)
	}
	root := filepath.Dir(cfg.ConfigDir)
	backup := filepath.Join(root, "snapshot")
	m, e := Backup(ctx, cfg, backup, "0.8.0-test")
	if e != nil {
		t.Fatal(e)
	}
	if m.Schema != 75 || len(m.Files) != 2 {
		t.Fatal(m)
	}
	if _, e = Verify(ctx, backup); e != nil {
		t.Fatal(e)
	}
	cwd, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	relative, e := filepath.Rel(cwd, backup)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = Restore(ctx, cfg, relative, filepath.Join(backup, "nested-restore")); e == nil {
		t.Fatal("relative backup allowed a restore inside itself")
	}
	if _, e = Verify(ctx, backup); e != nil {
		t.Fatal("rejected nested restore modified the backup", e)
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
	var matchingState, matchingOwner string
	var matchingLease int64
	if e = restored.QueryRow(`SELECT state,lease_owner,lease_until FROM library_match_jobs WHERE scan_id='automatic-fixture'`).Scan(&matchingState, &matchingOwner, &matchingLease); e != nil || matchingState != "paused" || matchingOwner != "" || matchingLease != 0 {
		t.Fatal("restored matching must require explicit restart", matchingState, matchingOwner, matchingLease, e)
	}
	saved, e := securestore.New(ctx, restored, dest)
	if e != nil {
		t.Fatal(e)
	}
	if got, e := saved.GetNamed(ctx, "download-client:"+clientID); e != nil || !bytes.Equal(got, clientConfig) {
		t.Fatal("restored client credentials changed", e)
	}
	var restoredDefault string
	if e = restored.QueryRow(`SELECT usenet FROM download_client_preferences`).Scan(&restoredDefault); e != nil || restoredDefault != clientID {
		t.Fatal("restored default changed", e)
	}
	b, e := saved.GetNamed(ctx, "opensubtitles")
	if e != nil || !bytes.Contains(b, []byte("fixture-private-key")) {
		t.Fatal("provider secret lost", e)
	}
	if e = restored.QueryRow(`SELECT ciphertext FROM indexers WHERE id='fixture'`).Scan(&indexerCipher); e != nil {
		t.Fatal(e)
	}
	if b, e = saved.Open("indexer:fixture", indexerCipher); e != nil || !bytes.Contains(b, []byte("fixture-session")) {
		t.Fatal("indexer credential lost in recovery", e)
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
	var format string
	if e = restored.QueryRow(`SELECT download_format FROM subtitle_preferences`).Scan(&format); e != nil || format != "original" {
		t.Fatal("restored subtitle format changed", e)
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
	if body[0] == 'A' {
		body[0] = 'B'
	} else {
		body[0] = 'A'
	}
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
	// Even a matching checksum cannot make the wrong encryption key valid.
	m, e := ReadManifest(root)
	if e != nil {
		t.Fatal(e)
	}
	changed, e := measure(ctx, root, "secrets.key")
	if e != nil {
		t.Fatal(e)
	}
	for i := range m.Files {
		if m.Files[i].Name == "secrets.key" {
			m.Files[i] = changed
		}
	}
	payload, _ := json.Marshal(m)
	if e = os.WriteFile(filepath.Join(dest, "backup-manifest.json"), payload, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Verify(ctx, dest); e == nil {
		t.Fatal("wrong key accepted after checksum update")
	}
	if _, e = Restore(ctx, cfg, dest, dest+"-recovered"); e == nil {
		t.Fatal("wrong-key backup restored")
	}
	if _, e = os.Stat(dest + "-recovered"); !os.IsNotExist(e) {
		t.Fatal("failed verification created a runnable destination")
	}
	m = Manifest{Format: 1, Version: "test", Schema: 7, Created: time.Now().UTC().Format(time.RFC3339), Files: []File{{Name: "../escaped.db", Size: 1, SHA: strings.Repeat("0", 64)}}}
	payload, _ = json.Marshal(m)
	if e = os.WriteFile(filepath.Join(dest, "backup-manifest.json"), payload, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadManifest(root); e == nil {
		t.Fatal("manifest path escape accepted")
	}
}

func TestBackupCancellationAndStorageOverlapPreserveSource(t *testing.T) {
	cfg := fixtureConfig(t)
	ctx := context.Background()
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	dest := filepath.Join(filepath.Dir(cfg.ConfigDir), "canceled")
	if _, e = Backup(canceled, cfg, dest, "test"); e == nil {
		t.Fatal("canceled backup succeeded")
	}
	if _, e = os.Stat(dest); !os.IsNotExist(e) {
		t.Fatal("canceled snapshot published")
	}
	if _, e = Backup(ctx, cfg, filepath.Join(cfg.ConfigDir, "nested"), "test"); e == nil {
		t.Fatal("nested config backup accepted")
	}
	if _, e = database.Inspect(ctx, db); e != nil {
		t.Fatal("source database changed", e)
	}
}
