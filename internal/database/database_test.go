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
	if name != "Persistent cinema" || journal != "wal" || foreignKeys != 1 || timeout != 5000 || count != 4 {
		t.Fatalf("persistence/pragmas failed: %s %s %d %d %d", name, journal, foreignKeys, timeout, count)
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
	files["migrations/005_broken.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE rollback_probe (id INTEGER); INSERT INTO nonexistent VALUES (1);")}
	if err := migrate(ctx, db, files); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'rollback_probe'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("DDL not rolled back: %v, %d", err, count)
	}
	version, err := Version(ctx, db)
	if err != nil || version != 4 {
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
