CREATE TABLE download_clients_next (
 id TEXT PRIMARY KEY, name TEXT NOT NULL COLLATE NOCASE UNIQUE,
 kind TEXT NOT NULL CHECK(kind IN ('qbittorrent','transmission','sabnzbd','nzbget')),
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1, created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
INSERT INTO download_clients_next SELECT * FROM download_clients;
DROP TABLE download_clients;
ALTER TABLE download_clients_next RENAME TO download_clients;
