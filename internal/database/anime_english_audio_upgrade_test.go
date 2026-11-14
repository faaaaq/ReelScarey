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

// TestAnimeEnglishAudioEvidenceUpgrade checks that migration 102 upgrades an
// installation still on the 093 default English-dub rule to the new default
// (which recognizes explicit "English Audio"/"Eng Audio" labels), while a
// customized rule is left untouched. The structural English-audio evidence
// gate in quality.DubRule.Qualifies applies regardless of the stored rule.
func TestAnimeEnglishAudioEvidenceUpgrade(t *testing.T) {
	ctx := context.Background()
	old := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "102_" {
			continue
		}
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	seed := func(t *testing.T) *sql.DB {
		t.Helper()
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
	// An installation still on the 093 default rule is upgraded with the
	// revision bumped like a saved edit.
	db := seed(t)
	defer db.Close()
	var rule string
	var revision int
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil {
		t.Fatal(err)
	}
	if rule == quality.DefaultDubRule {
		t.Fatal("test fixture already uses the new default")
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil || rule != quality.DefaultDubRule || revision != 3 {
		t.Fatal("default rule not upgraded", revision, err)
	}
	if version, err := Version(ctx, db); err != nil || version != len(names) {
		t.Fatal("unexpected schema version", version, len(names), err)
	}
	// A customized rule is left exactly as its owner saved it; it still gains
	// the structural evidence gate at runtime.
	db = seed(t)
	defer db.Close()
	if _, err := db.Exec(`UPDATE anime_preferences SET dub_rule='Dual Audio and nothing else',revision=9 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT dub_rule,revision FROM anime_preferences WHERE id=1`).Scan(&rule, &revision); err != nil || rule != "Dual Audio and nothing else" || revision != 9 {
		t.Fatal("customized rule changed", revision, err)
	}
}
