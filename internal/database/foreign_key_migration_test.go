package database

import (
	"context"
	"io/fs"
	"testing"
	"testing/fstest"
)

func TestMigrationRejectsForeignKeyDamageBeforeCommit(t *testing.T) {
	ctx := context.Background()
	db, e := Open(ctx, t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	files := fstest.MapFS{}
	names, _ := fs.Glob(migrationFiles, "migrations/*.sql")
	for _, name := range names {
		b, _ := fs.ReadFile(migrationFiles, name)
		files[name] = &fstest.MapFile{Data: b}
	}
	files["migrations/044_invalid_references.sql"] = &fstest.MapFile{Data: []byte(`PRAGMA defer_foreign_keys=ON; CREATE TABLE invalid_reference(id INTEGER REFERENCES media_items(id)); INSERT INTO invalid_reference VALUES(999999); PRAGMA defer_foreign_keys=OFF;`)}
	if e = migrate(ctx, db, files); e == nil {
		t.Fatal("invalid reference committed after deferred table rebuild")
	}
	var n int
	if e = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='invalid_reference'`).Scan(&n); e != nil || n != 0 {
		t.Fatal("failed migration was not rolled back", n, e)
	}
	if version, e := Version(ctx, db); e != nil || version != 54 {
		t.Fatal("invalid migration ledger", version, e)
	}
}
