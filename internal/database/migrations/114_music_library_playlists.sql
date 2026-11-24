-- Administrator library playlists are independent of the per-user Subsonic
-- playlists. Entries retain canonical catalog track IDs so losing a registered
-- file makes a track unplayable without silently removing it from the list.
CREATE TABLE music_library_playlists (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 100 AND name=trim(name)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX music_library_playlists_name
    ON music_library_playlists(name COLLATE NOCASE,id);

CREATE TABLE music_library_playlist_entries (
    id INTEGER PRIMARY KEY,
    playlist_id INTEGER NOT NULL REFERENCES music_library_playlists(id) ON DELETE CASCADE,
    track_id INTEGER NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK(position > 0),
    UNIQUE(playlist_id,track_id),
    UNIQUE(playlist_id,position)
);
CREATE INDEX music_library_playlist_entries_track
    ON music_library_playlist_entries(track_id,playlist_id);

-- Catalog rewrites and direct track deletion both cascade playlist entries.
-- Fence clients holding the old membership before SQLite performs that cascade.
-- Ordinary playlist entry mutations advance the revision in their own domain
-- transaction and do not fire this music_tracks trigger.
DROP TRIGGER IF EXISTS music_library_playlist_entries_catalog_delete;
CREATE TRIGGER music_library_playlist_entries_catalog_delete
BEFORE DELETE ON music_tracks
FOR EACH ROW
BEGIN
    UPDATE music_library_playlists
    SET revision=revision+1,updated_at=unixepoch()
    WHERE id IN (
        SELECT playlist_id FROM music_library_playlist_entries
        WHERE track_id=OLD.id
    );
END;
