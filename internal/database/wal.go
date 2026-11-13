package database

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
)

// WALStats is a read-only snapshot of write-ahead-log health (RS-0037). It
// never runs a checkpoint and never blocks behind writers: page counters come
// from PRAGMA reads and the WAL size from the file system.
type WALStats struct {
	// PageCount is the size of the main database file in pages.
	PageCount int64 `json:"page_count"`
	// FreelistPages are unused pages inside the main database file.
	FreelistPages int64 `json:"freelist_pages"`
	// PageSize in bytes, so consumers can convert page counts.
	PageSize int64 `json:"page_size"`
	// WALFileBytes is the current size of the -wal sidecar file. Sustained
	// growth across checkpoints is the starvation signature.
	WALFileBytes int64 `json:"wal_file_bytes"`
	// AutoCheckpointPages is the configured autocheckpoint threshold
	// (pages; 0 disables automatic checkpoints).
	AutoCheckpointPages int64 `json:"autocheckpoint_pages"`
}

// WALInfo collects the read-only WAL snapshot. The -wal sidecar may not exist
// after a clean checkpoint; that is reported as zero bytes, not an error.
func WALInfo(ctx context.Context, db *sql.DB) (WALStats, error) {
	var stats WALStats
	if err := db.QueryRowContext(ctx, `PRAGMA page_count`).Scan(&stats.PageCount); err != nil {
		return stats, err
	}
	if err := db.QueryRowContext(ctx, `PRAGMA freelist_count`).Scan(&stats.FreelistPages); err != nil {
		return stats, err
	}
	if err := db.QueryRowContext(ctx, `PRAGMA page_size`).Scan(&stats.PageSize); err != nil {
		return stats, err
	}
	if err := db.QueryRowContext(ctx, `PRAGMA wal_autocheckpoint`).Scan(&stats.AutoCheckpointPages); err != nil {
		return stats, err
	}
	// The WAL filename follows SQLite's convention: <database>-wal.
	if name := walFilename(db); name != "" {
		if info, err := os.Stat(name); err == nil {
			stats.WALFileBytes = info.Size()
		}
	}
	return stats, nil
}

func walFilename(db *sql.DB) string {
	var path string
	if err := db.QueryRow(`PRAGMA database_list`).Scan(new(any), new(any), &path); err != nil {
		return ""
	}
	if path == "" {
		return ""
	}
	return filepath.FromSlash(path + "-wal")
}
