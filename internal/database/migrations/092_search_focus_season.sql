-- A season-scoped search requested from a library page narrows the next
-- scheduled run to one show season: 0 means no season focus.
ALTER TABLE search_schedule ADD COLUMN focus_season INTEGER NOT NULL DEFAULT 0;
