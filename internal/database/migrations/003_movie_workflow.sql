CREATE TABLE integration_configs (
    name TEXT PRIMARY KEY CHECK (name IN ('torznab', 'qbittorrent')),
    ciphertext BLOB NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE TABLE release_candidates (
    id TEXT PRIMARY KEY,
    movie_id INTEGER NOT NULL REFERENCES media_items(id),
    title TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK(size_bytes >= 0),
    seeders INTEGER,
    peers INTEGER,
    published_at TEXT NOT NULL,
    payload BLOB NOT NULL,
    expires_at INTEGER NOT NULL
);
CREATE INDEX release_expiry ON release_candidates(expires_at);
CREATE TABLE downloads (
    id TEXT PRIMARY KEY,
    movie_id INTEGER NOT NULL REFERENCES media_items(id),
    release_id TEXT NOT NULL UNIQUE REFERENCES release_candidates(id),
    title TEXT NOT NULL,
    info_hash TEXT NOT NULL UNIQUE,
    payload BLOB NOT NULL,
    client_url TEXT NOT NULL,
    save_path TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('queued','submitting','downloading','importing','blocked','failed','completed','cancelled')),
    submission_attempted INTEGER NOT NULL DEFAULT 0,
    submitted_at INTEGER NOT NULL DEFAULT 0,
    progress REAL NOT NULL DEFAULT 0,
    total_bytes INTEGER NOT NULL DEFAULT 0,
    downloaded_bytes INTEGER NOT NULL DEFAULT 0,
    client_state TEXT NOT NULL DEFAULT '',
    selected_file INTEGER,
    choices TEXT NOT NULL DEFAULT '[]',
    attempts INTEGER NOT NULL DEFAULT 0,
    next_run INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX one_movie_download ON downloads(movie_id) WHERE state NOT IN ('completed','cancelled');
CREATE INDEX download_claim ON downloads(next_run,lease_until) WHERE state IN ('queued','submitting','downloading','importing');
CREATE TABLE import_operations (
    download_id TEXT PRIMARY KEY REFERENCES downloads(id),
    source_path TEXT NOT NULL,
    destination_path TEXT NOT NULL UNIQUE,
    stage_name TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK(size_bytes > 0),
    source_mtime INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    probe_json TEXT NOT NULL,
    mode TEXT NOT NULL DEFAULT 'pending' CHECK(mode IN ('pending','hardlink','copy')),
    phase TEXT NOT NULL CHECK(phase IN ('planned','staged','published','committed')),
    updated_at INTEGER NOT NULL
);
CREATE TABLE media_files (
    id INTEGER PRIMARY KEY,
    movie_id INTEGER NOT NULL UNIQUE REFERENCES media_items(id),
    download_id TEXT NOT NULL UNIQUE REFERENCES downloads(id),
    relative_path TEXT NOT NULL UNIQUE,
    size_bytes INTEGER NOT NULL CHECK(size_bytes > 0),
    sha256 TEXT NOT NULL,
    probe_json TEXT NOT NULL,
    imported_at INTEGER NOT NULL
);
