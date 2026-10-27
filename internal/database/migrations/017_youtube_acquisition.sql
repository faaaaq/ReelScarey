-- Persist the exact verified acquisition for an explicit later import review.
ALTER TABLE youtube_jobs ADD COLUMN source_size INTEGER NOT NULL DEFAULT 0;
ALTER TABLE youtube_jobs ADD COLUMN source_mtime INTEGER NOT NULL DEFAULT 0;
ALTER TABLE youtube_jobs ADD COLUMN source_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE youtube_jobs ADD COLUMN probe_json TEXT NOT NULL DEFAULT '';
