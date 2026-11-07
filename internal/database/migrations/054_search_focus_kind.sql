-- A bulk search requested from the Calendar narrows the next scheduled run to
-- one media kind: '' (everything), 'movie' or 'series' (TV episodes).
ALTER TABLE search_schedule ADD COLUMN focus_kind TEXT NOT NULL DEFAULT '';
