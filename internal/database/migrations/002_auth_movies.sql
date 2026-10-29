CREATE TABLE admin_account (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    username TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE TABLE sessions (
    token_hash TEXT PRIMARY KEY,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX sessions_expiry ON sessions (expires_at);
CREATE TABLE provider_secrets (
    provider TEXT PRIMARY KEY CHECK (provider = 'tmdb'),
    ciphertext BLOB NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
ALTER TABLE media_items ADD COLUMN overview TEXT NOT NULL DEFAULT '';
ALTER TABLE media_items ADD COLUMN poster_path TEXT NOT NULL DEFAULT '';
ALTER TABLE media_items ADD COLUMN release_date TEXT NOT NULL DEFAULT '';
ALTER TABLE media_items ADD COLUMN runtime_minutes INTEGER;
