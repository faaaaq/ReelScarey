PRAGMA defer_foreign_keys=ON;
CREATE TABLE media_files_next (
 id INTEGER PRIMARY KEY,
 movie_id INTEGER UNIQUE REFERENCES media_items(id),
 series_id INTEGER REFERENCES media_items(id),
 download_id TEXT REFERENCES downloads(id),
 file_index INTEGER NOT NULL DEFAULT -1,
 relative_path TEXT NOT NULL UNIQUE,
 size_bytes INTEGER NOT NULL CHECK(size_bytes>0),
 sha256 TEXT NOT NULL,
 probe_json TEXT NOT NULL,
 imported_at INTEGER NOT NULL,
 origin TEXT NOT NULL DEFAULT 'download' CHECK(origin IN ('download','existing')),
 source_name TEXT NOT NULL DEFAULT '',
 CHECK((movie_id IS NOT NULL AND series_id IS NULL) OR (movie_id IS NULL AND series_id IS NOT NULL)),
 CHECK((origin='download' AND download_id IS NOT NULL) OR (origin='existing' AND download_id IS NULL)),
 UNIQUE(download_id,file_index)
);
INSERT INTO media_files_next(id,movie_id,series_id,download_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at)
 SELECT id,movie_id,series_id,download_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at FROM media_files;
DROP TABLE media_files;
ALTER TABLE media_files_next RENAME TO media_files;
CREATE INDEX media_file_path_folded ON media_files(relative_path COLLATE NOCASE);
-- The migration runner checks all foreign keys before committing. Preserve
-- file IDs and every episode/subtitle reference through this parent rebuild.
PRAGMA defer_foreign_keys=OFF;

CREATE TABLE library_scans (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('movie','series')),
 media_root TEXT NOT NULL,
 resolved_root TEXT NOT NULL,
 folder TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('queued','scanning','completed','cancelled','failed')),
 message TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 lease_owner TEXT NOT NULL DEFAULT '',
 lease_until INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX one_library_scan ON library_scans((1)) WHERE state IN ('queued','scanning');
CREATE TABLE library_scan_dirs (
 scan_id TEXT NOT NULL REFERENCES library_scans(id) ON DELETE CASCADE,
 relative_path TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','done')),
 examined INTEGER NOT NULL DEFAULT 0,
 skipped INTEGER NOT NULL DEFAULT 0,
 PRIMARY KEY(scan_id,relative_path)
);
CREATE TABLE library_scan_files (
 id TEXT PRIMARY KEY,
 scan_id TEXT NOT NULL REFERENCES library_scans(id) ON DELETE CASCADE,
 relative_path TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes>0),
 mtime INTEGER NOT NULL,
 file_identity TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'review' CHECK(state IN ('review','queued','verifying','registered','skipped','blocked')),
 media_id INTEGER REFERENCES media_items(id),
 targets TEXT NOT NULL DEFAULT '[]',
 media_file_id INTEGER REFERENCES media_files(id),
 message TEXT NOT NULL DEFAULT '',
 updated_at INTEGER NOT NULL,
 lease_owner TEXT NOT NULL DEFAULT '',
 lease_until INTEGER NOT NULL DEFAULT 0,
 UNIQUE(scan_id,relative_path)
);
CREATE INDEX library_file_work ON library_scan_files(state,lease_until);
