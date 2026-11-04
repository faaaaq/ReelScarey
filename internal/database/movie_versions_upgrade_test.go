package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMovieVersionsUpgradePreservesFilesAndSubtitleHistory(t *testing.T) {
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
		if filepath.Base(name) >= "044_" {
			continue
		}
		body, e := fs.ReadFile(migrationFiles, name)
		if e != nil {
			t.Fatal(e)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	if err = migrate(ctx, db, old); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',100,'Two versions')`,
		`INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name,verification_json) VALUES(55,1,'Movies/old.mkv',100,'','{"width":1920,"height":800}',10,'existing','original release','{"Identity":"fixture"}')`,
		`INSERT INTO subtitle_jobs(id,media_file_id,created_at,updated_at) VALUES('job',55,1,2)`,
		`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,updated_at) VALUES(55,'en','full','existing','Movies/old.en.srt','saved-subtitle',10)`,
		`INSERT INTO subtitle_edits(rowid,id,media_file_id,language,variant,action,snapshot,content,old_sha,new_sha,size_bytes,cues,destination,stage_path,previous_path,state,phase,expires_at,created_at,lease_owner,lease_until) VALUES(87,'edit',55,'en','full','offset','saved snapshot',X'010203','old','new',3,1,'Movies/old.en.srt','stage','previous','running','staged',99,1,'lease',88)`,
		`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('release',1,'Replacement',0,'',X'00',9999999999)`,
		`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at) VALUES('replacement',1,'release','Replacement','unique-hash',X'00','http://fixture','/owned','queued',1,1)`,
		`INSERT INTO acquisition_replacements(source_kind,job_id,media_file_id,target_sha) VALUES('download','replacement',55,'old-file-hash')`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var resolution, id, rows int
	var source, observation, content, state, lease string
	if err = db.QueryRow(`SELECT movie_version FROM downloads WHERE id='replacement'`).Scan(&state); err != nil || state != "replacement" {
		t.Fatal("pending replacement lost its intent", state, err)
	}
	if err = db.QueryRow(`SELECT id,video_resolution,source_name,verification_json FROM media_files WHERE movie_id=1`).Scan(&id, &resolution, &source, &observation); err != nil || id != 55 || resolution != 1080 || source != "original release" || observation != `{"Identity":"fixture"}` {
		t.Fatal(id, resolution, source, observation, err)
	}
	if err = db.QueryRow(`SELECT rowid,hex(content),state,lease_owner FROM subtitle_edits WHERE id='edit'`).Scan(&id, &content, &state, &lease); err != nil || id != 87 || content != "010203" || state != "running" || lease != "lease" {
		t.Fatal(id, content, state, lease, err)
	}
	if err = db.QueryRow(`SELECT (SELECT COUNT(*) FROM subtitle_jobs WHERE media_file_id=55)+(SELECT COUNT(*) FROM subtitles WHERE media_file_id=55)`).Scan(&rows); err != nil || rows != 2 {
		t.Fatal(rows, err)
	}
	if _, err = db.Exec(`INSERT INTO media_files(movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,'Movies/new.mkv',200,'hash','{"width":3840,"height":1600}',11,'existing')`); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM media_files WHERE movie_id=1`).Scan(&rows); err != nil || rows != 2 {
		t.Fatal(rows, err)
	}
	if _, err = db.Exec(`INSERT INTO media_files(movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,'Movies/new.mkv',200,'hash','{}',11,'existing')`); err == nil {
		t.Fatal("duplicate path accepted")
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
}
