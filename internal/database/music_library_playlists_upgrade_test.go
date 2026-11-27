package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMusicLibraryPlaylistsUpgradePreservesCatalogAndSubsonicState(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "reelscarey.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	old := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "114_" {
			continue
		}
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	if err := migrate(ctx, db, old); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,'artist-old','Owned artist',1,'available',1,1);
	INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,'album-old','Owned album',1,'available',1,1);
	INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at) VALUES(3,2,'track-old','recording-old',1,1,'Owned song',123000,1,1);
	INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,format,duration_ms,imported_at,origin) VALUES(4,1,2,3,'Music','owned.flac',100,'flac',123000,1,'existing');
	INSERT INTO music_server_users(id,username,password_sealed,created_at,updated_at) VALUES(5,'owner',X'00',1,1);
	INSERT INTO music_playlists(id,user_id,name,comment,public,created_at,updated_at) VALUES(6,5,'Subsonic playlist','kept',1,10,20);
	INSERT INTO music_playlist_entries(id,playlist_id,position,music_file_id) VALUES(7,6,1,4)`)
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if version, err := Version(ctx, db); err != nil || version != 116 {
		t.Fatalf("schema version = %d, %v", version, err)
	}
	var title, name, comment string
	var duration, subsonicPosition int
	if err := db.QueryRow(`SELECT t.title,t.duration_ms,p.name,p.comment,e.position FROM music_tracks t,music_playlists p,music_playlist_entries e WHERE t.id=3 AND p.id=6 AND e.id=7`).Scan(&title, &duration, &name, &comment, &subsonicPosition); err != nil {
		t.Fatal(err)
	}
	if title != "Owned song" || duration != 123000 || name != "Subsonic playlist" || comment != "kept" || subsonicPosition != 1 {
		t.Fatalf("old data changed: %q %d %q %q %d", title, duration, name, comment, subsonicPosition)
	}

	if _, err := db.Exec(`INSERT INTO music_library_playlists(id,name,created_at,updated_at) VALUES(20,'Library playlist',30,30);
		INSERT INTO music_library_playlist_entries(id,playlist_id,track_id,position) VALUES(21,20,3,1)`); err != nil {
		t.Fatal(err)
	}
	var revision int
	if err := db.QueryRow(`SELECT revision FROM music_library_playlists WHERE id=20`).Scan(&revision); err != nil || revision != 1 {
		t.Fatalf("default revision = %d, %v", revision, err)
	}
	if _, err := db.Exec(`INSERT INTO music_library_playlist_entries(playlist_id,track_id,position) VALUES(20,3,2)`); err == nil {
		t.Fatal("duplicate canonical track accepted")
	}
	if _, err := db.Exec(`INSERT INTO music_library_playlist_entries(playlist_id,track_id,position) VALUES(20,999,2)`); err == nil {
		t.Fatal("unknown canonical track accepted")
	}
	if _, err := db.Exec(`INSERT INTO music_library_playlist_entries(playlist_id,track_id,position) VALUES(20,3,0)`); err == nil {
		t.Fatal("zero position accepted")
	}
	if _, err := db.Exec(`DELETE FROM music_playlists WHERE id=6`); err != nil {
		t.Fatal(err)
	}
	var libraryCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_library_playlist_entries WHERE playlist_id=20 AND track_id=3`).Scan(&libraryCount); err != nil || libraryCount != 1 {
		t.Fatalf("Subsonic cascade touched library playlist: %d %v", libraryCount, err)
	}
	if _, err := db.Exec(`DELETE FROM music_tracks WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	var updatedAt int64
	if err := db.QueryRow(`SELECT revision,updated_at FROM music_library_playlists WHERE id=20`).Scan(&revision, &updatedAt); err != nil || revision != 2 || updatedAt <= 30 {
		t.Fatalf("catalog deletion did not fence playlist: revision=%d updated_at=%d err=%v", revision, updatedAt, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_library_playlist_entries WHERE playlist_id=20`).Scan(&libraryCount); err != nil || libraryCount != 0 {
		t.Fatalf("catalog deletion did not cascade canonical entry: %d %v", libraryCount, err)
	}
	if _, err := db.Exec(`DELETE FROM music_library_playlists WHERE id=20`); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM music_library_playlist_entries WHERE playlist_id=20`).Scan(&libraryCount); err != nil || libraryCount != 0 {
		t.Fatalf("library playlist cascade failed: %d %v", libraryCount, err)
	}
}
