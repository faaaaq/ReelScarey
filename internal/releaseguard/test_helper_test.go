package releaseguard

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faaaaq/ReelScarey/internal/database"
)

var fixtureSequence atomic.Int64

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("open temporary database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// insertDownload creates one disposable movie download for stalled-state tests.
func insertDownload(t *testing.T, db *sql.DB, id, state string, progress float64, lastProgressAt, stalledAt int64) {
	t.Helper()
	sequence := fixtureSequence.Add(1)
	result, err := db.Exec(`INSERT INTO media_items(kind, tmdb_id, title) VALUES ('movie', ?, ?)`,
		sequence, "Fixture "+id)
	if err != nil {
		t.Fatalf("insert media item for %s: %v", id, err)
	}
	movieID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("read media item id for %s: %v", id, err)
	}
	if _, err := db.Exec(`INSERT INTO release_candidates
		(id, movie_id, title, size_bytes, published_at, payload, expires_at)
		VALUES (?, ?, ?, 1, ?, X'', ?)`,
		"release-"+id, movieID, "Release "+id, "2026-01-01T00:00:00Z", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatalf("insert release candidate for %s: %v", id, err)
	}
	if _, err := db.Exec(`INSERT INTO downloads
		(id, movie_id, release_id, title, info_hash, payload, client_url, save_path,
		 state, progress, last_progress_at, stalled_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, X'', 'http://127.0.0.1:8080', '/downloads',
		 ?, ?, ?, ?, ?, ?)`,
		id, movieID, "release-"+id, "Release "+id, fmt.Sprintf("%040x", sequence),
		state, progress, lastProgressAt, stalledAt, lastProgressAt, lastProgressAt); err != nil {
		t.Fatalf("insert download %s: %v", id, err)
	}
}

func downloadStalledAt(t *testing.T, db *sql.DB, id string) int64 {
	t.Helper()
	var stalledAt int64
	if err := db.QueryRow(`SELECT stalled_at FROM downloads WHERE id = ?`, id).Scan(&stalledAt); err != nil {
		t.Fatalf("read stalled_at for %s: %v", id, err)
	}
	return stalledAt
}
