package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Open(ctx context.Context, dir string) (*sql.DB, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}
	filename, err := filepath.Abs(filepath.Join(dir, "reelscarey.db"))
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open database file: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	// A drive letter must be in the URI path, not interpreted as an authority.
	uriPath := filepath.ToSlash(filename)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	u := url.URL{Scheme: "file", Path: uriPath}
	query := url.Values{}
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)"} {
		query.Add("_pragma", pragma)
	}
	// Acquire the write lock before a transaction's first read. Deferred WAL
	// transactions can otherwise pass a revision check and then fail with
	// SQLITE_BUSY_SNAPSHOT when a concurrent commit invalidates their snapshot.
	// BEGIN IMMEDIATE uses busy_timeout while waiting and gives every fenced
	// read-modify-write transaction a current, serializable snapshot.
	query.Set("_txlock", "immediate")
	u.RawQuery = query.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	// Four connections (measured in pool_soak_test.go, RS-0006): under
	// concurrent readers plus a long lease transaction the audited pool of 2
	// serialized all HTTP reads behind the workers (read p95 2.5ms, max
	// 156ms, ~13k reads/5s); four connections cut the tail to ~11ms and
	// served 7x the reads with zero busy errors, unchanged write throughput
	// and a bounded WAL. Eight connections offered no additional write
	// benefit, so the conservative end of the measured range is used.
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB) error { return migrate(ctx, db, migrationFiles) }

func migrate(ctx context.Context, db *sql.DB, files fs.FS) error {
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY, name TEXT NOT NULL, checksum TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')))`); err != nil {
		return fmt.Errorf("initialize migration ledger: %w", err)
	}
	names, err := fs.Glob(files, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	var latest int
	if err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&latest); err != nil {
		return err
	}
	if latest > len(names) {
		return fmt.Errorf("database schema is newer than this application; restore the matching application version")
	}
	for index, name := range names {
		version, err := strconv.Atoi(strings.Split(filepath.Base(name), "_")[0])
		if err != nil || version != index+1 {
			return fmt.Errorf("migration sequence is invalid at %s", name)
		}
		body, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(body))
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		err = apply(ctx, tx, version, name, checksum, body)
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func apply(ctx context.Context, tx *sql.Tx, version int, name, checksum string, body []byte) error {
	var saved string
	err := tx.QueryRowContext(ctx, "SELECT checksum FROM schema_migrations WHERE version = ?", version).Scan(&saved)
	if err == nil {
		if saved != checksum {
			return fmt.Errorf("checksum mismatch; released migrations must not be edited")
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return err
	}
	checks, err := tx.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	violated := checks.Next()
	checkErr := checks.Err()
	checks.Close()
	if checkErr != nil {
		return checkErr
	}
	if violated {
		return fmt.Errorf("migration would break foreign key references")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO schema_migrations (version, name, checksum) VALUES (?, ?, ?)", version, name, checksum)
	return err
}

func Version(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version)
	return version, err
}

// Ping checks database liveness without blocking behind a busy shared
// connection. The caller must have configured the pool with more than one
// connection (SetMaxOpenConns) so a dedicated pool slot is available even
// when every other connection is mid-transaction. This prevents the /readyz
// healthcheck (2s budget) from 503-ing the Docker healthcheck during long
// catalog snapshots.
func Ping(ctx context.Context, db *sql.DB) error {
	var ready int
	return db.QueryRowContext(ctx, `SELECT 1`).Scan(&ready)
}
