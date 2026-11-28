package database_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/faaaaq/ReelScarey/internal/database"
	_ "modernc.org/sqlite"
)

func TestPopulatedDatabaseUpgradeAddsEvidenceAndPlaybackState(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO music_artists(id,musicbrainz_id,name,monitored,status,created_at,updated_at) VALUES(1,'artist-local','Owned artist',1,'available',1,1);
		INSERT INTO music_albums(id,artist_id,musicbrainz_id,title,monitored,status,created_at,updated_at) VALUES(2,1,'album-local','Owned album',1,'available',1,1);
		INSERT INTO music_tracks(id,album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at) VALUES(3,2,'track-local','recording-local',1,1,'Owned song',200000,1,1);
		INSERT INTO music_files(id,artist_id,album_id,track_id,root,relative_path,size_bytes,sha256,format,duration_ms,tag_json,imported_at,origin,source_name) VALUES(1,1,2,3,'Music','owned.mp3',10,'sha','mp3',200000,'{}',1,'existing','owned.mp3');
		INSERT INTO music_server_users(id,username,password_sealed,created_at,updated_at) VALUES(1,'owner',X'00',1,1);
		INSERT INTO music_acquisition_jobs(id,source_url,source_kind,title,root,format,state,track_count,created_at,updated_at) VALUES(1,'https://catalog.invalid/album','album','Owned album','Music','source','failed',1,1,1);
		INSERT INTO music_acquisition_tracks(id,job_id,position,title,artist,album,track_number,disc_number,duration_ms,source_url,state,created_at,updated_at) VALUES(1,1,1,'Owned song','Owned artist','Owned album',1,1,200000,'https://origin.invalid/song','failed',1,1)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}

	raw, err := sql.Open("sqlite", filepath.Join(dir, "reelscarey.db"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`PRAGMA foreign_keys=OFF;
		DELETE FROM schema_migrations WHERE version>=109;
		ALTER TABLE music_artists DROP COLUMN editor_fields;
		ALTER TABLE music_artists DROP COLUMN custom_artwork;
		ALTER TABLE music_artists DROP COLUMN custom_artwork_type;
		ALTER TABLE music_artists DROP COLUMN custom_artwork_key;
		ALTER TABLE music_albums DROP COLUMN editor_fields;
		ALTER TABLE music_albums DROP COLUMN custom_artwork;
		ALTER TABLE music_albums DROP COLUMN custom_artwork_type;
		ALTER TABLE music_albums DROP COLUMN custom_artwork_key;
		ALTER TABLE music_tracks DROP COLUMN editor_fields;
		ALTER TABLE music_acquisition_jobs DROP COLUMN audio_source;
		ALTER TABLE music_preferences DROP COLUMN download_source;
		ALTER TABLE music_preferences DROP COLUMN tidal_quality;
		ALTER TABLE music_albums DROP COLUMN title_overridden;
		ALTER TABLE music_albums DROP COLUMN year_overridden;
		ALTER TABLE music_artists DROP COLUMN name_overridden;
		ALTER TABLE music_edit_jobs DROP COLUMN override_fields;
		DROP INDEX music_acquisition_target_track;
		ALTER TABLE music_acquisition_tracks DROP COLUMN target_album_id;
		ALTER TABLE music_acquisition_tracks DROP COLUMN target_track_id;
		ALTER TABLE music_acquisition_tracks DROP COLUMN recording_id;
		ALTER TABLE music_acquisition_tracks DROP COLUMN musicbrainz_track_id;
		ALTER TABLE music_acquisition_tracks DROP COLUMN release_group_id;
		ALTER TABLE music_acquisition_tracks DROP COLUMN release_id;
		ALTER TABLE music_acquisition_tracks DROP COLUMN version_qualifiers;
		ALTER TABLE music_acquisition_tracks DROP COLUMN candidate_evidence_json;
		ALTER TABLE music_acquisition_tracks DROP COLUMN selected_source_id;
		ALTER TABLE music_acquisition_tracks DROP COLUMN selected_source_url;
		DROP TABLE music_acquisition_attempts;
		DROP TRIGGER music_playback_marks_song_cleanup;
		DROP TRIGGER music_playback_marks_album_cleanup;
		DROP TRIGGER music_playback_marks_artist_cleanup;
		DROP TABLE music_playback_marks;
		DROP TABLE music_library_playlist_entries;
		DROP TABLE music_library_playlists;
		DROP TABLE music_playlist_entries;
		DROP TABLE music_playlists;
		DROP TABLE music_play_queue_entries;
		DROP TABLE music_play_queue;
		DROP TABLE music_play_events;
		DROP TABLE music_now_playing;
		DROP TABLE music_upgrade_active;
		DROP TABLE music_upgrade_items;
		DROP TABLE music_upgrade_jobs;
		DROP TABLE music_upgrade_rate;
		DROP TABLE account_sessions;
		DROP TABLE accounts;
		PRAGMA foreign_keys=ON`)
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err = raw.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := database.Open(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	version, err := database.Version(ctx, upgraded)
	if err != nil || version != 117 {
		t.Fatalf("schema version = %d, want 117: %v", version, err)
	}
	var title, origin, evidence string
	var duration int
	if err = upgraded.QueryRow(`SELECT a.title,t.duration_ms,t.source_url,t.candidate_evidence_json FROM music_acquisition_tracks t JOIN music_albums a ON a.id=2 WHERE t.id=1`).Scan(&title, &duration, &origin, &evidence); err != nil {
		t.Fatal(err)
	}
	if title != "Owned album" || duration != 200000 || origin != "https://origin.invalid/song" || evidence != "[]" {
		t.Fatalf("populated row changed during upgrade: title=%q duration=%d origin=%q evidence=%q", title, duration, origin, evidence)
	}
	if _, err = upgraded.Exec(`INSERT INTO music_playback_marks(user_id,kind,object_id,starred_at,rating,updated_at) VALUES(1,'song',1,10,5,10)`); err != nil {
		t.Fatal(err)
	}
	var rating int
	if err = upgraded.QueryRow(`SELECT rating FROM music_playback_marks WHERE user_id=1 AND kind='song' AND object_id=1`).Scan(&rating); err != nil || rating != 5 {
		t.Fatalf("new playback state is unavailable: rating=%d err=%v", rating, err)
	}
}
