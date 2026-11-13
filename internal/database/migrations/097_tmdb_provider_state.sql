CREATE TABLE tmdb_provider_state (
 id INTEGER PRIMARY KEY CHECK(id = 1),
 cooldown_until INTEGER NOT NULL DEFAULT 0,
 message TEXT NOT NULL DEFAULT '',
 updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO tmdb_provider_state(id) VALUES(1);
