ALTER TABLE media_items ADD COLUMN category TEXT NOT NULL DEFAULT 'standard' CHECK(category IN ('standard','anime'));
ALTER TABLE media_items ADD COLUMN category_revision INTEGER NOT NULL DEFAULT 1 CHECK(category_revision>0);
CREATE TABLE anime_episode_numbers (
  episode_id INTEGER PRIMARY KEY REFERENCES episodes(id) ON DELETE CASCADE,
  series_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
  absolute_number INTEGER NOT NULL CHECK(absolute_number BETWEEN 1 AND 100000),
  UNIQUE(series_id,absolute_number)
);
