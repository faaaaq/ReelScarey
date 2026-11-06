-- Quality presets, live progress and series-source provenance for YouTube
-- downloads. Existing jobs keep the default "best" preset and zero progress.
ALTER TABLE youtube_subtitle_options ADD COLUMN quality TEXT NOT NULL DEFAULT 'best';
ALTER TABLE youtube_jobs ADD COLUMN quality TEXT NOT NULL DEFAULT 'best';
ALTER TABLE youtube_jobs ADD COLUMN target_label TEXT NOT NULL DEFAULT '';
ALTER TABLE youtube_jobs ADD COLUMN progress REAL NOT NULL DEFAULT 0;
ALTER TABLE youtube_jobs ADD COLUMN downloaded_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE youtube_jobs ADD COLUMN total_bytes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE youtube_jobs ADD COLUMN speed_bps INTEGER NOT NULL DEFAULT 0;
ALTER TABLE youtube_jobs ADD COLUMN eta_seconds INTEGER NOT NULL DEFAULT 0;
ALTER TABLE youtube_jobs ADD COLUMN auto_import INTEGER NOT NULL DEFAULT 0;
ALTER TABLE youtube_jobs ADD COLUMN series_source_id TEXT NOT NULL DEFAULT '';
