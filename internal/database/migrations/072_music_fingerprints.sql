-- AcoustID application keys share the encrypted integration store. The name is
-- added to the fixed allowlist; existing rows, ciphertexts and triggers are
-- preserved so the key is covered by the existing secrets.key backup checks.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE integration_configs_next (
 name TEXT PRIMARY KEY CHECK(name IN ('torznab','qbittorrent','opensubtitles','sabnzbd','emby','mullvad','acoustid') OR
  (length(name)=48 AND substr(name,1,16)='download-client:' AND substr(name,17) NOT GLOB '*[^0-9a-f]*')),
 ciphertext BLOB NOT NULL,
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO integration_configs_next SELECT * FROM integration_configs;
DROP TABLE integration_configs;
ALTER TABLE integration_configs_next RENAME TO integration_configs;
CREATE TRIGGER event_connection_added AFTER INSERT ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection saved');
END;
CREATE TRIGGER event_connection_changed AFTER UPDATE OF ciphertext ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection updated');
END;
CREATE TRIGGER event_connection_removed AFTER DELETE ON integration_configs BEGIN
 INSERT INTO activity_events(occurred_at,kind,object_id,title,state) VALUES(unixepoch(),'connection',OLD.name,OLD.name,'connection removed');
END;

-- Chromaprint fingerprints are keyed by the media-root relative path. A saved
-- fingerprint is reused only while the file size and modification time still
-- match; a changed file replaces the row after it is fingerprinted again.
CREATE TABLE music_fingerprints (
    relative_path TEXT PRIMARY KEY,
    size_bytes INTEGER NOT NULL,
    modified_at INTEGER NOT NULL,
    duration_seconds INTEGER NOT NULL,
    fingerprint TEXT NOT NULL,
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);
