// Package maintenance creates verified offline configuration snapshots. It does
// not copy media, migrate databases, reset accounts, or replace config folders.
package maintenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/faaaaq/ReelScarey/internal/config"
	"github.com/faaaaq/ReelScarey/internal/database"
	"github.com/faaaaq/ReelScarey/internal/instance"
	"github.com/faaaaq/ReelScarey/internal/securestore"
)

const maxDatabase = int64(32 << 30)

var fileNames = []string{"reelscarey.db", "secrets.key", "setup-token"}

type File struct {
	Name string `json:"name"`
	Size int64  `json:"size_bytes"`
	SHA  string `json:"sha256"`
}
type Manifest struct {
	Format  int    `json:"format"`
	Version string `json:"application_version"`
	Schema  int    `json:"schema_version"`
	Created string `json:"created_at"`
	Files   []File `json:"files"`
}

func overlap(a, b string) bool {
	r, e := filepath.Rel(a, b)
	return e == nil && (r == "." || filepath.IsLocal(r))
}
func newDestination(cfg config.Config, name string, restore bool) (string, error) {
	dest, e := filepath.Abs(name)
	if e != nil {
		return "", e
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(dest))
	if e != nil {
		return "", errors.New("the destination parent folder must already exist")
	}
	dest = filepath.Join(parent, filepath.Base(dest))
	if _, e = os.Lstat(dest); !errors.Is(e, os.ErrNotExist) {
		return "", errors.New("destination must be a new folder; existing folders are never replaced")
	}
	for _, p := range []string{cfg.ConfigDir, cfg.MediaDir, cfg.DownloadsDir} {
		if p == "" {
			continue
		}
		p, e = filepath.Abs(p)
		if e != nil {
			return "", e
		}
		if canonical, ce := filepath.EvalSymlinks(p); ce == nil {
			p = canonical
		}
		if restore {
			if rel, e := filepath.Rel(p, dest); e == nil && rel == "." && p == cfg.ConfigDir {
				continue
			}
		}
		if overlap(p, dest) || overlap(dest, p) {
			return "", errors.New("backup and restore destinations must be separate from current config, media and downloads")
		}
	}
	return dest, nil
}
func stageFor(dest string) (string, error) {
	return os.MkdirTemp(filepath.Dir(dest), ".reelscarey-maintenance-")
}
func cleanupStage(stage string) {
	// The path was returned by MkdirTemp, remains under its resolved parent and is
	// never populated from an archive path or user-supplied filename.
	absolute, e := filepath.Abs(stage)
	if e != nil || !strings.HasPrefix(filepath.Base(absolute), ".reelscarey-maintenance-") {
		return
	}
	parent, e := filepath.EvalSymlinks(filepath.Dir(absolute))
	if e != nil || parent != filepath.Dir(absolute) {
		return
	}
	info, e := os.Lstat(absolute)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	_ = os.RemoveAll(absolute)
}
func regular(root *os.Root, name string, max int64) (*os.File, error) {
	before, e := root.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !before.Mode().IsRegular() || before.Size() > max {
		return nil, errors.New("backup file must be a supported regular file")
	}
	f, e := root.Open(name)
	if e != nil {
		return nil, e
	}
	info, e := f.Stat()
	if e != nil || !os.SameFile(before, info) {
		f.Close()
		return nil, errors.New("backup file changed during inspection")
	}
	return f, nil
}
func measure(ctx context.Context, root *os.Root, name string) (File, error) {
	limit := maxDatabase
	if name != "reelscarey.db" {
		limit = 1024
	}
	f, e := regular(root, name, limit)
	if e != nil {
		return File{}, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return File{}, e
	}
	h := sha256.New()
	n, e := io.Copy(h, &contextReader{ctx: ctx, r: io.LimitReader(f, limit+1)})
	if e != nil {
		return File{}, e
	}
	after, e := f.Stat()
	if e != nil || n != info.Size() || n > limit || after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return File{}, errors.New("backup file changed while checksumming")
	}
	return File{Name: name, Size: n, SHA: hex.EncodeToString(h.Sum(nil))}, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.Read(b)
}
func copyFile(ctx context.Context, source, dest *os.Root, want File) error {
	f, e := regular(source, want.Name, maxDatabase)
	if e != nil {
		return e
	}
	defer f.Close()
	out, e := dest.OpenFile(want.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("a destination file already exists or cannot be created")
	}
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(out, h), &contextReader{ctx, io.LimitReader(f, want.Size+1)})
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e != nil {
		return e
	}
	if ce != nil {
		return ce
	}
	if n != want.Size || hex.EncodeToString(h.Sum(nil)) != want.SHA {
		return errors.New("backup checksum verification failed")
	}
	return nil
}
func syncFolder(root *os.Root) error {
	f, e := root.Open(".")
	if e != nil {
		return e
	}
	defer f.Close()
	e = f.Sync()
	if runtime.GOOS == "windows" {
		return nil
	}
	return e
}
func writeJSON(root *os.Root, name string, v any) error {
	f, e := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	e = json.NewEncoder(f).Encode(v)
	if e == nil {
		e = f.Sync()
	}
	return errors.Join(e, f.Close())
}
func validateDB(ctx context.Context, dir string) (int, error) {
	db, e := database.OpenExisting(ctx, filepath.Join(dir, "reelscarey.db"), true)
	if e != nil {
		return 0, e
	}
	defer db.Close()
	schema, e := database.Inspect(ctx, db)
	if e != nil {
		return 0, e
	}
	if e = securestore.Validate(ctx, db, dir); e != nil {
		return 0, e
	}
	return schema, nil
}

func Backup(ctx context.Context, cfg config.Config, destination, version string) (Manifest, error) {
	var empty Manifest
	if _, e := os.Stat(filepath.Join(cfg.ConfigDir, "reelscarey.db")); e != nil {
		return empty, errors.New("the source config has no database")
	}
	lock, e := instance.Acquire(cfg.ConfigDir)
	if e != nil {
		return empty, e
	}
	defer lock.Close()
	guard, e := instance.Guard(cfg.ConfigDir, false)
	if e != nil {
		return empty, e
	}
	defer guard.Close()
	if e = instance.CheckStart(cfg.ConfigDir); e != nil {
		return empty, e
	}
	return BackupOwned(ctx, cfg, destination, version)
}

// BackupOwned is for the server that already holds the config ownership lock.
// SQLite snapshots committed WAL data while other connections remain open. The
// encryption key is immutable for the lifetime of that configuration.
func BackupOwned(ctx context.Context, cfg config.Config, destination, version string) (Manifest, error) {
	var empty Manifest
	dest, e := newDestination(cfg, destination, false)
	if e != nil {
		return empty, e
	}
	stage, e := stageFor(dest)
	if e != nil {
		return empty, e
	}
	defer cleanupStage(stage)
	source, e := os.OpenRoot(cfg.ConfigDir)
	if e != nil {
		return empty, e
	}
	defer source.Close()
	target, e := os.OpenRoot(stage)
	if e != nil {
		return empty, e
	}
	defer target.Close()
	sourceDB, e := regular(source, "reelscarey.db", maxDatabase)
	if e != nil {
		return empty, e
	}
	sourceDB.Close()
	db, e := database.OpenExisting(ctx, filepath.Join(cfg.ConfigDir, "reelscarey.db"), false)
	if e != nil {
		return empty, e
	}
	// SQLite creates a consistent compact snapshot, including committed WAL data.
	_, e = db.ExecContext(ctx, `VACUUM INTO ?`, filepath.Join(stage, "reelscarey.db"))
	db.Close()
	if e != nil {
		return empty, errors.New("SQLite could not create a consistent backup snapshot")
	}
	for _, name := range fileNames[1:] {
		file, e := measure(ctx, source, name)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return empty, e
		}
		if e = copyFile(ctx, source, target, file); e != nil {
			return empty, e
		}
	}
	schema, e := validateDB(ctx, stage)
	if e != nil {
		return empty, e
	}
	manifest := Manifest{Format: 1, Version: version, Schema: schema, Created: time.Now().UTC().Format(time.RFC3339), Files: []File{}}
	for _, name := range fileNames {
		file, e := measure(ctx, target, name)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			return empty, e
		}
		manifest.Files = append(manifest.Files, file)
	}
	if e = os.Mkdir(dest, 0700); e != nil {
		return empty, errors.New("backup destination appeared during creation; it was not replaced")
	}
	output, e := os.OpenRoot(dest)
	if e != nil {
		return empty, e
	}
	defer output.Close()
	if e = writeJSON(output, ".backup-in-progress", map[string]int{"format": 1}); e != nil {
		return empty, e
	}
	for _, file := range manifest.Files {
		if e = copyFile(ctx, target, output, file); e != nil {
			return empty, e
		}
	}
	if e = writeJSON(output, "backup-manifest.json", manifest); e != nil {
		return empty, e
	}
	if e = syncFolder(output); e != nil {
		return empty, e
	}
	if e = output.Remove(".backup-in-progress"); e != nil {
		return empty, e
	}
	if e = syncFolder(output); e != nil {
		return empty, e
	}
	return manifest, nil
}

func ReadManifest(root *os.Root) (Manifest, error) {
	var m Manifest
	f, e := regular(root, "backup-manifest.json", 32768)
	if e != nil {
		return m, e
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 32769))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&m); e != nil {
		return m, errors.New("backup manifest is invalid")
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return m, errors.New("backup manifest contains extra data")
	}
	if m.Format != 1 || m.Version == "" || len(m.Version) > 80 || m.Schema < 1 || len(m.Files) < 1 || len(m.Files) > 3 {
		return m, errors.New("backup manifest format is unsupported")
	}
	if _, e := time.Parse(time.RFC3339, m.Created); e != nil {
		return m, errors.New("backup creation timestamp is invalid")
	}
	seen := map[string]bool{}
	for _, f := range m.Files {
		decoded, e := hex.DecodeString(f.SHA)
		if !slices.Contains(fileNames, f.Name) || seen[f.Name] || f.Size <= 0 || f.Size > maxDatabase || e != nil || len(decoded) != 32 || (f.Name != "reelscarey.db" && f.Size > 1024) {
			return m, errors.New("backup manifest contains unsupported file entries")
		}
		seen[f.Name] = true
	}
	if !seen["reelscarey.db"] {
		return m, errors.New("backup has no database")
	}
	entries, e := root.Open(".")
	if e != nil {
		return m, e
	}
	list, e := entries.ReadDir(10)
	entries.Close()
	if e != nil && e != io.EOF {
		return m, e
	}
	for _, entry := range list {
		if entry.Name() != "backup-manifest.json" && !seen[entry.Name()] {
			return m, errors.New("backup directory contains unexpected or incomplete files")
		}
	}
	return m, nil
}

func Verify(ctx context.Context, dir string) (Manifest, error) {
	root, e := os.OpenRoot(dir)
	if e != nil {
		return Manifest{}, e
	}
	defer root.Close()
	m, e := ReadManifest(root)
	if e != nil {
		return m, e
	}
	for _, file := range m.Files {
		actual, e := measure(ctx, root, file.Name)
		if e != nil || actual != file {
			return m, errors.New("backup files failed checksum verification")
		}
	}
	schema, e := validateDB(ctx, dir)
	if e != nil {
		return m, e
	}
	if schema != m.Schema {
		return m, errors.New("backup schema differs from its manifest")
	}
	return m, nil
}

func Restore(ctx context.Context, cfg config.Config, backup, destination string) (Manifest, error) {
	m, e := Verify(ctx, backup)
	if e != nil {
		return m, e
	}
	dest, e := newDestination(cfg, destination, true)
	if e != nil {
		return m, e
	}
	sourcePath, e := filepath.EvalSymlinks(backup)
	if e != nil {
		return m, e
	}
	sourcePath, e = filepath.Abs(sourcePath)
	if e != nil {
		return m, e
	}
	if overlap(sourcePath, dest) || overlap(dest, sourcePath) {
		return m, errors.New("restore destination must be separate from its backup")
	}
	stage, e := stageFor(dest)
	if e != nil {
		return m, e
	}
	defer cleanupStage(stage)
	source, e := os.OpenRoot(backup)
	if e != nil {
		return m, e
	}
	defer source.Close()
	target, e := os.OpenRoot(stage)
	if e != nil {
		return m, e
	}
	defer target.Close()
	for _, file := range m.Files {
		if e = copyFile(ctx, source, target, file); e != nil {
			return m, e
		}
	}
	schema, e := validateDB(ctx, stage)
	if e != nil || schema != m.Schema {
		return m, errors.New("restored database or encryption key failed verification")
	}
	if e = os.Mkdir(dest, 0700); e != nil {
		return m, errors.New("restore destination already exists; it was preserved")
	}
	lock, e := instance.Acquire(dest)
	if e != nil {
		return m, e
	}
	defer lock.Close()
	guard, e := instance.Guard(dest, false)
	if e != nil {
		return m, e
	}
	defer guard.Close()
	output, e := os.OpenRoot(dest)
	if e != nil {
		return m, e
	}
	defer output.Close()
	if e = writeJSON(output, ".restore-in-progress", map[string]int{"format": 1}); e != nil {
		return m, e
	}
	for _, file := range m.Files {
		if e = copyFile(ctx, target, output, file); e != nil {
			return m, e
		}
	}
	if e = writeJSON(output, "recovery-mode", map[string]int{"format": 1}); e != nil {
		return m, e
	}
	if e = pauseRestored(ctx, dest); e != nil {
		return m, e
	}
	if _, e = validateDB(ctx, dest); e != nil {
		return m, fmt.Errorf("restore is incomplete: %w", e)
	}
	if e = syncFolder(output); e != nil {
		return m, e
	}
	if e = output.Remove(".restore-in-progress"); e != nil {
		return m, e
	}
	if e = syncFolder(output); e != nil {
		return m, e
	}
	return m, nil
}

func pauseRestored(ctx context.Context, dir string) error {
	db, e := database.OpenCurrent(ctx, filepath.Join(dir, "reelscarey.db"))
	if e != nil {
		return e
	}
	defer db.Close()
	tx, e := db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, q := range []struct{ table, sql string }{
		{"sessions", `DELETE FROM sessions`},
		{"request_sessions", `DELETE FROM request_sessions`},
		{"backup_schedule", `UPDATE backup_schedule SET enabled=0,running=0,next_run=0,last_error='',revision=revision+1`},
		{"metadata_refresh_schedule", `UPDATE metadata_refresh_schedule SET enabled=0,requested=0,lease_owner='',lease_until=0,next_run=0,revision=revision+1,state='paused',message='Restored backup: review metadata settings before resuming.'`},
		{"youtube_subscriptions", `UPDATE youtube_subscriptions SET enabled=0,requested=0,lease_owner='',lease_until=0,next_run=0,revision=revision+1,state='paused',message='Restored backup: review subscriptions before enabling.'`},
		{"search_schedule", `UPDATE search_schedule SET enabled=0,requested=0,lease_owner='',lease_until=0,revision=revision+1,state='paused',message='Restored backup: review settings before enabling searches.'`},
		{"subtitle_preferences", `UPDATE subtitle_preferences SET enabled=0,revision=revision+1`},
		{"subtitle_jobs", `UPDATE subtitle_jobs SET requested=0,lease_owner='',lease_until=0,state='paused',message='Restored backup: review settings before resuming.'`},
	} {
		var n int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, q.table).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			if _, e = tx.ExecContext(ctx, q.sql); e != nil {
				return e
			}
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	var busy, log, checkpointed int
	if e = db.QueryRowContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &log, &checkpointed); e != nil || busy != 0 {
		return errors.New("restored database could not finish its checkpoint")
	}
	return nil
}
