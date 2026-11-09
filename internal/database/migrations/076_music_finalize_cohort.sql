-- Restrict a finalize workflow to the matched files that the owner reviewed.
-- Without this cohort, resuming a finalize re-organized every file registered
-- by earlier scans in the same scan.

ALTER TABLE music_scan_files ADD COLUMN finalize_requested INTEGER NOT NULL DEFAULT 0 CHECK(finalize_requested IN (0,1));
CREATE INDEX music_scan_files_finalize ON music_scan_files(scan_id, finalize_requested, state);
