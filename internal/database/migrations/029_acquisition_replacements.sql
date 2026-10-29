CREATE TABLE acquisition_replacements (
 source_kind TEXT NOT NULL CHECK(source_kind IN ('download','youtube')),
 job_id TEXT NOT NULL,
 media_file_id INTEGER NOT NULL REFERENCES media_files(id),
 target_sha TEXT NOT NULL,
 source_path TEXT NOT NULL DEFAULT '',
 size_bytes INTEGER NOT NULL DEFAULT 0,
 sha256 TEXT NOT NULL DEFAULT '',
 probe_json TEXT NOT NULL DEFAULT '',
 file_index INTEGER NOT NULL DEFAULT -1,
 plan_id TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(source_kind,job_id)
);
