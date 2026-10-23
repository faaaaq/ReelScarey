package database

import (
	"context"
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
	if name != "Persistent cinema" || journal != "wal" || foreignKeys != 1 || timeout != 5000 || count != 2 {
		t.Fatalf("persistence/pragmas failed: %s %s %d %d %d", name, journal, foreignKeys, timeout, count)
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
	files["migrations/003_broken.sql"] = &fstest.MapFile{Data: []byte("CREATE TABLE rollback_probe (id INTEGER); INSERT INTO nonexistent VALUES (1);")}
	if err := migrate(ctx, db, files); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = 'rollback_probe'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("DDL not rolled back: %v, %d", err, count)
	}
	version, err := Version(ctx, db)
	if err != nil || version != 2 {
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
