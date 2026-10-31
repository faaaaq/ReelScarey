ALTER TABLE subtitle_imports ADD COLUMN provider TEXT NOT NULL DEFAULT 'opensubtitles';
ALTER TABLE subtitles ADD COLUMN provider TEXT NOT NULL DEFAULT '';
UPDATE subtitles SET provider='opensubtitles' WHERE source='downloaded';
