CREATE TABLE series_details (
    media_id INTEGER PRIMARY KEY REFERENCES media_items(id),
    series_status TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO series_details(media_id) SELECT id FROM media_items WHERE kind='series';
CREATE TABLE seasons (
    id INTEGER PRIMARY KEY,
    series_id INTEGER NOT NULL REFERENCES series_details(media_id),
    tmdb_id INTEGER NOT NULL,
    number INTEGER NOT NULL CHECK(number >= 0),
    title TEXT NOT NULL,
    overview TEXT NOT NULL,
    air_date TEXT NOT NULL,
    episode_count INTEGER NOT NULL CHECK(episode_count >= 0),
    monitored INTEGER NOT NULL DEFAULT 1 CHECK(monitored IN (0,1)),
    loaded_at TEXT NOT NULL DEFAULT '',
    UNIQUE(series_id,number)
);
CREATE TABLE episodes (
    id INTEGER PRIMARY KEY,
    season_id INTEGER NOT NULL REFERENCES seasons(id),
    tmdb_id INTEGER NOT NULL,
    number INTEGER NOT NULL CHECK(number > 0),
    title TEXT NOT NULL,
    overview TEXT NOT NULL,
    air_date TEXT NOT NULL,
    runtime_minutes INTEGER,
    monitored INTEGER NOT NULL DEFAULT 1 CHECK(monitored IN (0,1)),
    UNIQUE(season_id,number)
);
