CREATE TABLE anime_search_settings (
 media_id INTEGER PRIMARY KEY REFERENCES media_items(id),
 mode TEXT NOT NULL DEFAULT 'disabled' CHECK(mode IN ('disabled','season','absolute')),
 search_title TEXT NOT NULL DEFAULT '',
 alternate_titles TEXT NOT NULL DEFAULT '[]',
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0)
);
ALTER TABLE downloads ADD COLUMN anime_search_snapshot TEXT NOT NULL DEFAULT '';
