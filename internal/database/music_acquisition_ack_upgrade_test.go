package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// The acquisition-acknowledgment migration is additive: existing acquisition
// rows keep their history and the new column defaults to "still needs
// attention" so a populated database cannot lose a pending warning.
func TestMusicAcquisitionAcknowledgeUpgradeIsAdditive(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	old := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "101_" {
			continue
		}
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	if err = migrate(ctx, db, old); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_acquisition_jobs(id,title,root,state,track_count,completed_count,error,created_at,updated_at)
		VALUES(1,'Historic','Music','failed',2,0,'No tracks completed.',unixepoch(),unixepoch())`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var title, state, jobError string
	var acknowledged int
	if err = db.QueryRow(`SELECT title,state,error,acknowledged_at FROM music_acquisition_jobs WHERE id=1`).
		Scan(&title, &state, &jobError, &acknowledged); err != nil {
		t.Fatal(err)
	}
	if title != "Historic" || state != "failed" || jobError != "No tracks completed." || acknowledged != 0 {
		t.Fatal("existing acquisition row changed", title, state, jobError, acknowledged)
	}
	// The issues query the banner uses must work against upgraded data.
	var issues int
	if err = db.QueryRow(`SELECT COALESCE(SUM(CASE WHEN (state='failed' OR (state='completed' AND completed_count<track_count)) AND acknowledged_at=0 THEN 1 ELSE 0 END),0)
		FROM music_acquisition_jobs`).Scan(&issues); err != nil {
		t.Fatal(err)
	}
	if issues != 1 {
		t.Fatal("upgraded job should still need attention", issues)
	}
}
