CREATE TABLE subtitle_overrides (
 media_id INTEGER PRIMARY KEY REFERENCES media_items(id) ON DELETE CASCADE,
 enabled INTEGER NOT NULL DEFAULT 1, tracks TEXT NOT NULL,
 hearing_impaired TEXT NOT NULL, match_policy TEXT NOT NULL,
 revision INTEGER NOT NULL DEFAULT 1
);
