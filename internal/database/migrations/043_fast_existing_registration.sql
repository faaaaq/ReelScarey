-- Existing media can be catalogued without streaming the entire video. A full
-- SHA remains empty until an operation that needs it explicitly prepares it.
ALTER TABLE media_files ADD COLUMN verification_json TEXT NOT NULL DEFAULT '';
CREATE INDEX library_scan_file_resolution ON library_scan_files(scan_id,state,relative_path);
