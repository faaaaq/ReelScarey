CREATE TABLE anime_search_settings_next (
 media_id INTEGER PRIMARY KEY REFERENCES media_items(id),
 mode TEXT NOT NULL DEFAULT 'disabled' CHECK(mode IN ('disabled','season','absolute','mapped')),
 search_title TEXT NOT NULL DEFAULT '',
 alternate_titles TEXT NOT NULL DEFAULT '[]',
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0),
 include_specials INTEGER NOT NULL DEFAULT 0 CHECK(include_specials IN (0,1)),
 allow_packs INTEGER NOT NULL DEFAULT 0 CHECK(allow_packs IN (0,1)),
 max_pack_episodes INTEGER NOT NULL DEFAULT 20 CHECK(max_pack_episodes BETWEEN 2 AND 100),
 approved_order_revision INTEGER NOT NULL DEFAULT 0 CHECK(approved_order_revision>=0)
);
INSERT INTO anime_search_settings_next(media_id,mode,search_title,alternate_titles,revision)
 SELECT media_id,mode,search_title,alternate_titles,revision FROM anime_search_settings;
DROP TABLE anime_search_settings;
ALTER TABLE anime_search_settings_next RENAME TO anime_search_settings;
