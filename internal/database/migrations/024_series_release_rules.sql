CREATE TABLE series_release_rules (
 media_id INTEGER PRIMARY KEY REFERENCES media_items(id) ON DELETE CASCADE,
 codec TEXT NOT NULL DEFAULT '' CHECK(codec IN ('','h264','h265')),
 release_group TEXT NOT NULL DEFAULT '',
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>0)
);
