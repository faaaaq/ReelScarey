-- Existing selections and interrupted subtitle journals retain their SRT
-- destination. Format changes apply to newly reviewed/provider selections.
ALTER TABLE subtitle_preferences ADD COLUMN download_format TEXT NOT NULL DEFAULT 'srt'
 CHECK(download_format IN ('srt','original'));
