CREATE TABLE integration_configs_next (
 name TEXT PRIMARY KEY CHECK(name IN ('torznab','qbittorrent','opensubtitles','sabnzbd')),
 ciphertext BLOB NOT NULL,
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO integration_configs_next SELECT * FROM integration_configs;
DROP TABLE integration_configs;
ALTER TABLE integration_configs_next RENAME TO integration_configs;

CREATE TABLE indexers_next (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 80),
 kind TEXT NOT NULL CHECK(kind IN ('torznab','newznab','iptorrents','xspeeds','nyaa','rss')),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1,
 generation TEXT NOT NULL,
 ciphertext BLOB NOT NULL,
 updated_at INTEGER NOT NULL,
 last_test_at INTEGER NOT NULL DEFAULT 0,
 last_search_at INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 last_result_count INTEGER NOT NULL DEFAULT 0
);
INSERT INTO indexers_next SELECT * FROM indexers;
DROP TABLE indexers;
ALTER TABLE indexers_next RENAME TO indexers;

ALTER TABLE downloads ADD COLUMN protocol TEXT NOT NULL DEFAULT 'torrent' CHECK(protocol IN ('torrent','usenet'));
ALTER TABLE downloads ADD COLUMN client_id TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN client_files TEXT NOT NULL DEFAULT '';
ALTER TABLE downloads ADD COLUMN client_storage TEXT NOT NULL DEFAULT '';
