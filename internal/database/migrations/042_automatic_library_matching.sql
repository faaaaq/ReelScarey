CREATE TABLE library_match_jobs (
 scan_id TEXT PRIMARY KEY REFERENCES library_scans(id) ON DELETE CASCADE,
 state TEXT NOT NULL CHECK(state IN ('pending','running','waiting','completed','cancelled','paused')),
 phase TEXT NOT NULL DEFAULT 'resolve' CHECK(phase IN ('resolve','queue')),
 category TEXT NOT NULL DEFAULT 'standard',
 message TEXT NOT NULL DEFAULT '',
 lease_owner TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0,
 retry_at INTEGER NOT NULL DEFAULT 0, attempts INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
);
CREATE TABLE library_match_candidates (
 file_id TEXT PRIMARY KEY REFERENCES library_scan_files(id) ON DELETE CASCADE,
 scan_id TEXT NOT NULL REFERENCES library_match_jobs(scan_id) ON DELETE CASCADE,
 state TEXT NOT NULL CHECK(state IN ('ready','review','queued')),
 payload TEXT NOT NULL DEFAULT '{}', message TEXT NOT NULL DEFAULT ''
);
CREATE INDEX library_match_candidate_scan ON library_match_candidates(scan_id,state);
CREATE TABLE library_match_cache (
 scan_id TEXT NOT NULL REFERENCES library_match_jobs(scan_id) ON DELETE CASCADE,
 identity TEXT NOT NULL, payload TEXT NOT NULL,
 PRIMARY KEY(scan_id,identity)
);