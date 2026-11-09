-- Resumable music scan workflow. Existing scans keep their state and counts;
-- the new columns default to "no pending request and no live lease". A scan
-- interrupted mid-walk is still reset to failed by RecoverScans because its
-- observed rows may be incomplete. A queued, matching, fingerprinting or
-- organizing request is re-queued and resumes in the durable worker.
ALTER TABLE music_scans ADD COLUMN full_scan INTEGER NOT NULL DEFAULT 0;
ALTER TABLE music_scans ADD COLUMN phase TEXT NOT NULL DEFAULT '';
ALTER TABLE music_scans ADD COLUMN progress_total INTEGER NOT NULL DEFAULT 0;
ALTER TABLE music_scans ADD COLUMN progress_done INTEGER NOT NULL DEFAULT 0;
ALTER TABLE music_scans ADD COLUMN current_path TEXT NOT NULL DEFAULT '';
ALTER TABLE music_scans ADD COLUMN message TEXT NOT NULL DEFAULT '';
ALTER TABLE music_scans ADD COLUMN skipped_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE music_scans ADD COLUMN request TEXT NOT NULL DEFAULT '';
ALTER TABLE music_scans ADD COLUMN requested_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE music_scans ADD COLUMN work_owner TEXT NOT NULL DEFAULT '';
ALTER TABLE music_scans ADD COLUMN work_until INTEGER NOT NULL DEFAULT 0;

-- The skip decision looks up the newest observation for one relative path
-- without walking every old scan.
CREATE INDEX music_scan_files_path ON music_scan_files(relative_path);

-- The workflow worker claims one queued, unleased scan at a time.
CREATE INDEX music_scans_work ON music_scans(request, work_until);
