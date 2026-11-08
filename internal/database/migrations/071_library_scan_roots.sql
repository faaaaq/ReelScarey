-- Scan roots are discovery sources. Organization destinations and naming
-- templates stay in organization_config.libraries and are not changed here.
CREATE TABLE library_root_config (
    id INTEGER PRIMARY KEY CHECK(id=1),
    revision INTEGER NOT NULL DEFAULT 1,
    roots TEXT NOT NULL DEFAULT '{}'
);
INSERT INTO library_root_config(id) VALUES(1);

-- Existing scans recorded one folder. Keep that exact source visible after
-- upgrading and let new scans store every configured root.
ALTER TABLE library_scans ADD COLUMN roots TEXT NOT NULL DEFAULT '[]';
UPDATE library_scans SET roots = json_array(folder);

-- Music scans may read several configured roots, while music_scans.root stays
-- the human-readable summary of that snapshot.
CREATE TABLE music_scan_roots (
    scan_id INTEGER NOT NULL REFERENCES music_scans(id) ON DELETE CASCADE,
    relative_path TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(scan_id, relative_path)
);
INSERT INTO music_scan_roots(scan_id,relative_path)
    SELECT id,root FROM music_scans WHERE root<>'';

-- Several scans may wait in the queue. The worker still leases exactly one
-- scan at a time, so dropping the single-active-row index is safe.
DROP INDEX one_library_scan;
CREATE INDEX library_scan_active ON library_scans(state,created_at);
