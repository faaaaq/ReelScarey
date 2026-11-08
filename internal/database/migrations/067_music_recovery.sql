ALTER TABLE music_edit_files ADD COLUMN original_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE music_edit_files ADD COLUMN edited_sha TEXT NOT NULL DEFAULT '';
ALTER TABLE music_edit_files ADD COLUMN original_root TEXT NOT NULL DEFAULT '';
ALTER TABLE music_edit_files ADD COLUMN rollback_phase INTEGER NOT NULL DEFAULT 0;

ALTER TABLE music_downloads ADD COLUMN remote_id TEXT NOT NULL DEFAULT '';
ALTER TABLE music_downloads ADD COLUMN client_url TEXT NOT NULL DEFAULT '';
CREATE INDEX music_download_hash ON music_downloads(info_hash COLLATE NOCASE);

ALTER TABLE music_import_jobs ADD COLUMN download_id TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX music_import_download ON music_import_jobs(download_id) WHERE download_id<>'';
ALTER TABLE music_import_files ADD COLUMN publication TEXT NOT NULL DEFAULT '';

ALTER TABLE music_schedule ADD COLUMN cursor_album_id INTEGER NOT NULL DEFAULT 0;

-- Earlier manual uploads were marked public without checking their private
-- flag. Proven public indexer rows keep their classification.
UPDATE downloads SET indexer_public=0 WHERE indexer_name='Manual upload';
CREATE TABLE torrent_public_pause_completion (
    hash TEXT PRIMARY KEY,
    added_at INTEGER NOT NULL,
    completed_at INTEGER NOT NULL
);

ALTER TABLE media_items ADD COLUMN match_title TEXT;
CREATE INDEX media_match_title ON media_items(kind,match_title,year);
CREATE INDEX media_match_pending ON media_items(kind,id) WHERE match_title IS NULL;
CREATE TRIGGER media_match_title_changed AFTER UPDATE OF title ON media_items
WHEN OLD.title<>NEW.title BEGIN
    UPDATE media_items SET match_title=NULL WHERE id=NEW.id;
END;

ALTER TABLE music_scan_files ADD COLUMN verification_json TEXT NOT NULL DEFAULT '';
CREATE INDEX music_scan_observation ON music_scan_files(relative_path,id DESC)
    WHERE verification_json<>'' AND sha256<>'';
