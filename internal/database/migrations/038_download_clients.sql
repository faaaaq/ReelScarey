-- Preserve legacy credential names/ciphertexts while permitting only the new
-- bounded named-client namespace. Encryption associated-data names are unchanged.
CREATE TABLE integration_configs_next (
 name TEXT PRIMARY KEY CHECK(name IN ('torznab','qbittorrent','opensubtitles','sabnzbd','emby') OR
  (length(name)=48 AND substr(name,1,16)='download-client:' AND substr(name,17) NOT GLOB '*[^0-9a-f]*')),
 ciphertext BLOB NOT NULL,
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO integration_configs_next SELECT * FROM integration_configs;
DROP TABLE integration_configs;
ALTER TABLE integration_configs_next RENAME TO integration_configs;
-- Rebuilding the credential table also removes its triggers. Retain the
-- established secret-free connection history without backfilling new events.
CREATE TRIGGER event_connection_added AFTER INSERT ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection saved');
END;
CREATE TRIGGER event_connection_changed AFTER UPDATE OF ciphertext ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection updated');
END;
CREATE TRIGGER event_connection_removed AFTER DELETE ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',OLD.name,OLD.name,'connection removed');
END;

CREATE TABLE download_clients (
 id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE UNIQUE,
 kind TEXT NOT NULL CHECK(kind IN ('qbittorrent','sabnzbd','nzbget')),
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE TABLE download_client_preferences (
 id INTEGER PRIMARY KEY CHECK(id=1), torrent TEXT NOT NULL DEFAULT '',
 usenet TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 1
);
INSERT INTO download_client_preferences(id) VALUES(1);
ALTER TABLE downloads ADD COLUMN client_key TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN client_kind TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN client_label TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN client_download_root TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN client_local_folder TEXT NOT NULL DEFAULT '';
CREATE INDEX downloads_client_binding ON downloads(client_key,state);
