-- Subtitle options and evidence are bound to the same reviewed video job.
CREATE TABLE youtube_subtitle_options (
 review_id TEXT PRIMARY KEY REFERENCES youtube_reviews(id),
 languages_json TEXT NOT NULL,
 automatic INTEGER NOT NULL CHECK(automatic IN (0,1))
);
CREATE TABLE youtube_subtitle_sources (
 job_id TEXT NOT NULL REFERENCES youtube_jobs(id),
 language TEXT NOT NULL,
 source_path TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes BETWEEN 1 AND 4194304),
 source_mtime INTEGER NOT NULL,
 sha256 TEXT NOT NULL,
 PRIMARY KEY(job_id,language)
);
CREATE TABLE youtube_subtitle_imports (
 job_id TEXT NOT NULL REFERENCES youtube_jobs(id),
 language TEXT NOT NULL,
 source_path TEXT NOT NULL,
 destination_path TEXT NOT NULL UNIQUE,
 stage_name TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes BETWEEN 1 AND 4194304),
 source_mtime INTEGER NOT NULL,
 sha256 TEXT NOT NULL,
 phase TEXT NOT NULL CHECK(phase IN ('planned','staged','published','committed')),
 PRIMARY KEY(job_id,language)
);
CREATE TABLE youtube_subtitle_files (
 job_id TEXT NOT NULL REFERENCES youtube_jobs(id),
 language TEXT NOT NULL,
 relative_path TEXT NOT NULL UNIQUE,
 sha256 TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes BETWEEN 1 AND 4194304),
 PRIMARY KEY(job_id,language)
);
