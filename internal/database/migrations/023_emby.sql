CREATE TABLE integration_configs_next (
 name TEXT PRIMARY KEY CHECK(name IN ('torznab','qbittorrent','opensubtitles','sabnzbd','emby')),
 ciphertext BLOB NOT NULL,
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO integration_configs_next SELECT * FROM integration_configs;
DROP TABLE integration_configs;
ALTER TABLE integration_configs_next RENAME TO integration_configs;
