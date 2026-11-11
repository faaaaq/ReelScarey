-- Add named Jellyfin and Plex connections to the encrypted integration store.
-- The rebuild preserves every existing row; the table has no foreign-key
-- children, and the migration runner checks integrity before committing.
CREATE TABLE integration_configs_next (
    name TEXT PRIMARY KEY CHECK(name IN ('torznab','qbittorrent','opensubtitles','sabnzbd','emby','jellyfin','plex','mullvad','acoustid','musicbrainz') OR
      (length(name)=48 AND substr(name,1,16)='download-client:' AND substr(name,17) NOT GLOB '*[^0-9a-f]*')),
    ciphertext BLOB NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO integration_configs_next(name,ciphertext,updated_at) SELECT name,ciphertext,updated_at FROM integration_configs;
DROP TABLE integration_configs;
ALTER TABLE integration_configs_next RENAME TO integration_configs;
-- The rebuild removes the triggers with the old table. Recreate the established
-- secret-free connection history triggers.
CREATE TRIGGER event_connection_added AFTER INSERT ON integration_configs BEGIN
  INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
  VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection saved');
END;
CREATE TRIGGER event_connection_changed AFTER UPDATE OF ciphertext ON integration_configs BEGIN
  INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
  VALUES(unixepoch(),'connection',NEW.name,NEW.name,'connection updated');
END;
CREATE TRIGGER event_connection_removed AFTER DELETE ON integration_configs BEGIN
  INSERT INTO activity_events(occurred_at,kind,object_id,title,state)
  VALUES(unixepoch(),'connection',OLD.name,OLD.name,'connection removed');
END;
