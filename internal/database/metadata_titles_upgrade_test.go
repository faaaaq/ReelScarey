package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMetadataTitleUpgradeQueuesOlderRegistrationsAndCatalogCorrections(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	old := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "106_" {
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
	_, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',1,'Movie');
	 INSERT INTO media_files(id,movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,1,'Movies/old.mkv',1,'original','{}',1,'existing');
	 INSERT INTO music_artists(id,musicbrainz_id,name,created_at,updated_at) VALUES(1,'artist','Artist',1,1);
	 INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,created_at,updated_at) VALUES(1,1,'album','Album',1,1);
	 INSERT INTO music_tracks(id,album_id,disc,number,title,created_at,updated_at) VALUES(1,1,1,1,'Track',1,1);
	 INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,imported_at) VALUES(1,1,1,1,'Music','Music/old.flac',1,1),(2,1,1,NULL,'Music','Music/unmatched.flac',1,1);
	 DELETE FROM video_title_updates; DELETE FROM music_title_updates;`)
	if err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`SELECT count(*) FROM video_title_updates WHERE media_file_id=1`,
		`SELECT count(*) FROM music_title_updates WHERE music_file_id=1`,
		`SELECT count(*) FROM media_files WHERE sha256='original' AND relative_path='Movies/old.mkv'`,
	} {
		var count int
		if err = db.QueryRow(query).Scan(&count); err != nil || count != 1 {
			t.Fatalf("%s: %d %v", query, count, err)
		}
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM music_title_updates WHERE music_file_id=2`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unmatched file queued: %d %v", count, err)
	}
	if _, err = db.Exec(`UPDATE music_title_updates SET next_attempt=9999999999; UPDATE music_albums SET title='Corrected album' WHERE id=1; UPDATE music_artists SET name='Corrected artist' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM music_title_updates WHERE music_file_id=1 AND revision=3 AND next_attempt=0`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("corrections not queued: %d %v", count, err)
	}
}
