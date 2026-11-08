CREATE TABLE music_downloads (
    id TEXT PRIMARY KEY,
    album_id INTEGER NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    release_id TEXT NOT NULL,
    title TEXT NOT NULL,
    info_hash TEXT NOT NULL DEFAULT '',
    protocol TEXT NOT NULL DEFAULT 'torrent' CHECK(protocol IN ('torrent','usenet')),
    client_key TEXT NOT NULL DEFAULT '',
    client_kind TEXT NOT NULL DEFAULT '',
    client_label TEXT NOT NULL DEFAULT '',
    client_download_root TEXT NOT NULL DEFAULT '',
    client_local_folder TEXT NOT NULL DEFAULT '',
    save_path TEXT NOT NULL DEFAULT '',
    root TEXT NOT NULL,
    indexer_name TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK(state IN ('queued','submitting','downloading','importing','blocked','failed','completed','cancelled')),
    progress REAL NOT NULL DEFAULT 0,
    total_bytes INTEGER NOT NULL DEFAULT 0,
    downloaded_bytes INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    import_job_id INTEGER,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_run INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX music_download_claim ON music_downloads(next_run, lease_until);
CREATE INDEX music_download_album ON music_downloads(album_id, state);

CREATE TABLE music_preferences (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    allow_lossless INTEGER NOT NULL DEFAULT 1 CHECK(allow_lossless IN (0,1)),
    min_bitrate INTEGER NOT NULL DEFAULT 0,
    preferred_formats TEXT NOT NULL DEFAULT 'FLAC,ALAC,MP3',
    revision INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL
);
INSERT INTO music_preferences(id, updated_at) VALUES (1, unixepoch());

CREATE TABLE music_schedule (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    interval_hours INTEGER NOT NULL DEFAULT 24 CHECK(interval_hours BETWEEN 1 AND 168),
    mode TEXT NOT NULL DEFAULT 'search' CHECK(mode IN ('search','download')),
    next_run INTEGER NOT NULL DEFAULT 0,
    last_run INTEGER NOT NULL DEFAULT 0,
    lease_until INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL
);
INSERT INTO music_schedule(id, updated_at) VALUES (1, unixepoch());
