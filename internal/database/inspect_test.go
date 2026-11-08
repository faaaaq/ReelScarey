package database

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInspectionNeverCreatesAndRejectsMigrationDrift(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	file := filepath.Join(dir, "missing.db")
	if _, e := OpenExisting(ctx, file, false); e == nil {
		t.Fatal("missing database opened")
	}
	if _, e := os.Stat(file); !os.IsNotExist(e) {
		t.Fatal("missing database created")
	}
	db, e := Open(ctx, dir)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if v, e := Inspect(ctx, db); e != nil || v != 65 {
		t.Fatal(v, e)
	}
	if _, e = db.Exec(`UPDATE schema_migrations SET checksum='drift' WHERE version=1`); e != nil {
		t.Fatal(e)
	}
	if _, e = Inspect(ctx, db); e == nil {
		t.Fatal("changed migration accepted")
	}
}
