package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestMigrationsPersistAndConfigureSQLite(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "folder with spaces # and café")
	db, err := Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE settings SET library_name = 'Persistent cinema' WHERE id = 1"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var name, journal string
	var foreignKeys, timeout, count int
	for _, query := range []struct {
		sql  string
		dest any
	}{
		{"SELECT library_name FROM settings WHERE id = 1", &name},
		{"PRAGMA journal_mode", &journal}, {"PRAGMA foreign_keys", &foreignKeys},
		{"PRAGMA busy_timeout", &timeout}, {"SELECT COUNT(*) FROM schema_migrations", &count},
	} {
		if err := db.QueryRow(query.sql).Scan(query.dest); err != nil {
			t.Fatal(err)
		}
	}
	if name != "Persistent cinema" || journal != "wal" || foreignKeys != 1 || timeout != 5000 || count != 57 {
		t.Fatalf("persistence/pragmas failed: %s %s %d %d %d", name, journal, foreignKeys, timeout, count)
	}
	var enabled, profiles int
	if err = db.QueryRow(`SELECT enabled,(SELECT COUNT(*) FROM quality_profiles) FROM search_schedule WHERE id=1`).Scan(&enabled, &profiles); err != nil || enabled != 0 || profiles != 2 {
		t.Fatal("automation must start paused with usable profiles", enabled, profiles, err)
	}
}

func TestTelevisionUpgradePreservesCatalogAndSecrets(t *testing.T) {
	ctx := context.Background()
	db, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	old := fstest.MapFS{}
	names, e := fs.Glob(migrationFiles, "migrations/*.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range names {
		if filepath.Base(name) >= "004_" {
			continue
		}
		body, e := fs.ReadFile(migrationFiles, name)
		if e != nil {
			t.Fatal(e)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	if e = migrate(ctx, db, old); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO media_items(kind,tmdb_id,title,monitored) VALUES('movie',42,'Existing movie',0),('series',42,'Existing series',0)`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO integration_configs(name,ciphertext) VALUES('qbittorrent',?)`, []byte("opaque encrypted test bytes")); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	var title string
	var monitored bool
	var revision int
	if e = db.QueryRow(`SELECT m.title,m.monitored,d.revision FROM media_items m JOIN series_details d ON d.media_id=m.id WHERE m.kind='series'`).Scan(&title, &monitored, &revision); e != nil || title != "Existing series" || monitored || revision != 1 {
		t.Fatal(title, monitored, revision, e)
	}
	var sealed []byte
	if e = db.QueryRow(`SELECT ciphertext FROM integration_configs WHERE name='qbittorrent'`).Scan(&sealed); e != nil || string(sealed) != "opaque encrypted test bytes" {
		t.Fatal("integration ciphertext changed", e)
	}
	var count int
	if e = db.QueryRow(`SELECT COUNT(*) FROM media_items`).Scan(&count); e != nil || count != 2 {
		t.Fatal("existing catalog changed", e)
	}
}

func TestMigrationRollbackAndDrift(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = &fstest.MapFile{Data: body}
	}
	files["migrations/053_broken.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE rollback_probe (id INTEGER); INSERT INTO nonexistent VALUES (1);")}
	if err := migrate(ctx, db, files); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'rollback_probe'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("DDL not rolled back: %v, %d", err, count)
	}
	version, err := Version(ctx, db)
	if err != nil || version != 57 {
		t.Fatalf("broken migration recorded: %d %v", version, err)
	}
	files["migrations/001_foundation.sql"] = &fstest.MapFile{Data: []byte("SELECT 1;")}
	if err := migrate(ctx, db, files); err == nil {
		t.Fatal("migration drift accepted")
	}
	if _, err := db.Exec("INSERT INTO schema_migrations(version, name, checksum) VALUES(99, 'future', 'hash')"); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err == nil {
		t.Fatal("newer database accepted")
	}
}

func TestSubtitleUpgradeRetainsEncryptedIntegrationsAndAutomation(t *testing.T) {
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
	names, e := fs.Glob(migrationFiles, "migrations/*.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range names {
		if filepath.Base(name) >= "007_" {
			continue
		}
		body, e := fs.ReadFile(migrationFiles, name)
		if e != nil {
			t.Fatal(e)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	if e = migrate(ctx, db, old); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO integration_configs(name,ciphertext,updated_at) VALUES('torznab',X'010203','original-time'),('qbittorrent',X'040506','original-time')`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE search_schedule SET enabled=1,interval_minutes=120,revision=8`); e != nil {
		t.Fatal(e)
	}
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	var ciphertext, timestamp, tracks string
	var enabled, revision, interval, subtitleEnabled int
	if e = db.QueryRow(`SELECT hex(ciphertext),updated_at FROM integration_configs WHERE name='torznab'`).Scan(&ciphertext, &timestamp); e != nil || ciphertext != "010203" || timestamp != "original-time" {
		t.Fatal(ciphertext, timestamp, e)
	}
	if e = db.QueryRow(`SELECT enabled,revision,interval_minutes FROM search_schedule`).Scan(&enabled, &revision, &interval); e != nil || enabled != 1 || revision != 8 || interval != 120 {
		t.Fatal(enabled, revision, interval, e)
	}
	if e = db.QueryRow(`SELECT enabled,tracks FROM subtitle_preferences`).Scan(&subtitleEnabled, &tracks); e != nil || subtitleEnabled != 0 || tracks != `[{"language":"en","variant":"full"},{"language":"en","variant":"signs"},{"language":"en","variant":"forced"}]` {
		t.Fatal(subtitleEnabled, tracks, e)
	}
	if _, e = db.Exec(`INSERT INTO integration_configs(name,ciphertext) VALUES('opensubtitles',X'070809')`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO integration_configs(name,ciphertext) VALUES('unimplemented',X'070809')`); e == nil {
		t.Fatal("provider constraint lost")
	}
}

func TestTVDownloadMigrationPreservesMovieImportJournals(t *testing.T) {
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
	names, e := fs.Glob(migrationFiles, "migrations/*.sql")
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range names {
		if filepath.Base(name) >= "005_" {
			continue
		}
		b, e := fs.ReadFile(migrationFiles, name)
		if e != nil {
			t.Fatal(e)
		}
		old[name] = &fstest.MapFile{Data: b}
	}
	if e = migrate(ctx, db, old); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{
		`INSERT INTO media_items(id,kind,tmdb_id,title) VALUES(1,'movie',1,'Completed'),(2,'movie',2,'In progress')`,
		`INSERT INTO release_candidates(id,movie_id,title,size_bytes,published_at,payload,expires_at) VALUES('r1',1,'one',10,'',X'0102',10),('r2',2,'two',10,'',X'0304',10)`,
		`INSERT INTO downloads(id,movie_id,release_id,title,info_hash,payload,client_url,save_path,state,created_at,updated_at) VALUES('j1',1,'r1','one','hash1',X'010203','http://test','/test','completed',1,2),('j2',2,'r2','two','hash2',X'040506','http://test','/test','importing',3,4)`,
		`INSERT INTO import_operations(download_id,source_path,destination_path,stage_name,size_bytes,source_mtime,sha256,probe_json,phase,mode,updated_at) VALUES('j1','source-one','Movies/one.mkv','.reelscarey-j1.part',10,50,'first-hash','{}','committed','copy',2),('j2','source-two','Movies/two.mkv','.reelscarey-j2.part',10,60,'second-hash','{}','staged','hardlink',4)`,
		`INSERT INTO media_files(id,movie_id,download_id,relative_path,size_bytes,sha256,probe_json,imported_at) VALUES(55,1,'j1','Movies/one.mkv',10,'first-hash','{}',2)`,
	} {
		if _, e = db.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	if e = Migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	var index int
	var mode, phase, source string
	if e = db.QueryRow(`SELECT file_index,mode,phase,source_path FROM import_operations WHERE download_id='j2'`).Scan(&index, &mode, &phase, &source); e != nil || index != -1 || mode != "hardlink" || phase != "staged" || source != "source-two" {
		t.Fatal(index, mode, phase, source, e)
	}
	var fileID int64
	if e = db.QueryRow(`SELECT id FROM media_files WHERE movie_id=1 AND download_id='j1' AND file_index=-1 AND relative_path='Movies/one.mkv' AND sha256='first-hash'`).Scan(&fileID); e != nil || fileID != 55 {
		t.Fatal("movie registration changed", e)
	}
	var payload []byte
	if e = db.QueryRow(`SELECT payload FROM downloads WHERE id='j2'`).Scan(&payload); e != nil || len(payload) != 3 || payload[0] != 4 {
		t.Fatal("ciphertext changed", e)
	}
	rows, e := db.Query(`PRAGMA foreign_key_check`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("foreign key consistency lost")
	}
}
