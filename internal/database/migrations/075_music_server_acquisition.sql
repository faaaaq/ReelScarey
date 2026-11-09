-- Opt-in music server, link acquisitions and playlist monitoring.

CREATE TABLE music_server_settings (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    revision INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL
);
INSERT INTO music_server_settings(id, updated_at) VALUES (1, unixepoch());

CREATE TABLE music_acquisition_jobs (
    id INTEGER PRIMARY KEY,
    source_url TEXT NOT NULL DEFAULT '',
    source_kind TEXT NOT NULL DEFAULT 'link',
    title TEXT NOT NULL DEFAULT '',
    root TEXT NOT NULL,
    format TEXT NOT NULL DEFAULT 'source',
    state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','running','completed','failed','cancelled')),
    track_count INTEGER NOT NULL DEFAULT 0,
    completed_count INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX music_acquisition_jobs_state ON music_acquisition_jobs(state,id);

CREATE TABLE music_acquisition_tracks (
    id INTEGER PRIMARY KEY,
    job_id INTEGER NOT NULL REFERENCES music_acquisition_jobs(id) ON DELETE CASCADE,
    position INTEGER NOT NULL,
    title TEXT NOT NULL,
    artist TEXT NOT NULL,
    album TEXT NOT NULL,
    album_artist TEXT NOT NULL DEFAULT '',
    year INTEGER,
    track_number INTEGER NOT NULL DEFAULT 1 CHECK(track_number > 0),
    disc_number INTEGER NOT NULL DEFAULT 1 CHECK(disc_number > 0),
    duration_ms INTEGER NOT NULL DEFAULT 0,
    cover_url TEXT NOT NULL DEFAULT '',
    source_url TEXT NOT NULL DEFAULT '',
    search_query TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','downloading','importing','completed','failed','skipped')),
    error TEXT NOT NULL DEFAULT '',
    music_file_id INTEGER,
    import_job_id INTEGER,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(job_id, position)
);
CREATE INDEX music_acquisition_tracks_job ON music_acquisition_tracks(job_id,state,position);

CREATE TABLE music_monitor_settings (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    interval_hours INTEGER NOT NULL DEFAULT 24 CHECK(interval_hours BETWEEN 1 AND 168),
    next_run INTEGER NOT NULL DEFAULT 0,
    last_run INTEGER NOT NULL DEFAULT 0,
    lease_until INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL
);
INSERT INTO music_monitor_settings(id, updated_at) VALUES (1, unixepoch());

CREATE TABLE music_monitor_sources (
    id INTEGER PRIMARY KEY,
    url TEXT NOT NULL UNIQUE,
    title TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL DEFAULT 'playlist',
    provider TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)),
    last_checked INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX music_monitor_sources_enabled ON music_monitor_sources(enabled,id);

CREATE TABLE music_monitor_seen (
    source_id INTEGER NOT NULL REFERENCES music_monitor_sources(id) ON DELETE CASCADE,
    track_key TEXT NOT NULL,
    first_seen INTEGER NOT NULL,
    PRIMARY KEY(source_id, track_key)
);
