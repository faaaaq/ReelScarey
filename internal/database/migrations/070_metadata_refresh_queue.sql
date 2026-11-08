CREATE TABLE metadata_refresh_queue (
    media_id INTEGER PRIMARY KEY REFERENCES media_items(id) ON DELETE CASCADE,
    requested_at INTEGER NOT NULL DEFAULT (unixepoch()),
    full INTEGER NOT NULL DEFAULT 0 CHECK (full IN (0, 1)),
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    next_attempt INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX metadata_refresh_queue_due ON metadata_refresh_queue(next_attempt, requested_at);
