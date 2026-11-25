-- Durable music quality-upgrade jobs. One job upgrades a whole library,
-- artist, album or a single track to a better source copy, keeping the
-- original file registered as an archive variant. The per-hour rate is fixed
-- in the application and recorded here only as evidence for the UI.
CREATE TABLE music_upgrade_jobs (
    id INTEGER PRIMARY KEY,
    scope TEXT NOT NULL CHECK(scope IN ('library','artist','album','track')),
    scope_id INTEGER NOT NULL DEFAULT 0,
    scope_name TEXT NOT NULL DEFAULT '',
    target_quality TEXT NOT NULL CHECK(target_quality IN ('hires','lossless','high')),
    max_bitrate_kbps INTEGER NOT NULL DEFAULT 0 CHECK(max_bitrate_kbps >= 0),
    rate_per_hour INTEGER NOT NULL DEFAULT 200 CHECK(rate_per_hour = 200),
    state TEXT NOT NULL CHECK(state IN ('queued','running','completed','failed','cancelled')),
    total_count INTEGER NOT NULL DEFAULT 0,
    processed_count INTEGER NOT NULL DEFAULT 0,
    upgraded_count INTEGER NOT NULL DEFAULT 0,
    skipped_count INTEGER NOT NULL DEFAULT 0,
    failed_count INTEGER NOT NULL DEFAULT 0,
    current_track TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX music_upgrade_jobs_state ON music_upgrade_jobs(state, id);

CREATE TABLE music_upgrade_items (
    id INTEGER PRIMARY KEY,
    job_id INTEGER NOT NULL REFERENCES music_upgrade_jobs(id) ON DELETE CASCADE,
    track_id INTEGER NOT NULL REFERENCES music_tracks(id) ON DELETE CASCADE,
    music_file_id INTEGER NOT NULL REFERENCES music_files(id) ON DELETE CASCADE,
    -- Canonical identities and original-file evidence are immutable snapshots.
    artist_id INTEGER NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
    album_id INTEGER NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    old_root TEXT NOT NULL DEFAULT '',
    old_relative_path TEXT NOT NULL DEFAULT '',
    old_sha256 TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL,
    artist TEXT NOT NULL,
    album TEXT NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    old_bitrate INTEGER NOT NULL DEFAULT 0,
    old_format TEXT NOT NULL DEFAULT '',
    old_sample_rate INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL CHECK(state IN ('pending','searching','downloading','upgraded','skipped','failed')),
    detail TEXT NOT NULL DEFAULT '',
    selected_source_id TEXT NOT NULL DEFAULT '',
    selected_source_url TEXT NOT NULL DEFAULT '',
    candidate_evidence_json TEXT NOT NULL DEFAULT '',
    new_bitrate INTEGER NOT NULL DEFAULT 0,
    new_format TEXT NOT NULL DEFAULT '',
    new_sample_rate INTEGER NOT NULL DEFAULT 0,
    -- Attempt timestamps and the global clock use milliseconds. Job created/
    -- updated timestamps and lease_until retain the schema's unix seconds.
    attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts >= 0),
    not_before INTEGER NOT NULL DEFAULT 0,
    last_attempt_at INTEGER NOT NULL DEFAULT 0,
    -- A bounded upgradePublication document makes a crash between staging,
    -- linking the new sibling and committing the catalog replayable.
    publication TEXT NOT NULL DEFAULT '',
    new_music_file_id INTEGER REFERENCES music_files(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(job_id, music_file_id)
);
CREATE INDEX music_upgrade_items_job ON music_upgrade_items(job_id, state, id);
CREATE INDEX music_upgrade_items_original ON music_upgrade_items(music_file_id, job_id);

-- An active original has exactly one owner across overlapping library, artist,
-- album and track jobs. The guard is written in the enqueue transaction and
-- removed only when that job reaches a terminal state (or is cancelled).
CREATE TABLE music_upgrade_active (
    music_file_id INTEGER PRIMARY KEY REFERENCES music_files(id) ON DELETE CASCADE,
    job_id INTEGER NOT NULL REFERENCES music_upgrade_jobs(id) ON DELETE CASCADE,
    item_id INTEGER NOT NULL UNIQUE REFERENCES music_upgrade_items(id) ON DELETE CASCADE
);
CREATE INDEX music_upgrade_active_job ON music_upgrade_active(job_id);

-- One durable clock spaces item claims across every job and every Service
-- instance. RecoverUpgradeJobs deliberately does not reset this row.
CREATE TABLE music_upgrade_rate (
    id INTEGER PRIMARY KEY CHECK(id = 1),
    next_attempt_ms INTEGER NOT NULL DEFAULT 0
);
INSERT INTO music_upgrade_rate(id, next_attempt_ms) VALUES(1, 0);
