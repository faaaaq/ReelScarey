CREATE TABLE music_playback_marks (
    user_id INTEGER NOT NULL REFERENCES music_server_users(id) ON DELETE CASCADE,
    kind TEXT NOT NULL CHECK(kind IN ('song','album','artist')),
    object_id INTEGER NOT NULL,
    starred_at INTEGER NOT NULL DEFAULT 0,
    rating INTEGER NOT NULL DEFAULT 0 CHECK(rating BETWEEN 0 AND 5),
    updated_at INTEGER NOT NULL,
    PRIMARY KEY(user_id,kind,object_id)
);
CREATE TABLE music_playlists (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES music_server_users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    comment TEXT NOT NULL DEFAULT '',
    public INTEGER NOT NULL DEFAULT 0 CHECK(public IN (0,1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX music_playlists_owner ON music_playlists(user_id,id);
CREATE INDEX music_playlists_public ON music_playlists(public,updated_at DESC,id DESC);
CREATE TABLE music_playlist_entries (
    id INTEGER PRIMARY KEY,
    playlist_id INTEGER NOT NULL REFERENCES music_playlists(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK(position > 0),
    music_file_id INTEGER NOT NULL REFERENCES music_files(id) ON DELETE CASCADE,
    UNIQUE(playlist_id,position)
);
CREATE TABLE music_play_queue (
    user_id INTEGER PRIMARY KEY REFERENCES music_server_users(id) ON DELETE CASCADE,
    current_file_id INTEGER REFERENCES music_files(id) ON DELETE SET NULL,
    current_index INTEGER CHECK(current_index IS NULL OR current_index >= 0),
    position_ms INTEGER NOT NULL DEFAULT 0 CHECK(position_ms >= 0),
    changed_at INTEGER NOT NULL,
    changed_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE music_play_queue_entries (
    user_id INTEGER NOT NULL REFERENCES music_server_users(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK(position > 0),
    music_file_id INTEGER NOT NULL REFERENCES music_files(id) ON DELETE CASCADE,
    PRIMARY KEY(user_id,position)
);
CREATE TABLE music_play_events (
    id INTEGER PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES music_server_users(id) ON DELETE CASCADE,
    music_file_id INTEGER NOT NULL REFERENCES music_files(id) ON DELETE CASCADE,
    played_at INTEGER NOT NULL,
    client TEXT NOT NULL DEFAULT ''
);
CREATE INDEX music_play_events_user ON music_play_events(user_id,played_at DESC,id DESC);
CREATE INDEX music_play_events_file ON music_play_events(user_id,music_file_id,played_at DESC);
CREATE TABLE music_now_playing (
    user_id INTEGER PRIMARY KEY REFERENCES music_server_users(id) ON DELETE CASCADE,
    music_file_id INTEGER NOT NULL REFERENCES music_files(id) ON DELETE CASCADE,
    started_at INTEGER NOT NULL,
    client TEXT NOT NULL DEFAULT ''
);
CREATE INDEX music_now_playing_started ON music_now_playing(started_at DESC,user_id);

-- Playback marks are polymorphic, so SQLite cannot declare one conventional
-- foreign key for object_id. Remove marks atomically with their catalog object
-- instead of leaving stale IDs that could attach to a later reused row ID.
CREATE TRIGGER music_playback_marks_song_cleanup
AFTER DELETE ON music_files
BEGIN
    DELETE FROM music_playback_marks WHERE kind='song' AND object_id=OLD.id;
END;
CREATE TRIGGER music_playback_marks_album_cleanup
AFTER DELETE ON music_albums
BEGIN
    DELETE FROM music_playback_marks WHERE kind='album' AND object_id=OLD.id;
END;
CREATE TRIGGER music_playback_marks_artist_cleanup
AFTER DELETE ON music_artists
BEGIN
    DELETE FROM music_playback_marks WHERE kind='artist' AND object_id=OLD.id;
END;
