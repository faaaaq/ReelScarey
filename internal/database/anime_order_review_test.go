package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestReviewAnimeOrderUpgradePreservesExplicitSearchAndQueuedCanonicalIDs(t *testing.T) {
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
	prior := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "014_" {
			continue
		}
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		prior[name] = &fstest.MapFile{Data: body}
	}
	if err = migrate(ctx, db, prior); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title,category,category_revision,monitored) VALUES(1,'series',100,'Preserved Anime','anime',3,1)`,
		`INSERT INTO series_details(media_id,revision) VALUES(1,9)`,
		`INSERT INTO seasons(id,series_id,tmdb_id,number,title,overview,air_date,episode_count,loaded_at) VALUES(1,1,101,2,'Season 2','','2020-01-01',1,'loaded')`,
		`INSERT INTO episodes(id,season_id,tmdb_id,number,title,overview,air_date,monitored) VALUES(1,1,102,1,'Queued','','2020-01-01',1)`,
		`INSERT INTO anime_episode_numbers(episode_id,series_id,absolute_number) VALUES(1,1,25)`,
		`INSERT INTO anime_search_settings(media_id,mode,search_title,alternate_titles,revision) VALUES(1,'absolute','Explicit alias','["Explicit alias"]',7)`,
		`INSERT INTO item_automation(media_id,profile_id,revision) VALUES(1,2,6)`,
		`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at,scope) VALUES('release',1,'Original release',1024,'',X'0102',1,'tv:2:1')`,
		`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at,kind,organization_snapshot,anime_search_snapshot) VALUES('queued',1,'release','Original release','original-hash',X'0304','https://unused.invalid','/downloads','queued',1,2,'series','{"naming":"unchanged"}','{"policy":"immutable original alias"}')`,
		`INSERT INTO download_tv(download_id,series_tmdb_id,series_title,season_number,series_folder,naming) VALUES('queued',100,'Preserved Anime',2,'Anime/TV Shows','standard')`,
		`INSERT INTO download_targets(download_id,episode_id,tmdb_id,season_number,episode_number,title,absolute_number) VALUES('queued',1,102,2,1,'Queued',25)`,
		`INSERT INTO episode_downloads(episode_id,download_id) VALUES(1,'queued')`,
		`INSERT INTO media_files(id,series_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name) VALUES(3,1,'Anime/Season 02/025.mkv',2048,'preserved-sha','{}',3,'existing','original.mkv')`,
		`INSERT INTO episode_files(episode_id,media_file_id) VALUES(1,3)`,
		`INSERT INTO subtitles(media_file_id,language,variant,source,relative_path,updated_at) VALUES(3,'en','signs','existing','Anime/Season 02/025.eng.signs.ass',3)`,
	} {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(q, err)
		}
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ q, want string }{
		{`SELECT COUNT(*) FROM anime_orders`, "0"},
		{`SELECT COUNT(*) FROM anime_provider_links`, "0"},
		{`SELECT COUNT(*) FROM anime_order_entries`, "0"},
		{`SELECT COUNT(*) FROM anime_order_reviews`, "0"},
		{`SELECT category||':'||category_revision||':'||monitored FROM media_items WHERE id=1`, "anime:3:1"},
		{`SELECT revision FROM series_details WHERE media_id=1`, "9"},
		{`SELECT mode||':'||search_title||':'||alternate_titles||':'||revision FROM anime_search_settings WHERE media_id=1`, `absolute:Explicit alias:["Explicit alias"]:7`},
		{`SELECT absolute_number FROM anime_episode_numbers WHERE episode_id=1`, "25"},
		{`SELECT profile_id||':'||revision FROM item_automation WHERE media_id=1`, "2:6"},
		{`SELECT organization_snapshot||':'||anime_search_snapshot||':'||anime_order_snapshot||':'||state||':'||hex(payload) FROM downloads WHERE id='queued'`, `{"naming":"unchanged"}:{"policy":"immutable original alias"}::queued:0304`},
		{`SELECT episode_id||':'||tmdb_id||':'||season_number||':'||episode_number||':'||absolute_number FROM download_targets WHERE download_id='queued'`, "1:102:2:1:25"},
		{`SELECT download_id FROM episode_downloads WHERE episode_id=1`, "queued"},
		{`SELECT relative_path||':'||sha256 FROM media_files WHERE id=3`, "Anime/Season 02/025.mkv:preserved-sha"},
		{`SELECT media_file_id FROM episode_files WHERE episode_id=1`, "3"},
		{`SELECT relative_path FROM subtitles WHERE media_file_id=3`, "Anime/Season 02/025.eng.signs.ass"},
	} {
		var got string
		if err = db.QueryRow(check.q).Scan(&got); err != nil || got != check.want {
			t.Fatal("order upgrade changed existing data", check.q, got, check.want, err)
		}
	}
	rows, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() || rows.Err() != nil {
		t.Fatal("order upgrade damaged references", rows.Err())
	}
}
