package backups

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
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

func fixture(t *testing.T) (*Service, *sql.DB, config.Config) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	cfg := config.Config{ConfigDir: filepath.Join(dir, "config"), BackupsDir: filepath.Join(dir, "backups"), MediaDir: filepath.Join(dir, "media"), DownloadsDir: filepath.Join(dir, "downloads")}
	db, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	a, e := auth.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	code, e := securestore.SecretFile(filepath.Join(cfg.ConfigDir, "setup-token"), 32, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Setup(ctx, "testadmin", "fixture-only-password", base64.RawURLEncoding.EncodeToString(code)); e != nil {
		t.Fatal(e)
	}
	if _, e = a.CreateRequestAccount(ctx, "viewer", "fixture-viewer-password"); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Login(ctx, "viewer", "fixture-viewer-password"); e != nil {
		t.Fatal(e)
	}
	key, e := securestore.New(ctx, db, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	if e = key.SetNamed(ctx, "opensubtitles", []byte(`{"api_key":"private-fixture"}`)); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE settings SET library_name='Snapshot cinema'; INSERT INTO media_items(kind,tmdb_id,title,year,monitored,status,overview,poster_path,release_date) VALUES('movie',101,'Fixture movie',2026,1,'missing','','','2026-01-01')`); e != nil {
		t.Fatal(e)
	}
	s, e := New(ctx, db, cfg, "test-version", func() {})
	if e != nil {
		t.Fatal(e)
	}
	return s, db, cfg
}
func mustArchive(t *testing.T, s *Service, kind string) Archive {
	t.Helper()
	a, e := s.create(context.Background(), kind)
	if e != nil {
		t.Fatal(e)
	}
	return a
}

func TestOnlineBackupUploadReviewedRestoreAndSafetySnapshot(t *testing.T) {
	s, db, cfg := fixture(t)
	ctx := context.Background()
	lock, e := instance.Acquire(cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Close()
	a := mustArchive(t, s, "manual") // A live owner and committed WAL do not block the server snapshot.
	if _, e = db.Exec(`UPDATE settings SET library_name='Current cinema'; UPDATE backup_schedule SET enabled=1`); e != nil {
		t.Fatal(e)
	}
	f, _, e := s.Open(a.ID)
	if e != nil {
		t.Fatal(e)
	}
	uploaded, e := s.Upload(ctx, f)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	review, e := s.Preview(ctx, uploaded.ID)
	if e != nil || review.Movies != 1 || review.Administrator != "testadmin" {
		t.Fatal(review, e)
	}
	if e = s.PrepareRestore(ctx, uploaded.ID, "stale", "RESTORE"); e == nil || s.Restoring() {
		t.Fatal("stale review accepted")
	}
	if e = s.PrepareRestore(ctx, uploaded.ID, uploaded.SHA, "no"); e == nil {
		t.Fatal("missing confirmation accepted")
	}
	if e = s.PrepareRestore(ctx, uploaded.ID, uploaded.SHA, "RESTORE"); e != nil {
		t.Fatal(e)
	}
	if !s.Restoring() {
		t.Fatal("writes are not fenced")
	}
	if e = s.Delete(a.ID); e != ErrBusy {
		t.Fatal("restore archive could be deleted", e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	if e = ApplyPending(ctx, cfg, "test-version"); e != nil {
		t.Fatal(e)
	}
	restored, e := database.Open(ctx, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	defer restored.Close()
	var name string
	if e = restored.QueryRow(`SELECT library_name FROM settings`).Scan(&name); e != nil || name != "Snapshot cinema" {
		t.Fatal(name, e)
	}
	for _, table := range []string{"sessions", "request_sessions"} {
		var n int
		if e = restored.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); e != nil || n != 0 {
			t.Fatal("restored session survived", table, n, e)
		}
	}
	var enabled int
	if e = restored.QueryRow(`SELECT enabled FROM backup_schedule`).Scan(&enabled); e != nil || enabled != 0 {
		t.Fatal(enabled, e)
	}
	if !instance.RecoveryPending(cfg.ConfigDir) {
		t.Fatal("restored workers not held")
	}
	key, e := securestore.New(ctx, restored, cfg.ConfigDir)
	if e != nil {
		t.Fatal(e)
	}
	secret, e := key.GetNamed(ctx, "opensubtitles")
	if e != nil || !bytes.Contains(secret, []byte("private-fixture")) {
		t.Fatal("credentials not preserved", e)
	}
	list, e := s.List()
	if e != nil {
		t.Fatal(e)
	}
	safety := ""
	for _, a := range list {
		if a.Kind == "pre_restore" {
			safety = a.ID
		}
	}
	if safety == "" {
		t.Fatal("no safety snapshot")
	}
	_, dir, e := s.inspect(ctx, safety)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	old, e := database.OpenExisting(ctx, filepath.Join(dir, "reelscarey.db"), true)
	if e != nil {
		t.Fatal(e)
	}
	defer old.Close()
	if e = old.QueryRow(`SELECT library_name FROM settings`).Scan(&name); e != nil || name != "Current cinema" {
		t.Fatal("safety snapshot did not preserve latest state", name, e)
	}
	if e = ApplyPending(ctx, cfg, "test-version"); e != nil {
		t.Fatal("restore not idempotent", e)
	}
}

func TestRejectUnsafeArchivesAndChangedBytes(t *testing.T) {
	s, _, _ := fixture(t)
	ctx := context.Background()
	for _, name := range []string{"../reelscarey.db", "/reelscarey.db", "folder/reelscarey.db", "secrets.key\\other"} {
		var b bytes.Buffer
		z := zip.NewWriter(&b)
		f, _ := z.Create(name)
		f.Write([]byte("bad"))
		f, _ = z.Create("backup-manifest.json")
		f.Write([]byte("{}"))
		z.Close()
		if _, e := s.Upload(ctx, &b); e == nil {
			t.Fatal("unsafe archive accepted", name)
		}
	}
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for range 2 {
		f, _ := z.Create("reelscarey.db")
		f.Write([]byte("bad"))
	}
	z.Close()
	if _, e := s.Upload(ctx, &b); e == nil {
		t.Fatal("duplicate entries accepted")
	}
	a := mustArchive(t, s, "manual")
	f, e := os.OpenFile(filepath.Join(s.dir, a.ID+".zip"), os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteAt([]byte("damage"), 15)
	f.Close()
	if _, e := s.Preview(ctx, a.ID); e == nil {
		t.Fatal("changed archive accepted")
	}
	if _, _, e := s.Open("../other"); e == nil {
		t.Fatal("path accepted as ID")
	}
}

func TestZIPDirectoryIsBoundedBeforeReadingEntryMetadata(t *testing.T) {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range []string{"reelscarey.db", "backup-manifest.json"} {
		f, _ := z.Create(name)
		_, _ = f.Write([]byte("fixture"))
	}
	_ = z.Close()
	valid := b.Bytes()
	end := bytes.LastIndex(valid, []byte{'P', 'K', 5, 6})
	for _, kind := range []string{"valid", "many_entries", "oversized_directory", "extra_tail", "different_disk"} {
		t.Run(kind, func(t *testing.T) {
			data := bytes.Clone(valid)
			switch kind {
			case "many_entries":
				binary.LittleEndian.PutUint16(data[end+8:], 65535)
				binary.LittleEndian.PutUint16(data[end+10:], 65535)
			case "oversized_directory":
				binary.LittleEndian.PutUint32(data[end+12:], 1<<30)
			case "extra_tail":
				data = append(data, 'x')
			case "different_disk":
				binary.LittleEndian.PutUint16(data[end+4:], 1)
			}
			f, e := os.CreateTemp(t.TempDir(), "zip-")
			if e != nil {
				t.Fatal(e)
			}
			defer f.Close()
			if _, e = f.Write(data); e != nil {
				t.Fatal(e)
			}
			e = boundedDirectory(f, int64(len(data)))
			if (e == nil) != (kind == "valid") {
				t.Fatal(kind, e)
			}
		})
	}
}

func TestScheduleRevisionsPersistenceAndRetentionProtectOtherBackups(t *testing.T) {
	s, db, cfg := fixture(t)
	ctx := context.Background()
	p, e := s.Schedule(ctx)
	if e != nil || p.Enabled {
		t.Fatal(p, e)
	}
	p.Enabled = true
	p.KeepCount = 1
	if e = s.Save(ctx, p); e != nil {
		t.Fatal(e)
	}
	if e = s.Save(ctx, p); e == nil {
		t.Fatal("stale schedule overwrote saved settings")
	}
	reopened, e := New(ctx, db, cfg, "test", func() {})
	if e != nil {
		t.Fatal(e)
	}
	got, e := reopened.Schedule(ctx)
	if e != nil || !got.Enabled || got.NextRun <= time.Now().Unix() {
		t.Fatal(got, e)
	}
	manual := mustArchive(t, s, "manual")
	safety := mustArchive(t, s, "pre_restore")
	old := mustArchive(t, s, "scheduled")
	root, e := os.OpenRoot(s.dir)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	old.Created -= 100
	if e = replaceJSON(root, old.ID+".json", old); e != nil {
		t.Fatal(e)
	}
	latest := mustArchive(t, s, "scheduled")
	if e = s.retain(ctx); e != nil {
		t.Fatal(e)
	}
	for _, a := range []Archive{manual, safety, latest} {
		if _, e = s.record(a.ID); e != nil {
			t.Fatal("protected backup removed", a.Kind, e)
		}
	}
	if _, e = s.record(old.ID); !os.IsNotExist(e) {
		t.Fatal("old scheduled backup retained", e)
	}
	if _, e = os.Stat(filepath.Join(s.dir, "unknown-user-file")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(s.dir, "unknown-user-file"), []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.retain(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(s.dir, "unknown-user-file")); e != nil {
		t.Fatal("unknown file touched", e)
	}
}

func TestManualWorkerCompletesAndReportsInterruptedJob(t *testing.T) {
	s, db, cfg := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); s.Run(ctx, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	defer func() { cancel(); <-done }()
	if e := s.Start(ctx); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		p, e := s.Schedule(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if !p.Running && p.LastSuccess > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("backup did not complete", p)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, e := db.Exec(`UPDATE backup_schedule SET running=1`); e != nil {
		t.Fatal(e)
	}
	restarted, e := New(ctx, db, cfg, "test", func() {})
	if e != nil {
		t.Fatal(e)
	}
	p, e := restarted.Schedule(ctx)
	if e != nil || p.Running || !strings.Contains(p.LastError, "interrupted") {
		t.Fatal(p, e)
	}
}

func TestStorageRefusesOverlapAndSymlinkWithoutWritingLibrary(t *testing.T) {
	s, _, cfg := fixture(t)
	cfg.BackupsDir = filepath.Join(cfg.MediaDir, "new-child", "backups")
	if _, e := backupDir(cfg); e == nil {
		t.Fatal("overlap accepted")
	}
	if _, e := os.Stat(cfg.MediaDir); !os.IsNotExist(e) {
		t.Fatal("library modified before validation")
	}
	link := filepath.Join(filepath.Dir(s.dir), "link")
	if e := os.Symlink(s.dir, link); e != nil {
		t.Skip(e)
	}
	cfg.BackupsDir = link
	if _, e := backupDir(cfg); e == nil {
		t.Fatal("symlink storage accepted")
	}
}

func TestDueScheduleRunsOnceAndHonorsRecoveryHold(t *testing.T) {
	s, db, cfg := fixture(t)
	ctx := context.Background()
	if _, e := db.Exec(`UPDATE backup_schedule SET enabled=1,next_run=1`); e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(cfg.ConfigDir, "recovery-mode")
	if e := os.WriteFile(marker, []byte(`{"format":1}`), 0600); e != nil {
		t.Fatal(e)
	}
	s.tick(ctx)
	p, e := s.Schedule(ctx)
	if e != nil || p.Running || len(s.jobs) != 0 {
		t.Fatal("schedule bypassed recovery hold", p, e)
	}
	if e := os.Remove(marker); e != nil {
		t.Fatal(e)
	}
	run, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { defer close(done); s.Run(run, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		p, e = s.Schedule(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if p.LastSuccess > 0 && !p.Running {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("due schedule did not run", p)
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.tick(ctx)
	list, e := s.List()
	if e != nil || len(list) != 1 || list[0].Kind != "scheduled" || len(s.jobs) != 0 || p.NextRun <= time.Now().Unix() {
		t.Fatal(list, p, e)
	}
}

func TestInterruptedFileSwitchRollsBackAtEveryBoundary(t *testing.T) {
	for cut := 0; cut <= 7; cut++ {
		t.Run(string(rune('A'+cut)), func(t *testing.T) {
			s, db, cfg := fixture(t)
			ctx := context.Background()
			a := mustArchive(t, s, "manual")
			if _, e := db.Exec(`UPDATE settings SET library_name='Latest state'`); e != nil {
				t.Fatal(e)
			}
			if e := s.PrepareRestore(ctx, a.ID, a.SHA, "RESTORE"); e != nil {
				t.Fatal(e)
			}
			db.Close()
			root, e := os.OpenRoot(cfg.ConfigDir)
			if e != nil {
				t.Fatal(e)
			}
			defer root.Close()
			p, e := readPending(root)
			if e != nil {
				t.Fatal(e)
			}
			previous := ".restore-previous-" + p.ID
			if e = root.Mkdir(previous, 0700); e != nil {
				t.Fatal(e)
			}
			for _, name := range switchedFiles {
				if _, e := root.Lstat(name); e == nil {
					p.Original = append(p.Original, name)
				}
			}
			p.Phase = "applying"
			if e = replaceJSON(root, ".restore-request.json", p); e != nil {
				t.Fatal(e)
			}
			step := 0
			for _, name := range p.Original {
				if step >= cut {
					break
				}
				if e = root.Rename(name, filepath.Join(previous, name)); e != nil {
					t.Fatal(e)
				}
				step++
			}
			if step == len(p.Original) {
				for _, f := range p.Files {
					if step >= cut {
						break
					}
					b, e := os.ReadFile(filepath.Join(s.dir, ".restore-ready-"+p.ID, f.Name))
					if e != nil {
						t.Fatal(e)
					}
					if e = root.WriteFile(f.Name, b, 0600); e != nil {
						t.Fatal(e)
					}
					step++
				}
			}
			if e = ApplyPending(ctx, cfg, "test"); e != nil {
				t.Fatal(e)
			}
			recovered, e := database.Open(ctx, cfg.ConfigDir)
			if e != nil {
				t.Fatal(e)
			}
			defer recovered.Close()
			var name string
			if e = recovered.QueryRow(`SELECT library_name FROM settings`).Scan(&name); e != nil || name != "Latest state" {
				t.Fatal("previous database not recovered", name, e)
			}
			if e = securestore.Validate(ctx, recovered, cfg.ConfigDir); e != nil {
				t.Fatal(e)
			}
			b, e := os.ReadFile(filepath.Join(cfg.ConfigDir, "restore-result.json"))
			if e != nil {
				t.Fatal(e)
			}
			var result struct {
				Message string `json:"message"`
			}
			if e = json.Unmarshal(b, &result); e != nil || !strings.Contains(result.Message, "rolled back") {
				t.Fatal(result, e)
			}
		})
	}
}
