CREATE TABLE related_titles (
 left_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
 right_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
 created_at INTEGER NOT NULL DEFAULT (unixepoch()),
 PRIMARY KEY(left_id,right_id),
 CHECK(left_id<right_id)
);
