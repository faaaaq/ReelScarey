-- Per-show YouTube sources for monitored series. A source is an explicit
-- channel or playlist with an explicit mapping (SxxExx titles or playlist
-- order), never a title search. Entries record the baseline and every queued or
-- skipped video so a repeated poll cannot queue the same episode twice.
CREATE TABLE youtube_series_sources (
    id TEXT PRIMARY KEY,
    media_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    source_url TEXT NOT NULL,
    title TEXT NOT NULL,
    mapping TEXT NOT NULL CHECK(mapping IN ('episode','order')),
    season_number INTEGER NOT NULL DEFAULT 0 CHECK(season_number >= 0),
    start_episode INTEGER NOT NULL DEFAULT 1 CHECK(start_episode > 0),
    quality TEXT NOT NULL DEFAULT 'best',
    subtitles_json TEXT NOT NULL DEFAULT '{"languages":[],"automatic":false,"quality":"best"}',
    auto_import INTEGER NOT NULL DEFAULT 1 CHECK(auto_import IN (0,1)),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    interval_hours INTEGER NOT NULL DEFAULT 6 CHECK(interval_hours BETWEEN 1 AND 168),
    initialized INTEGER NOT NULL DEFAULT 0 CHECK(initialized IN (0,1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
    requested INTEGER NOT NULL DEFAULT 0 CHECK(requested IN (0,1)),
    next_run INTEGER NOT NULL DEFAULT 0,
    last_checked INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'idle',
    message TEXT NOT NULL DEFAULT '',
    target_snapshot TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX youtube_series_source_url ON youtube_series_sources(media_id, source_url);
CREATE TABLE youtube_series_entries (
    source_id TEXT NOT NULL REFERENCES youtube_series_sources(id) ON DELETE CASCADE,
    video_id TEXT NOT NULL,
    position INTEGER NOT NULL DEFAULT 0,
    title TEXT NOT NULL,
    channel TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'new',
    episode_id INTEGER REFERENCES episodes(id),
    job_id TEXT REFERENCES youtube_jobs(id),
    note TEXT NOT NULL DEFAULT '',
    discovered_at INTEGER NOT NULL,
    PRIMARY KEY(source_id, video_id)
);
CREATE INDEX youtube_series_entries_state ON youtube_series_entries(source_id, state, discovered_at);
