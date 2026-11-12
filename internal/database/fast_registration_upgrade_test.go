package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestFastRegistrationUpgradePreservesExistingFilesAndPendingScan(t *testing.T) {
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
	names, _ := fs.Glob(migrationFiles, "migrations/*.sql")
	for _, name := range names {
		if filepath.Base(name) >= "043_" {
			continue
		}
		body, _ := fs.ReadFile(migrationFiles, name)
		old[name] = &fstest.MapFile{Data: body}
	}
	if err = migrate(ctx, db, old); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',1,'Saved')`,
		`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,1,'Movies/saved.mkv',100,'existing-full-hash','{"codec":"h264"}',1,'existing')`,
		`INSERT INTO library_scans(id,kind,media_root,resolved_root,folder,state,created_at,updated_at) VALUES('scan','movie','/media','/media','Movies','completed',1,1)`,
		`INSERT INTO library_scan_files(id,scan_id,relative_path,size_bytes,mtime,file_identity,state,updated_at) VALUES('file','scan','Movies/pending.mkv',200,1,'1:2','review',1)`,
		`INSERT INTO library_match_jobs(scan_id,state,phase,created_at,updated_at) VALUES('scan','running','queue',1,1)`,
		`INSERT INTO library_match_candidates(file_id,scan_id,state,payload) VALUES('file','scan','ready','{"saved":"evidence"}')`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var sha, observation, probe string
	if err = db.QueryRow(`SELECT sha256,verification_json,probe_json FROM media_files WHERE id=1`).Scan(&sha, &observation, &probe); err != nil || sha != "existing-full-hash" || observation != "" || probe != `{"codec":"h264"}` {
		t.Fatal("existing registration changed", err)
	}
	var state, phase, payload string
	if err = db.QueryRow(`SELECT j.state,j.phase,c.payload FROM library_match_jobs j JOIN library_match_candidates c ON c.scan_id=j.scan_id WHERE j.scan_id='scan'`).Scan(&state, &phase, &payload); err != nil || state != "running" || phase != "queue" || payload != `{"saved":"evidence"}` {
		t.Fatal("pending matching state changed", err)
	}
	if version, err := Version(ctx, db); err != nil || version != 90 {
		t.Fatal(version, err)
	}
}
