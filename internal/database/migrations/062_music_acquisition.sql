ALTER TABLE music_albums ADD COLUMN revision INTEGER NOT NULL DEFAULT 1;

CREATE TABLE music_release_candidates (
    id TEXT PRIMARY KEY,
    album_id INTEGER NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    seeders INTEGER,
    peers INTEGER,
    published_at TEXT NOT NULL DEFAULT '',
    info_url TEXT NOT NULL DEFAULT '',
    parsed_artist TEXT NOT NULL DEFAULT '',
    parsed_album TEXT NOT NULL DEFAULT '',
    parsed_year INTEGER,
    format TEXT NOT NULL DEFAULT '',
    bitrate INTEGER NOT NULL DEFAULT 0,
    lossless INTEGER NOT NULL DEFAULT 0 CHECK(lossless IN (0,1)),
    payload BLOB NOT NULL,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX music_release_album ON music_release_candidates(album_id);
CREATE INDEX music_release_expiry ON music_release_candidates(expires_at);

CREATE TABLE music_import_jobs (
    id INTEGER PRIMARY KEY,
    album_id INTEGER NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    root TEXT NOT NULL,
    source_path TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('running','completed','failed')),
    file_count INTEGER NOT NULL DEFAULT 0,
    imported_count INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE music_import_files (
    id INTEGER PRIMARY KEY,
    job_id INTEGER NOT NULL REFERENCES music_import_jobs(id) ON DELETE CASCADE,
    source_path TEXT NOT NULL,
    track_id INTEGER REFERENCES music_tracks(id) ON DELETE SET NULL,
    destination_path TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK(state IN ('pending','imported','failed','skipped')),
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX music_import_files_job ON music_import_files(job_id);
