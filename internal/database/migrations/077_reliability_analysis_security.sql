-- Reliability, analysis, NFO export and account-security support.

CREATE TABLE release_blacklist (
    id INTEGER PRIMARY KEY,
    protocol TEXT NOT NULL CHECK(protocol IN ('torrent','usenet')),
    release_key TEXT NOT NULL,
    indexer TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL DEFAULT 0,
    UNIQUE(release_key, indexer)
);
CREATE INDEX release_blacklist_lookup ON release_blacklist(release_key, expires_at);

ALTER TABLE downloads ADD COLUMN last_progress_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE downloads ADD COLUMN stalled_at INTEGER NOT NULL DEFAULT 0;

-- Record a progress heartbeat only when bytes actually move. Polling an
-- unchanged client must not look like activity forever.
CREATE TRIGGER downloads_progress_moved AFTER UPDATE OF downloaded_bytes ON downloads
WHEN OLD.downloaded_bytes <> NEW.downloaded_bytes
BEGIN
    UPDATE downloads SET last_progress_at = unixepoch() WHERE id = NEW.id;
END;
UPDATE downloads SET last_progress_at = updated_at WHERE state = 'downloading' AND progress < 1;

ALTER TABLE media_files ADD COLUMN analysis_state TEXT NOT NULL DEFAULT '';
ALTER TABLE media_files ADD COLUMN analysis_json TEXT NOT NULL DEFAULT '';
ALTER TABLE media_files ADD COLUMN analysis_at INTEGER NOT NULL DEFAULT 0;

ALTER TABLE settings ADD COLUMN nfo_export INTEGER NOT NULL DEFAULT 0 CHECK(nfo_export IN (0,1));

CREATE TABLE admin_totp (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    secret_ciphertext BLOB NOT NULL,
    recovery_hashes TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
