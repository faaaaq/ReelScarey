-- Sonarr/Radarr-style release detection. The feed check is enabled by default
-- every 15 minutes (owner decision in 0.40) and only grabs monitored missing
-- titles; it never replaces a registered file.
CREATE TABLE rss_sync (
    id INTEGER PRIMARY KEY CHECK(id=1),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
    interval_minutes INTEGER NOT NULL DEFAULT 15 CHECK(interval_minutes BETWEEN 5 AND 1440),
    revision INTEGER NOT NULL DEFAULT 1,
    next_run INTEGER NOT NULL DEFAULT 0,
    requested INTEGER NOT NULL DEFAULT 0 CHECK(requested IN (0,1)),
    last_run INTEGER NOT NULL DEFAULT 0,
    checked INTEGER NOT NULL DEFAULT 0,
    queued INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'scheduled',
    message TEXT NOT NULL DEFAULT 'Waiting for the next release check.',
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0
);
INSERT INTO rss_sync(id) VALUES(1);

-- Feed entries are processed once per indexer. The key is a bounded hash, never
-- a download URL or credential.
CREATE TABLE rss_seen (
    indexer_id TEXT NOT NULL,
    release_key TEXT NOT NULL,
    first_seen INTEGER NOT NULL,
    PRIMARY KEY(indexer_id, release_key)
) WITHOUT ROWID;
CREATE INDEX rss_seen_first_seen ON rss_seen(first_seen);

CREATE TABLE rss_runs (
    id TEXT PRIMARY KEY,
    started_at INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    indexers INTEGER NOT NULL DEFAULT 0,
    checked INTEGER NOT NULL DEFAULT 0,
    queued INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE rss_results (
    id INTEGER PRIMARY KEY,
    run_id TEXT NOT NULL,
    indexer_id TEXT NOT NULL DEFAULT '',
    indexer_name TEXT NOT NULL DEFAULT '',
    media_id INTEGER,
    episode_id INTEGER,
    target TEXT NOT NULL DEFAULT '',
    release_title TEXT NOT NULL,
    outcome TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    download_id TEXT,
    created_at INTEGER NOT NULL
);
CREATE INDEX rss_results_run ON rss_results(run_id,id);
CREATE INDEX rss_results_created ON rss_results(created_at);
