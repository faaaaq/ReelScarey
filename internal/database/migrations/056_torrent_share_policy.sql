-- Automatic seeding limits for the supervised built-in qBittorrent client.
-- qBittorrent only pauses on its own share limits and never deletes data, so
-- ReelScarey enforces this single-row policy itself. Zero disables an
-- individual rule; an enabled policy must have at least one rule.
CREATE TABLE torrent_share_policy (
    id INTEGER PRIMARY KEY CHECK(id=1),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN(0,1)),
    action TEXT NOT NULL DEFAULT 'pause' CHECK(action IN('pause','remove','remove_and_delete')),
    max_ratio REAL NOT NULL DEFAULT 2.0 CHECK(max_ratio=0 OR (max_ratio>=0.1 AND max_ratio<=1000)),
    max_seeding_minutes INTEGER NOT NULL DEFAULT 0 CHECK(max_seeding_minutes BETWEEN 0 AND 525600),
    include_reelscarey INTEGER NOT NULL DEFAULT 0 CHECK(include_reelscarey IN(0,1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
    updated_at INTEGER NOT NULL DEFAULT (unixepoch()),
    CHECK(enabled=0 OR max_ratio>0 OR max_seeding_minutes>0)
);
INSERT INTO torrent_share_policy(id) VALUES(1);
