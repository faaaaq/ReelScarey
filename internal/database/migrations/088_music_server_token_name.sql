-- The opt-in music server token is stored in the encrypted integration store,
-- but the allowlist introduced by the acoustid/musicbrainz and media-server
-- rebuilds never included its name. Generating a token therefore failed the
-- table CHECK constraint and surfaced as a generic internal error. Add the name
-- while preserving every existing row, ciphertext and connection-history
-- trigger; the table has no foreign-key children.
PRAGMA defer_foreign_keys=ON;
CREATE TABLE integration_configs_next (
 name TEXT PRIMARY KEY CHECK(name IN ('torznab','qbittorrent','opensubtitles','sabnzbd','emby','jellyfin','plex','mullvad','acoustid','musicbrainz','music-server-token') OR
  (length(name)=48 AND substr(name,1,16)='download-client:' AND substr(name,17) NOT GLOB '*[^0-9a-f]*')),
 ciphertext BLOB NOT NULL,
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO integration_configs_next(name,ciphertext,updated_at) SELECT name,ciphertext,updated_at FROM integration_configs;
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
