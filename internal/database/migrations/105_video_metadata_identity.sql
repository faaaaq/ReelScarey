-- parent-rebuild: keep existing referenced IDs without invoking cascades.
CREATE TABLE media_items_next (
 id INTEGER PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('movie','series')),
 tmdb_id INTEGER NOT NULL CHECK(tmdb_id>0 OR (kind='series' AND tmdb_id<0)),
 title TEXT NOT NULL CHECK(length(title)>0),
 year INTEGER CHECK(year BETWEEN 1800 AND 9999),
 monitored INTEGER NOT NULL DEFAULT 1 CHECK(monitored IN (0,1)),
 status TEXT NOT NULL DEFAULT 'missing' CHECK(status IN ('missing','downloading','available')),
 created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
 overview TEXT NOT NULL DEFAULT '',
 poster_path TEXT NOT NULL DEFAULT '',
 release_date TEXT NOT NULL DEFAULT '',
 runtime_minutes INTEGER,
 category TEXT NOT NULL DEFAULT 'standard' CHECK(category IN ('standard','anime')),
 category_revision INTEGER NOT NULL DEFAULT 1 CHECK(category_revision>0),
 theatrical_release_date TEXT NOT NULL DEFAULT '',
 digital_release_date TEXT NOT NULL DEFAULT '',
 physical_release_date TEXT NOT NULL DEFAULT '',
 match_title TEXT,
 original_language TEXT NOT NULL DEFAULT '',
 title_sort TEXT GENERATED ALWAYS AS (CASE WHEN lower(title) LIKE 'the %' THEN substr(title,5) ELSE title END) VIRTUAL,
 UNIQUE(kind,tmdb_id)
);
INSERT INTO media_items_next(id,kind,tmdb_id,title,year,monitored,status,created_at,overview,poster_path,release_date,runtime_minutes,category,category_revision,theatrical_release_date,digital_release_date,physical_release_date,match_title)
 SELECT id,kind,tmdb_id,title,year,monitored,status,created_at,overview,poster_path,release_date,runtime_minutes,category,category_revision,theatrical_release_date,digital_release_date,physical_release_date,match_title FROM media_items;
DROP TABLE media_items;
ALTER TABLE media_items_next RENAME TO media_items;
CREATE INDEX media_match_title ON media_items(kind,match_title,year);
CREATE INDEX media_match_pending ON media_items(kind,id) WHERE match_title IS NULL;
CREATE INDEX media_items_browse ON media_items(kind,title COLLATE NOCASE);
CREATE INDEX media_items_title_sort ON media_items(kind,title_sort COLLATE NOCASE,id);
CREATE TRIGGER event_media_added AFTER INSERT ON media_items BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',NEW.id,NEW.title,'added');
END;
CREATE TRIGGER event_media_monitoring AFTER UPDATE OF monitored ON media_items WHEN OLD.monitored<>NEW.monitored BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'title',NEW.id,NEW.title,CASE WHEN NEW.monitored=1 THEN 'monitoring enabled' ELSE 'monitoring paused' END);
END;
CREATE TRIGGER media_match_title_changed AFTER UPDATE OF title ON media_items WHEN OLD.title<>NEW.title BEGIN
 UPDATE media_items SET match_title=NULL WHERE id=NEW.id;
END;

-- Automatic container metadata uses the same durable publication as manual updates.
CREATE TABLE video_title_updates (
 media_file_id INTEGER PRIMARY KEY REFERENCES media_files(id) ON DELETE CASCADE,
 next_attempt INTEGER NOT NULL DEFAULT 0
);
CREATE TRIGGER video_title_registered AFTER INSERT ON media_files BEGIN
 INSERT OR IGNORE INTO video_title_updates(media_file_id) VALUES(NEW.id);
END;
CREATE TRIGGER video_title_rematched AFTER UPDATE OF movie_id,series_id,imported_at ON media_files
 WHEN OLD.movie_id IS NOT NEW.movie_id OR OLD.series_id IS NOT NEW.series_id OR OLD.imported_at<>NEW.imported_at BEGIN
 INSERT INTO video_title_updates(media_file_id) VALUES(NEW.id) ON CONFLICT(media_file_id) DO UPDATE SET next_attempt=0;
END;
CREATE TRIGGER video_title_episode_linked AFTER INSERT ON episode_files BEGIN
 INSERT INTO video_title_updates(media_file_id) VALUES(NEW.media_file_id) ON CONFLICT(media_file_id) DO UPDATE SET next_attempt=0;
END;
CREATE TRIGGER video_title_metadata_refreshed AFTER UPDATE OF title ON media_items WHEN OLD.title<>NEW.title BEGIN
 INSERT INTO video_title_updates(media_file_id) SELECT id FROM media_files WHERE movie_id=NEW.id OR series_id=NEW.id
 ON CONFLICT(media_file_id) DO UPDATE SET next_attempt=0;
END;
CREATE TRIGGER video_title_episode_refreshed AFTER UPDATE OF title ON episodes WHEN OLD.title<>NEW.title BEGIN
 INSERT INTO video_title_updates(media_file_id) SELECT media_file_id FROM episode_files WHERE episode_id=NEW.id
 ON CONFLICT(media_file_id) DO UPDATE SET next_attempt=0;
END;

ALTER TABLE music_edit_jobs ADD COLUMN automatic_title INTEGER NOT NULL DEFAULT 0 CHECK(automatic_title IN (0,1));
CREATE TABLE music_title_updates (
 music_file_id INTEGER PRIMARY KEY REFERENCES music_files(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL DEFAULT 1,
 next_attempt INTEGER NOT NULL DEFAULT 0
);
CREATE TRIGGER music_title_registered AFTER INSERT ON music_files WHEN NEW.track_id IS NOT NULL BEGIN
 INSERT OR IGNORE INTO music_title_updates(music_file_id) VALUES(NEW.id);
END;
CREATE TRIGGER music_title_rematched AFTER UPDATE OF track_id ON music_files WHEN NEW.track_id IS NOT NULL AND OLD.track_id IS NOT NEW.track_id BEGIN
 INSERT INTO music_title_updates(music_file_id) VALUES(NEW.id)
 ON CONFLICT(music_file_id) DO UPDATE SET revision=revision+1,next_attempt=0;
END;
CREATE TRIGGER music_track_title_refreshed AFTER UPDATE OF title ON music_tracks WHEN OLD.title<>NEW.title BEGIN
 INSERT INTO music_title_updates(music_file_id) SELECT id FROM music_files WHERE track_id=NEW.id
 ON CONFLICT(music_file_id) DO UPDATE SET revision=revision+1,next_attempt=0;
END;

CREATE TABLE integration_configs_next (
 name TEXT PRIMARY KEY CHECK(name IN ('torznab','qbittorrent','opensubtitles','sabnzbd','emby','jellyfin','plex','mullvad','acoustid','musicbrainz','music-server-token','thetvdb') OR
          (length(name)=48 AND substr(name,1,16)='download-client:' AND substr(name,17) NOT GLOB '*[^0-9a-f]*')),
 ciphertext BLOB NOT NULL,
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO integration_configs_next SELECT name,ciphertext,updated_at FROM integration_configs;
DROP TABLE integration_configs;
ALTER TABLE integration_configs_next RENAME TO integration_configs;

-- Album reads must distinguish a never-loaded track list from an empty list
-- after the owner deletes its last track.
ALTER TABLE music_albums ADD COLUMN tracks_loaded INTEGER NOT NULL DEFAULT 0 CHECK(tracks_loaded IN (0,1));
UPDATE music_albums SET tracks_loaded=1 WHERE EXISTS(SELECT 1 FROM music_tracks WHERE album_id=music_albums.id);
CREATE TRIGGER music_album_tracks_loaded AFTER INSERT ON music_tracks BEGIN
 UPDATE music_albums SET tracks_loaded=1 WHERE id=NEW.album_id AND tracks_loaded=0;
END;

CREATE TRIGGER event_connection_added AFTER INSERT ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection saved');
END;
CREATE TRIGGER event_connection_changed AFTER UPDATE OF ciphertext ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection updated');
END;
CREATE TRIGGER event_connection_removed AFTER DELETE ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',OLD.name,OLD.name,'connection removed');
END;
