package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestExistingLibraryUpgradePreservesFileAndSubtitleReferences(t *testing.T) {
	ctx := context.Background()
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, e = db.Exec(`PRAGMA foreign_keys=ON`); e != nil {
		t.Fatal(e)
	}
	old := fstest.MapFS{}
	names, _ := fs.Glob(migrationFiles, "migrations/*.sql")
	for _, n := range names {
		if filepath.Base(n) >= "009_" {
			continue
		}
		b, _ := fs.ReadFile(migrationFiles, n)
		old[n] = &fstest.MapFile{Data: b}
	}
	if e = migrate(ctx, db, old); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',1,'Existing movie'),(2,'series',2,'Existing TV')`,
		`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('r',1,'release',1,'',X'01',1)`,
		`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at) VALUES('d',1,'r','release','hash',X'02','https://unused.invalid','/unused','completed',1,1)`,
		`INSERT INTO media_files(id,movie_id,download_id,relative_path,size_bytes,sha256,probe_json,imported_at) VALUES(42,1,'d','Movies/video.mp4',10,'sha','{}',1)`,
		`INSERT INTO series_details(media_id) VALUES(2)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count) VALUES(1,2,3,1,'S1','','',1)`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date) VALUES(1,1,4,1,'Episode','','')`,
		`INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,42)`,
		`INSERT INTO subtitle_jobs(id,media_file_id,created_at,updated_at) VALUES('job',42,1,1)`,
		`INSERT INTO subtitle_inspections VALUES(42,'sha',10,1,'hash','{}')`,
		`INSERT INTO subtitle_candidates VALUES('candidate',42,'{}','generation',1,'sha',9999999999)`,
		`INSERT INTO subtitles(media_file_id,language,variant,source,updated_at) VALUES(42,'en','full','existing',1)`,
		`INSERT INTO subtitle_imports(id,media_file_id,language,variant,provider_file_id,destination,stage_name,sha256,size_bytes,phase,updated_at) VALUES('import',42,'en','forced',1,'Movies/video.en.srt','.stage','sha',1,'committed',1)`,
	} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(q, e)
		}
	}
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"media_files", "episode_files", "subtitle_jobs", "subtitle_inspections", "subtitle_candidates", "subtitles", "subtitle_imports"} {
		var n int
		if e = db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); e != nil || n != 1 {
			t.Fatal("reference lost", table, n, e)
		}
	}
	var origin string
	var id int
	if e = db.QueryRow(`SELECT id,origin FROM media_files WHERE download_id='d'`).Scan(&id, &origin); e != nil || id != 42 || origin != "download" {
		t.Fatal("file identity changed", id, origin, e)
	}
	if _, e = db.Exec(`INSERT INTO media_files(series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(2,'TV/existing.mp4',10,'sha','{}',1,'existing')`); e != nil {
		t.Fatal("local file required synthetic download", e)
	}
	rows, e := db.Query(`PRAGMA foreign_key_check`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("dangling references after migration")
	}
}
