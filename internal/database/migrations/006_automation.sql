CREATE TABLE quality_profiles (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
    qualities TEXT NOT NULL,
    cutoff INTEGER NOT NULL CHECK(cutoff IN (480,720,1080,2160)),
    language TEXT NOT NULL,
    min_seeders INTEGER NOT NULL CHECK(min_seeders BETWEEN 0 AND 10000),
    max_size_gib INTEGER NOT NULL CHECK(max_size_gib BETWEEN 1 AND 500),
    required_terms TEXT NOT NULL DEFAULT '',
    rejected_terms TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1
);
INSERT INTO quality_profiles(id,name,qualities,cutoff,language,min_seeders,max_size_gib)
VALUES(1,'HD balanced','[720,1080]',1080,'any',1,40),(2,'Ultra HD','[1080,2160]',2160,'any',1,100);
CREATE TABLE item_automation (
    media_id INTEGER PRIMARY KEY REFERENCES media_items(id),
    profile_id INTEGER REFERENCES quality_profiles(id),
    revision INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE search_schedule (
    id INTEGER PRIMARY KEY CHECK(id=1),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    mode TEXT NOT NULL DEFAULT 'search' CHECK(mode IN ('search','download')),
    interval_minutes INTEGER NOT NULL DEFAULT 360 CHECK(interval_minutes BETWEEN 15 AND 10080),
    batch_size INTEGER NOT NULL DEFAULT 10 CHECK(batch_size BETWEEN 1 AND 25),
    default_profile_id INTEGER NOT NULL DEFAULT 1 REFERENCES quality_profiles(id),
    revision INTEGER NOT NULL DEFAULT 1,
    next_run INTEGER NOT NULL DEFAULT 0,
    requested INTEGER NOT NULL DEFAULT 0 CHECK(requested IN (0,1)),
    cursor TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'paused',
    message TEXT NOT NULL DEFAULT 'Scheduled searches are paused.'
);
INSERT INTO search_schedule(id) VALUES(1);
CREATE TABLE automation_runs (
    id TEXT PRIMARY KEY,
    started_at INTEGER NOT NULL,
    finished_at INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL,
    message TEXT NOT NULL DEFAULT '',
    mode TEXT NOT NULL,
    checked INTEGER NOT NULL DEFAULT 0,
    queued INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE automation_results (
    id INTEGER PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES automation_runs(id) ON DELETE CASCADE,
    media_id INTEGER NOT NULL REFERENCES media_items(id),
    episode_id INTEGER REFERENCES episodes(id),
    target TEXT NOT NULL,
    outcome TEXT NOT NULL,
    message TEXT NOT NULL,
    decisions TEXT NOT NULL DEFAULT '[]',
    download_id TEXT REFERENCES downloads(id)
);
CREATE INDEX automation_run_results ON automation_results(run_id,id);
