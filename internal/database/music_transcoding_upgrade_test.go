package database

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// The transcoding migration is additive: preferences, registered files and
// server users keep their history and gain the new columns with their
// documented defaults, and the three new tables exist with their indexes.
func TestMusicTranscodingUpgradeIsAdditive(t *testing.T) {
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
		if filepath.Base(name) >= "103_" {
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
	// Migration 063 seeds a single preferences row; the upgrade test mutates
	// it so the existing history is preserved through the migration.
	if _, err = db.Exec(`UPDATE music_preferences SET allow_lossless=0, min_bitrate=192, preferred_formats='FLAC,MP3', revision=7, updated_at=unixepoch() WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"music_quality_profiles", "music_conversion_jobs", "music_stream_cache"} {
		var count int
		if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil || count != 1 {
			t.Fatal("missing transcoding table", table, count, err)
		}
	}
	var index int
	if err = db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_music_conversion_jobs_state'`).Scan(&index); err != nil || index != 1 {
		t.Fatal("missing conversion jobs index", index, err)
	}

	// The existing preference row keeps its history and gains the new columns
	// with the documented defaults.
	var allow, revision, transcode int
	var minBitrate, cache, maxConversions int
	var formats, downloadProfile, streamingQuality, retention, fallback string
	if err = db.QueryRow(`SELECT allow_lossless,min_bitrate,preferred_formats,download_profile,streaming_quality,
		original_retention,fallback_policy,transcoding_enabled,cache_limit_mb,max_conversions,revision
		FROM music_preferences WHERE id=1`).
		Scan(&allow, &minBitrate, &formats, &downloadProfile, &streamingQuality, &retention, &fallback, &transcode, &cache, &maxConversions, &revision); err != nil {
		t.Fatal(err)
	}
	if allow != 0 || minBitrate != 192 || formats != "FLAC,MP3" || revision != 7 {
		t.Fatal("existing preference row changed", allow, minBitrate, formats, revision)
	}
	if downloadProfile != "original" || streamingQuality != "balanced" || retention != "keep" || fallback != "best" {
		t.Fatal("unexpected preference defaults", downloadProfile, streamingQuality, retention, fallback)
	}
	if transcode != 1 || cache != 512 || maxConversions != 2 {
		t.Fatal("unexpected preference defaults", transcode, cache, maxConversions)
	}

	// Registered files and server users keep their rows and gain the new
	// columns with their defaults.
	var relativePath, format, variantRole, variantProfile string
	var sizeBytes int
	if err = db.QueryRow(`SELECT relative_path,size_bytes,format,variant_role,variant_profile FROM music_files WHERE relative_path='Artist/Historic/01 Track.flac'`).
		Scan(&relativePath, &sizeBytes, &format, &variantRole, &variantProfile); err != nil {
		t.Fatal(err)
	}
	if relativePath != "Artist/Historic/01 Track.flac" || sizeBytes != 123 || format != "FLAC" {
		t.Fatal("existing file row changed", relativePath, sizeBytes, format)
	}
	if variantRole != "primary" || variantProfile != "" {
		t.Fatal("unexpected variant defaults", variantRole, variantProfile)
	}
	var username, streamQuality, userDownloadProfile string
	if err = db.QueryRow(`SELECT username,stream_quality,download_profile FROM music_server_users WHERE username='listener'`).
		Scan(&username, &streamQuality, &userDownloadProfile); err != nil {
		t.Fatal(err)
	}
	if username != "listener" || streamQuality != "" || userDownloadProfile != "" {
		t.Fatal("existing server user row changed", username, streamQuality, userDownloadProfile)
	}
	var albumProfile string
	if err = db.QueryRow(`SELECT download_profile FROM music_albums WHERE id=1`).Scan(&albumProfile); err != nil {
		t.Fatal("album override column missing", err)
	}
	if albumProfile != "" {
		t.Fatal("album override default changed", albumProfile)
	}
	// The custom profile table is empty: builtins are code, never DB rows.
	var customRows int
	if err = db.QueryRow(`SELECT COUNT(*) FROM music_quality_profiles`).Scan(&customRows); err != nil {
		t.Fatal(err)
	}
	if customRows != 0 {
		t.Fatal("custom profile rows should be empty", customRows)
	}
	if _, err = db.Exec(`INSERT INTO music_quality_profiles(name,key,codec,container,bitrate_kbps,vbr,compression_level,require_lossless,keep_original,custom,created_at,updated_at)
		VALUES('Home','opus-96','opus','opus',96,1,0,0,0,1,unixepoch(),unixepoch())`); err != nil {
		t.Fatal("custom profile insert rejected", err)
	}
	if _, err = db.Exec(`INSERT INTO music_conversion_jobs(kind,profile,created_at,updated_at)
		VALUES('download','opus-128',unixepoch(),unixepoch())`); err != nil {
		t.Fatal("conversion job insert rejected", err)
	}
	if _, err = db.Exec(`INSERT INTO music_stream_cache(file_id,profile_key,relative_path,size_bytes,codec,created_at,last_used_at)
		VALUES(1,'opus-128','a/1.opus',456,'opus',unixepoch(),unixepoch())`); err != nil {
		t.Fatal("stream cache insert rejected", err)
	}
	if _, err = db.Exec(`INSERT INTO music_stream_cache(file_id,profile_key,relative_path,size_bytes,codec,created_at,last_used_at)
		VALUES(1,'opus-128','a/2.opus',456,'opus',unixepoch(),unixepoch())`); err == nil {
		t.Fatal("duplicate stream cache key accepted")
	}
}
