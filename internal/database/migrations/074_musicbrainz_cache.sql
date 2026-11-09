-- MusicBrainz responses are slow-changing provider evidence, not permanent
-- truth. One local cache row stores the raw JSON for a fixed base URL,
-- endpoint and sorted query. Negative rows are explicit 404/empty metadata
-- observations and expire sooner. The provider-state row keeps a durable
-- rate-limit cooldown so a restart cannot immediately retry against a server
-- that asked ReelScarey to wait.
PRAGMA defer_foreign_keys=ON;

-- A user-supplied MusicBrainz base URL is optional and encrypted through the
-- existing integration store. Add the name to the fixed allowlist while
-- preserving every existing row, ciphertext and activity trigger.
CREATE TABLE integration_configs_next (
 name TEXT PRIMARY KEY CHECK(name IN ('torznab','qbittorrent','opensubtitles','sabnzbd','emby','mullvad','acoustid','musicbrainz') OR
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

CREATE TABLE musicbrainz_cache (
    key TEXT PRIMARY KEY,
    payload TEXT NOT NULL,
    expires_at INTEGER NOT NULL,
    negative INTEGER NOT NULL DEFAULT 0
);

-- Expired rows are removed in bounded batches. The index keeps cleanup and
-- count checks cheap even after a large library scan.
CREATE INDEX musicbrainz_cache_expiry ON musicbrainz_cache(expires_at);

CREATE TABLE musicbrainz_provider_state (
    id INTEGER PRIMARY KEY CHECK(id=1),
    retry_until INTEGER NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);

INSERT INTO musicbrainz_provider_state(id) VALUES(1);
