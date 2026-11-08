CREATE TABLE music_scans (
    id INTEGER PRIMARY KEY,
    root TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('scanning','review','completed','failed','cancelled')),
    file_count INTEGER NOT NULL DEFAULT 0,
    matched_count INTEGER NOT NULL DEFAULT 0,
    unresolved_count INTEGER NOT NULL DEFAULT 0,
    registered_count INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE music_scan_files (
    id INTEGER PRIMARY KEY,
    scan_id INTEGER NOT NULL REFERENCES music_scans(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    modified_at INTEGER NOT NULL,
    sha256 TEXT NOT NULL DEFAULT '',
    format TEXT NOT NULL DEFAULT '',
    duration_ms INTEGER NOT NULL DEFAULT 0,
    bitrate INTEGER NOT NULL DEFAULT 0,
    sample_rate INTEGER NOT NULL DEFAULT 0,
    channels INTEGER NOT NULL DEFAULT 0,
    tag_artist TEXT NOT NULL DEFAULT '',
    tag_album_artist TEXT NOT NULL DEFAULT '',
    tag_album TEXT NOT NULL DEFAULT '',
    tag_title TEXT NOT NULL DEFAULT '',
    tag_track INTEGER,
    tag_disc INTEGER,
    tag_year INTEGER,
    tag_musicbrainz_artist TEXT NOT NULL DEFAULT '',
    tag_musicbrainz_album TEXT NOT NULL DEFAULT '',
    tag_musicbrainz_release_group TEXT NOT NULL DEFAULT '',
    tag_musicbrainz_track TEXT NOT NULL DEFAULT '',
    tag_musicbrainz_recording TEXT NOT NULL DEFAULT '',
    tag_json TEXT NOT NULL DEFAULT '{}',
    artist_id INTEGER REFERENCES music_artists(id) ON DELETE SET NULL,
    album_id INTEGER REFERENCES music_albums(id) ON DELETE SET NULL,
    track_id INTEGER REFERENCES music_tracks(id) ON DELETE SET NULL,
    state TEXT NOT NULL CHECK(state IN ('matched','unresolved','registered','skipped')),
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(scan_id, relative_path)
);
CREATE INDEX music_scan_files_scan ON music_scan_files(scan_id);

CREATE TABLE music_files (
    id INTEGER PRIMARY KEY,
    artist_id INTEGER NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
    album_id INTEGER NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    track_id INTEGER REFERENCES music_tracks(id) ON DELETE SET NULL,
    relative_path TEXT NOT NULL UNIQUE,
    size_bytes INTEGER NOT NULL,
    sha256 TEXT NOT NULL DEFAULT '',
    format TEXT NOT NULL DEFAULT '',
    bitrate INTEGER NOT NULL DEFAULT 0,
    sample_rate INTEGER NOT NULL DEFAULT 0,
    channels INTEGER NOT NULL DEFAULT 0,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    tag_json TEXT NOT NULL DEFAULT '{}',
    imported_at INTEGER NOT NULL,
    origin TEXT NOT NULL DEFAULT 'existing',
    source_name TEXT NOT NULL DEFAULT ''
);
CREATE INDEX music_files_album ON music_files(album_id);
CREATE INDEX music_files_artist ON music_files(artist_id);
