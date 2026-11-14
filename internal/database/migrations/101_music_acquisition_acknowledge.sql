-- Acknowledged acquisition jobs no longer count as "needs attention".
-- acknowledged_at is a unix timestamp; 0 means the job still needs attention.
ALTER TABLE music_acquisition_jobs ADD COLUMN acknowledged_at INTEGER NOT NULL DEFAULT 0;
