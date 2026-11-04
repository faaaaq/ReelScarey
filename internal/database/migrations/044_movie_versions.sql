-- Preserve registered IDs, observations, subtitle/episode links and provenance.
PRAGMA defer_foreign_keys=ON;
-- Deferred constraints do not defer ON DELETE CASCADE. Preserve subtitle edits
-- (including their rowids used by outstanding removal reviews) across the rebuild.
CREATE TEMP TABLE movie_version_subtitle_edits AS SELECT rowid AS saved_rowid,* FROM subtitle_edits;
CREATE TABLE media_files_next (
 id INTEGER PRIMARY KEY,
 movie_id INTEGER REFERENCES media_items(id),
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
 verification_json TEXT NOT NULL DEFAULT '',
 video_resolution INTEGER GENERATED ALWAYS AS (
   CASE WHEN json_valid(probe_json) THEN CASE
     WHEN json_extract(probe_json,'$.width')>=3500 OR json_extract(probe_json,'$.height')>=1900 THEN 2160
     WHEN json_extract(probe_json,'$.width')>=1700 OR json_extract(probe_json,'$.height')>=900 THEN 1080
     WHEN json_extract(probe_json,'$.width')>=1100 OR json_extract(probe_json,'$.height')>=650 THEN 720
     WHEN json_extract(probe_json,'$.width')>0 AND json_extract(probe_json,'$.height')>0 THEN 480
     ELSE 0 END ELSE 0 END
 ) VIRTUAL,
 CHECK((movie_id IS NOT NULL AND series_id IS NULL) OR (movie_id IS NULL AND series_id IS NOT NULL)),
 CHECK((origin='download' AND download_id IS NOT NULL) OR (origin='existing' AND download_id IS NULL)),
 UNIQUE(download_id,file_index)
);
INSERT INTO media_files_next(id,movie_id,series_id,download_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name,verification_json)
 SELECT id,movie_id,series_id,download_id,file_index,relative_path,size_bytes,sha256,probe_json,imported_at,origin,source_name,verification_json FROM media_files;
DROP TABLE media_files;
ALTER TABLE media_files_next RENAME TO media_files;
INSERT INTO subtitle_edits(rowid,id,media_file_id,language,variant,action,snapshot,content,old_sha,new_sha,size_bytes,cues,offset_ms,destination,stage_path,previous_path,state,phase,message,expires_at,created_at,lease_owner,lease_until)
 SELECT * FROM movie_version_subtitle_edits saved WHERE NOT EXISTS(SELECT 1 FROM subtitle_edits e WHERE e.id=saved.id);
DROP TABLE movie_version_subtitle_edits;
CREATE INDEX media_file_path_folded ON media_files(relative_path COLLATE NOCASE);
CREATE INDEX movie_file_versions ON media_files(movie_id,video_resolution DESC,id);
CREATE TRIGGER event_media_file_added AFTER INSERT ON media_files BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT unixepoch(),'title',m.id,m.title,'file registered' FROM media_items m WHERE m.id=COALESCE(NEW.movie_id,NEW.series_id);
END;
CREATE TRIGGER event_media_file_removed AFTER DELETE ON media_files BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
 SELECT unixepoch(),'title',m.id,m.title,'file registration removed' FROM media_items m WHERE m.id=COALESCE(OLD.movie_id,OLD.series_id);
END;
PRAGMA defer_foreign_keys=OFF;

CREATE TABLE movie_version_settings (
 media_id INTEGER PRIMARY KEY REFERENCES media_items(id),
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 hd_profile_id INTEGER NOT NULL REFERENCES quality_profiles(id),
 uhd_profile_id INTEGER NOT NULL REFERENCES quality_profiles(id),
 revision INTEGER NOT NULL DEFAULT 1
);
ALTER TABLE downloads ADD COLUMN movie_version TEXT NOT NULL DEFAULT '' CHECK(movie_version IN ('','hd','uhd','additional','replacement'));
UPDATE downloads SET movie_version='replacement' WHERE kind='movie' AND EXISTS(SELECT 1 FROM acquisition_replacements a WHERE a.source_kind='download' AND a.job_id=downloads.id);
DROP INDEX one_movie_download;
CREATE UNIQUE INDEX one_movie_download ON downloads(movie_id,movie_version)
 WHERE kind='movie' AND movie_version IN ('','hd','uhd') AND state NOT IN ('completed','cancelled');
