-- Auto-pause finished torrents from public sources in the supervised built-in
-- client. Public sources are the public Nyaa indexer and manually uploaded
-- torrents; private-tracker downloads keep seeding, and torrents added to the
-- client outside ReelScarey are left alone. Queued downloads record the
-- classification at queue time; existing rows keep 0, the fail-safe "never
-- auto-pause" value.
CREATE TABLE torrent_public_pause (
    id INTEGER PRIMARY KEY CHECK(id=1),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN(0,1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch())
);
INSERT INTO torrent_public_pause(id) VALUES(1);
ALTER TABLE downloads ADD COLUMN indexer_public INTEGER NOT NULL DEFAULT 0 CHECK(indexer_public IN(0,1));
ALTER TABLE music_downloads ADD COLUMN indexer_public INTEGER NOT NULL DEFAULT 0 CHECK(indexer_public IN(0,1));
