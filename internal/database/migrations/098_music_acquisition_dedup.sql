ALTER TABLE music_acquisition_jobs ADD COLUMN dedup_key TEXT;
CREATE UNIQUE INDEX one_active_music_acquisition ON music_acquisition_jobs(dedup_key) WHERE dedup_key IS NOT NULL AND state IN ('queued','running');
