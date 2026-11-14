-- Durable deferral of feed releases discovered before their episode's air
-- date. The documented policy grabs TV episodes on or after their listed air
-- date; a feed entry that arrives earlier is recorded once (not_aired) and its
-- sealed release payload is kept here, so a release found only by the feed
-- check is reconsidered at the air date even when the feed no longer carries
-- the entry. Bounded exactly like the announce deferral: at most 8 claims and
-- a 30-day horizon, each bound producing an explicit, observable terminal
-- outcome instead of silent loss. The payload is sealed with the release
-- key: it carries the release identity (provider link, size, seeders) and no
-- credential, and it is unsealed only by transfer at wake time.
CREATE TABLE rss_deferrals (
    id INTEGER PRIMARY KEY,
    indexer_id TEXT NOT NULL,
    indexer_name TEXT NOT NULL DEFAULT '',
    release_key TEXT NOT NULL,
    release_title TEXT NOT NULL,
    media_id INTEGER NOT NULL,
    episode_id INTEGER NOT NULL,
    target_key TEXT NOT NULL DEFAULT '',
    air_date TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    seeders INTEGER,
    protocol TEXT NOT NULL DEFAULT '',
    published_at TEXT NOT NULL DEFAULT '',
    payload BLOB NOT NULL,
    state TEXT NOT NULL DEFAULT 'deferred' CHECK(state IN ('deferred','queued','expired','rejected','stale')),
    outcome TEXT NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    download_id TEXT,
    next_run INTEGER NOT NULL DEFAULT 0,
    attempts INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX rss_deferrals_release ON rss_deferrals(indexer_id, release_key);
CREATE INDEX rss_deferrals_due ON rss_deferrals(state, next_run);
