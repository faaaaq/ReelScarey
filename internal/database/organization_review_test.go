package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// An organization upgrade must not reorganize any file or reinterpret the
// existing catalog while adding its schema. This fixture starts at released
// schema 10, including the path consumers most likely to be overlooked.
func TestOrganizationUpgradePreservesRegisteredMediaAndConsumers(t *testing.T) {
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
		if filepath.Base(name) >= "011_" {
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
	for _, query := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,monitored,status) VALUES(10,'movie',10,'Preserved movie',0,'available'),(20,'series',20,'Preserved show',1,'available')`,
		`INSERT INTO series_details(media_id,revision) VALUES(20,7)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,monitored,loaded_at) VALUES(21,20,210,1,'Season 1','','',2,1,'loaded')`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(22,21,220,1,'First','','',1),(23,21,230,2,'Second','','',0)`,
		`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('old-release',10,'Original release',512,'',X'0102',1)`,
		`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at) VALUES('old-download',10,'old-release','Original release','old-info-hash',X'0304','https://unused.invalid','/downloads','completed',1,2)`,
		`INSERT INTO media_files(id,movie_id,download_id,relative_path,size_bytes,sha256,probe_json,imported_at) VALUES(30,10,'old-download','Movies/Legacy name/video.mkv',512,'movie-sha','{}',3)`,
		`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(31,20,'TV Shows/Legacy show/double.mkv',1024,'show-sha','{}',4,'existing','double.mkv')`,
		`INSERT INTO episode_files(episode_id,media_file_id) VALUES(22,31),(23,31)`,
		`INSERT INTO import_operations(download_id,file_index,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,probe_json,mode,phase,updated_at) VALUES('old-download',-1,'source/video.mkv','Movies/Legacy name/video.mkv','.retained-stage',512,77,'movie-sha','{}','hardlink','committed',5)`,
		`INSERT INTO item_automation(media_id,profile_id,revision) VALUES(20,2,9)`,
		`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,sha256,provider_file_id,updated_at) VALUES(30,'en','forced','downloaded','Movies/Legacy name/video.en.forced.srt','subtitle-sha',60,6),(31,'en','signs','existing','TV Shows/Legacy show/double.eng.signs.ass','',NULL,7)`,
		`INSERT INTO subtitle_imports(id,media_file_id,language,variant,provider_file_id,destination,stage_name,sha256,size_bytes,phase,updated_at) VALUES('old-subtitle',30,'en','forced',60,'Movies/Legacy name/video.en.forced.srt','.retained-subtitle-stage','subtitle-sha',12,'committed',6)`,
		`INSERT INTO provider_secrets(provider,ciphertext,updated_at) VALUES('tmdb',X'08090A','old-secret-time')`,
		`INSERT INTO integration_configs(name,ciphertext,updated_at) VALUES('opensubtitles',X'0B0C0D','old-integration-time')`,
		`UPDATE settings SET movie_folder='Old Movies',series_folder='Old Shows',episode_naming='with-title',revision=8 WHERE id=1`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(query, err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	checks := []struct {
		query, want string
	}{
		{`SELECT kind||':'||title||':'||monitored||':'||status FROM media_items WHERE id=10`, "movie:Preserved movie:0:available"},
		{`SELECT kind||':'||title||':'||monitored||':'||status FROM media_items WHERE id=20`, "series:Preserved show:1:available"},
		{`SELECT revision FROM series_details WHERE media_id=20`, "7"},
		{`SELECT group_concat(number||':'||monitored,'|') FROM (SELECT number,monitored FROM episodes WHERE season_id=21 ORDER BY number)`, "1:1|2:0"},
		{`SELECT relative_path||':'||sha256||':'||download_id||':'||origin FROM media_files WHERE id=30`, "Movies/Legacy name/video.mkv:movie-sha:old-download:download"},
		{`SELECT relative_path||':'||sha256||':'||origin||':'||source_name FROM media_files WHERE id=31`, "TV Shows/Legacy show/double.mkv:show-sha:existing:double.mkv"},
		{`SELECT group_concat(episode_id||':'||media_file_id,'|') FROM (SELECT * FROM episode_files ORDER BY episode_id)`, "22:31|23:31"},
		{`SELECT source_path||':'||destination_path||':'||phase||':'||mode FROM import_operations WHERE download_id='old-download'`, "source/video.mkv:Movies/Legacy name/video.mkv:committed:hardlink"},
		{`SELECT profile_id||':'||revision FROM item_automation WHERE media_id=20`, "2:9"},
		{`SELECT relative_path||':'||sha256||':'||provider_file_id FROM subtitles WHERE media_file_id=30`, "Movies/Legacy name/video.en.forced.srt:subtitle-sha:60"},
		{`SELECT relative_path||':'||source FROM subtitles WHERE media_file_id=31`, "TV Shows/Legacy show/double.eng.signs.ass:existing"},
		{`SELECT destination||':'||phase FROM subtitle_imports WHERE id='old-subtitle'`, "Movies/Legacy name/video.en.forced.srt:committed"},
		{`SELECT hex(ciphertext)||':'||updated_at FROM provider_secrets WHERE provider='tmdb'`, "08090A:old-secret-time"},
		{`SELECT hex(ciphertext)||':'||updated_at FROM integration_configs WHERE name='opensubtitles'`, "0B0C0D:old-integration-time"},
		{`SELECT movie_folder||':'||series_folder||':'||episode_naming||':'||revision FROM settings WHERE id=1`, "Old Movies:Old Shows:with-title:8"},
	}
	for _, check := range checks {
		var got string
		if err = db.QueryRow(check.query).Scan(&got); err != nil || got != check.want {
			t.Errorf("upgrade changed saved state: %s\ngot %q, want %q, error %v", check.query, got, check.want, err)
		}
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		t.Fatal("upgrade broke a foreign key", rows.Err())
	}
}
