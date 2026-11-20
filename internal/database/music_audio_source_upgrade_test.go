package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// Migration 113 is additive: existing jobs retain automatic source behavior,
// existing music preferences retain their values, and neither provider tokens
// nor owner media are involved in the upgrade.
func TestMusicAudioSourceSettingsUpgradeIsAdditive(t *testing.T) {
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
		if filepath.Base(name) >= "113_" {
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
	if _, err = db.Exec(`INSERT INTO music_acquisition_jobs(id,title,root,state,track_count,completed_count,error,created_at,updated_at)
		VALUES(1,'Historic','Music','failed',2,0,'No tracks completed.',unixepoch(),unixepoch())`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE music_preferences SET download_profile='opus-balanced',streaming_quality='premium' WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	var title, jobSource string
	if err = db.QueryRow(`SELECT title,audio_source FROM music_acquisition_jobs WHERE id=1`).Scan(&title, &jobSource); err != nil {
		t.Fatal(err)
	}
	if title != "Historic" || jobSource != "" {
		t.Fatalf("historic acquisition changed: title=%q source=%q", title, jobSource)
	}
	var profile, streaming, source, quality string
	if err = db.QueryRow(`SELECT download_profile,streaming_quality,download_source,tidal_quality FROM music_preferences WHERE id=1`).
		Scan(&profile, &streaming, &source, &quality); err != nil {
		t.Fatal(err)
	}
	if profile != "opus-balanced" || streaming != "premium" || source != "" || quality != "" {
		t.Fatalf("preference upgrade changed existing values: profile=%q streaming=%q source=%q quality=%q", profile, streaming, source, quality)
	}
}
