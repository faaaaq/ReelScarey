-- A title search requested from a library page scopes the next scheduled run
-- to one show, movie or episode instead of the whole library.
ALTER TABLE search_schedule ADD COLUMN focus_media TEXT NOT NULL DEFAULT '';
ALTER TABLE search_schedule ADD COLUMN focus_episode TEXT NOT NULL DEFAULT '';
