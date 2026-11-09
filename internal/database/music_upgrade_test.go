package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// The music catalog migration is additive: existing catalog rows and foreign
// keys must be untouched, and the new tables must enforce their checks.
func TestMusicUpgradeIsAdditive(t *testing.T) {
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
		if filepath.Base(name) >= "059_" {
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
	if _, err = db.Exec(`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',100,'Preserved')`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var title string
	if err = db.QueryRow(`SELECT title FROM media_items WHERE id=1`).Scan(&title); err != nil || title != "Preserved" {
		t.Fatal("existing catalog row changed", title, err)
	}
	for _, table := range []string{"music_artists", "music_albums", "music_tracks", "music_fingerprints", "musicbrainz_cache", "musicbrainz_provider_state"} {
		var count int
		if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatal("missing music table", table, count, err)
		}
	}
	var cooldownRows int
	if err = db.QueryRow(`SELECT COUNT(*) FROM musicbrainz_provider_state WHERE id=1 AND retry_until=0 AND message=''`).Scan(&cooldownRows); err != nil || cooldownRows != 1 {
		t.Fatal("missing seeded provider state", cooldownRows, err)
	}
	var expiryIndex int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='musicbrainz_cache_expiry'`).Scan(&expiryIndex); err != nil || expiryIndex != 1 {
		t.Fatal("missing cache expiry index", expiryIndex, err)
	}
	if _, err = db.Exec(`INSERT INTO musicbrainz_cache(key,payload,expires_at,negative) VALUES('k','{}',9999999999,0)`); err != nil {
		t.Fatal("cache insert rejected", err)
	}
	if _, err = db.Exec(`INSERT INTO integration_configs(name,ciphertext) VALUES('acoustid',x'00')`); err != nil {
		t.Fatal("acoustid integration name rejected", err)
	}
	if _, err = db.Exec(`INSERT INTO integration_configs(name,ciphertext) VALUES('musicbrainz',x'00')`); err != nil {
		t.Fatal("musicbrainz integration name rejected", err)
	}
	if _, err = db.Exec(`DELETE FROM integration_configs WHERE name IN ('acoustid','musicbrainz')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_artists(musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES('a74b1b7f-71a5-4011-9441-d0b5e4122711','Artist',1,'missing',1,1)`); err != nil {
		t.Fatal(err)
	}
	var artistID int64
	if err = db.QueryRow(`SELECT id FROM music_artists`).Scan(&artistID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_albums(artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(?,'b1392450-e666-3926-a536-22c65f834433','Album',1,'missing',1,1)`, artistID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_albums(artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(?,'b1392450-e666-3926-a536-22c65f834433','Duplicate',1,'missing',1,1)`, artistID); err == nil {
		t.Fatal("duplicate album accepted")
	}
	if _, err = db.Exec(`DELETE FROM music_artists WHERE id=?`, artistID); err != nil {
		t.Fatal(err)
	}
	var albums int
	if err = db.QueryRow(`SELECT COUNT(*) FROM music_albums`).Scan(&albums); err != nil || albums != 0 {
		t.Fatal("album cascade failed", albums, err)
	}
}
