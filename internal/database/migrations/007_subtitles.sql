CREATE TABLE integration_configs_next (
 name TEXT PRIMARY KEY CHECK(name IN ('torznab','qbittorrent','opensubtitles')),
 ciphertext BLOB NOT NULL,
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO integration_configs_next SELECT * FROM integration_configs;
DROP TABLE integration_configs;
ALTER TABLE integration_configs_next RENAME TO integration_configs;

CREATE TABLE subtitle_preferences (
 id INTEGER PRIMARY KEY CHECK(id=1),
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 tracks TEXT NOT NULL DEFAULT '[{"language":"en","variant":"full"},{"language":"en","variant":"signs"},{"language":"en","variant":"forced"}]',
 hearing_impaired TEXT NOT NULL DEFAULT 'any' CHECK(hearing_impaired IN ('any','exclude','prefer')),
 match_policy TEXT NOT NULL DEFAULT 'hash_or_release' CHECK(match_policy IN ('hash','hash_or_release')),
 interval_minutes INTEGER NOT NULL DEFAULT 360 CHECK(interval_minutes BETWEEN 60 AND 10080),
 only_monitored INTEGER NOT NULL DEFAULT 1 CHECK(only_monitored IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1,
 discovery_cursor INTEGER NOT NULL DEFAULT 0
);
INSERT INTO subtitle_preferences(id) VALUES(1);
CREATE TABLE subtitle_provider_state (
 id INTEGER PRIMARY KEY CHECK(id=1),
 state TEXT NOT NULL DEFAULT 'ready',
 message TEXT NOT NULL DEFAULT '',
 cooldown_until INTEGER NOT NULL DEFAULT 0,
 remaining INTEGER,
 generation TEXT NOT NULL DEFAULT ''
);
INSERT INTO subtitle_provider_state(id) VALUES(1);
CREATE TABLE subtitle_jobs (
 id TEXT PRIMARY KEY,
 media_file_id INTEGER NOT NULL UNIQUE REFERENCES media_files(id),
 mode TEXT NOT NULL DEFAULT 'automatic' CHECK(mode IN ('automatic','review','download')),
 requested INTEGER NOT NULL DEFAULT 0 CHECK(requested IN (0,1)),
 selected_candidate TEXT NOT NULL DEFAULT '',
 state TEXT NOT NULL DEFAULT 'queued',
 message TEXT NOT NULL DEFAULT '',
 attempts INTEGER NOT NULL DEFAULT 0,
 next_run INTEGER NOT NULL DEFAULT 0,
 lease_owner TEXT NOT NULL DEFAULT '',
 lease_until INTEGER NOT NULL DEFAULT 0,
 revision INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE INDEX subtitle_job_claim ON subtitle_jobs(next_run,lease_until);
CREATE TABLE subtitle_candidates (
 id TEXT PRIMARY KEY,
 media_file_id INTEGER NOT NULL REFERENCES media_files(id),
 payload TEXT NOT NULL,
 generation TEXT NOT NULL,
 revision INTEGER NOT NULL,
 media_sha256 TEXT NOT NULL,
 expires_at INTEGER NOT NULL
);
CREATE INDEX subtitle_candidate_media ON subtitle_candidates(media_file_id,expires_at);
CREATE TABLE subtitle_inspections (
 media_file_id INTEGER PRIMARY KEY REFERENCES media_files(id),
 sha256 TEXT NOT NULL,
 size_bytes INTEGER NOT NULL,
 mtime INTEGER NOT NULL,
 moviehash TEXT NOT NULL,
 probe_json TEXT NOT NULL
);
CREATE TABLE subtitles (
 media_file_id INTEGER NOT NULL REFERENCES media_files(id),
 language TEXT NOT NULL,
 variant TEXT NOT NULL CHECK(variant IN ('full','signs','forced')),
 source TEXT NOT NULL CHECK(source IN ('downloaded','existing','embedded')),
 relative_path TEXT NOT NULL DEFAULT '',
 sha256 TEXT NOT NULL DEFAULT '',
 provider_file_id INTEGER,
 state TEXT NOT NULL DEFAULT 'available',
 message TEXT NOT NULL DEFAULT '',
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(media_file_id,language,variant)
);
CREATE TABLE subtitle_imports (
 id TEXT NOT NULL UNIQUE,
 media_file_id INTEGER NOT NULL REFERENCES media_files(id),
 language TEXT NOT NULL,
 variant TEXT NOT NULL,
 provider_file_id INTEGER NOT NULL,
 destination TEXT NOT NULL UNIQUE,
 stage_name TEXT NOT NULL,
 sha256 TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes BETWEEN 1 AND 4194304),
 content BLOB,
 phase TEXT NOT NULL CHECK(phase IN ('planned','staged','published','committed')),
 updated_at INTEGER NOT NULL,
 PRIMARY KEY(media_file_id,language,variant)
);
