package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/faaaaq/ReelScarey/internal/quality"
)

// TestAnimeDubExclusionUpgrade checks that migration 093 upgrades the seeded
// English-dub rule only while an installation still carries the previous
// default text, and leaves a customized rule exactly as its owner saved it.
func TestAnimeDubExclusionUpgrade(t *testing.T) {
	ctx := context.Background()
	old := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "093_" {
			continue
		}
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	seed := func(t *testing.T) *sql.DB {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		if err := migrate(ctx, db, old); err != nil {
			t.Fatal(err)
		}
		return db
	}
	// An installation still on the previous default rule is upgraded with the
	// revision bumped like a saved edit.
	db := seed(t)
	defer db.Close()
	var rule string
	var revision int
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil || rule == quality.DefaultDubRule || revision != 1 {
		t.Fatal("previous default rule not seeded", revision, err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil || rule != quality.DefaultDubRule || revision != 2 {
		t.Fatal("seeded default rule not upgraded", revision, err)
	}
	if version, err := Version(ctx, db); err != nil || version != len(names) {
		t.Fatal("unexpected schema version", version, len(names), err)
	}
	// A customized rule is left exactly as its owner saved it.
	db = seed(t)
	defer db.Close()
	if _, err := db.Exec(`UPDATE anime_preferences SET dub_rule='Dual Audio and nothing else',revision=7 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil || rule != "Dual Audio and nothing else" || revision != 7 {
		t.Fatal("customized rule changed", revision, err)
	}
}
