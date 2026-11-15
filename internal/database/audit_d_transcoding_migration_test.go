package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// Independent audit of migration 103: the upgrade from a populated
// pre-103 database must be idempotent, leave no foreign-key violations,
// and the new profile table must not allow two custom profiles to share a
// name, because custom profiles are resolved by name.
func audit103OldFS(t *testing.T) fstest.MapFS {
	t.Helper()
	old := fstest.MapFS{}
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if filepath.Base(name) >= "103_" {
			continue
		}
		body, err := fs.ReadFile(migrationFiles, name)
		if err != nil {
			t.Fatal(err)
		}
		old[name] = &fstest.MapFile{Data: body}
	}
	return old
}

func TestAuditDMigration103ReRunAndForeignKeys(t *testing.T) {
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
	if err = migrate(ctx, db, audit103OldFS(t)); err != nil {
		t.Fatal(err)
	}
	// Populate every table migration 103 alters with pre-existing history.
	if _, err = db.Exec(`INSERT INTO music_artists(musicbrainz_id,name,monitored,status,created_at,updated_at)
		VALUES('a74b1b7f-71a5-4011-9441-d0b5e4122711','Artist',1,'missing',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_albums(artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at)
		VALUES(1,'b1392450-e666-3926-a536-22c65f834433','Historic',1,'missing',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_files(artist_id,album_id,relative_path,size_bytes,format,bitrate,imported_at)
		VALUES(1,1,'Artist/Historic/01 Track.flac',123,'FLAC',1000,unixepoch())`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_server_users(username,password_sealed,created_at,updated_at)
		VALUES('listener',x'00',1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE music_preferences SET revision=7 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}

	// Defaults on existing rows are verified directly, not by claim.
	var transcode, cache, maxConversions int
	var streamingQuality, retention, fallback string
	if err = db.QueryRow(`SELECT transcoding_enabled,streaming_quality,original_retention,fallback_policy,cache_limit_mb,max_conversions
		FROM music_preferences WHERE id=1`).
		Scan(&transcode, &streamingQuality, &retention, &fallback, &cache, &maxConversions); err != nil {
		t.Fatal(err)
	}
	if transcode != 1 || streamingQuality != "balanced" || retention != "keep" || fallback != "best" || cache != 512 || maxConversions != 2 {
		t.Fatal("unexpected pre-existing preference defaults", transcode, streamingQuality, retention, fallback, cache, maxConversions)
	}
	var variantRole string
	if err = db.QueryRow(`SELECT variant_role FROM music_files`).Scan(&variantRole); err != nil || variantRole != "primary" {
		t.Fatal("pre-existing file row must become a primary", variantRole, err)
	}

	// The migration runner's own foreign_key_check gate is re-asserted here so
	// the populated upgrade is independently proven referentially intact.
	checks, err := db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer checks.Close()
	if checks.Next() {
		t.Fatal("foreign key violations after migration 103")
	}
	if err = checks.Err(); err != nil {
		t.Fatal(err)
	}

	// Re-running the migration must be a no-op (checksum-ledger skip), not a
	// duplicate apply that fails or resets the new columns.
	if err = Migrate(ctx, db); err != nil {
		t.Fatal("migration 103 re-run must be idempotent", err)
	}
	if err = db.QueryRow(`SELECT transcoding_enabled FROM music_preferences WHERE id=1`).Scan(&transcode); err != nil || transcode != 1 {
		t.Fatal("re-run changed migrated defaults", transcode, err)
	}
}

func TestAuditDMusicQualityProfilesUniqueName(t *testing.T) {
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
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	insert := func(name string) error {
		_, err := db.Exec(`INSERT INTO music_quality_profiles(name,key,codec,container,bitrate_kbps,vbr,compression_level,require_lossless,keep_original,custom,created_at,updated_at)
			VALUES(?,'','opus','opus',96,1,0,0,0,1,unixepoch(),unixepoch())`, name)
		return err
	}
	if err = insert("Home"); err != nil {
		t.Fatal(err)
	}
	// Custom profiles resolve by name (ResolveProfileKey), so a case-variant
	// duplicate would silently resolve to the oldest row.
	if err = insert("home"); err == nil {
		t.Fatal("duplicate custom profile name accepted")
	}
	if err = insert("HOME"); err == nil {
		t.Fatal("case-variant duplicate custom profile name accepted")
	}
	var count int
	if err = db.QueryRow(`SELECT COUNT(*) FROM music_quality_profiles`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("unexpected profile row count", count)
	}
}
