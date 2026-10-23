package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// OpenExisting never creates a database or applies migrations. Immutable is only
// for a private, complete snapshot with no concurrent writer or WAL.
func OpenExisting(ctx context.Context, filename string, immutable bool) (*sql.DB, error) {
	return openExisting(ctx, filename, immutable, false)
}

// OpenCurrent permits administration writes without creating or migrating files.
func OpenCurrent(ctx context.Context, filename string) (*sql.DB, error) {
	return openExisting(ctx, filename, false, true)
}
func openExisting(ctx context.Context, filename string, immutable, writable bool) (*sql.DB, error) {
	info, e := os.Lstat(filename)
	if e != nil || !info.Mode().IsRegular() {
		return nil, errors.New("an existing regular database file is required")
	}
	absolute, e := filepath.Abs(filename)
	if e != nil {
		return nil, e
	}
	p := filepath.ToSlash(absolute)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	q := url.Values{"mode": {"ro"}, "_pragma": {"busy_timeout(5000)", "foreign_keys(1)"}}
	if writable {
		q.Set("mode", "rw")
	}
	if immutable {
		q.Set("immutable", "1")
	}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if e = db.PingContext(ctx); e != nil {
		db.Close()
		return nil, errors.New("database could not be opened read-only")
	}
	return db, nil
}

// Inspect verifies integrity and every recorded migration without changing it.
func Inspect(ctx context.Context, db *sql.DB) (int, error) {
	var check string
	if e := db.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&check); e != nil || check != "ok" {
		return 0, errors.New("database integrity check failed")
	}
	rows, e := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if e != nil {
		return 0, e
	}
	has := rows.Next()
	e = rows.Err()
	rows.Close()
	if e != nil || has {
		return 0, errors.New("database relationship check failed")
	}
	rows, e = db.QueryContext(ctx, `SELECT version,name,checksum FROM schema_migrations ORDER BY version`)
	if e != nil {
		return 0, errors.New("database migration history is unavailable")
	}
	defer rows.Close()
	files, e := fs.Glob(migrationFiles, "migrations/*.sql")
	if e != nil {
		return 0, e
	}
	n := 0
	for rows.Next() {
		var version int
		var name, checksum string
		if e = rows.Scan(&version, &name, &checksum); e != nil {
			return 0, e
		}
		if version != n+1 || version > len(files) {
			return 0, errors.New("database migration history requires a different application version")
		}
		body, e := fs.ReadFile(migrationFiles, files[n])
		if e != nil {
			return 0, e
		}
		if files[n] != name || fmt.Sprintf("%x", sha256.Sum256(body)) != checksum {
			return 0, errors.New("database migration history does not match this application")
		}
		n++
	}
	if e = rows.Err(); e != nil {
		return 0, e
	}
	if n == 0 {
		return 0, errors.New("database has no migration history")
	}
	return n, nil
}
