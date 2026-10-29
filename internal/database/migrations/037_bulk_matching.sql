CREATE TABLE library_match_reviews (
 id TEXT PRIMARY KEY, scan_id TEXT NOT NULL REFERENCES library_scans(id) ON DELETE CASCADE,
 payload TEXT NOT NULL, expires_at INTEGER NOT NULL, consumed_at INTEGER NOT NULL DEFAULT 0
);
