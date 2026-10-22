CREATE TABLE settings (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    library_name TEXT NOT NULL,
    movie_folder TEXT NOT NULL,
    series_folder TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK (revision > 0),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
INSERT INTO settings (id, library_name, movie_folder, series_folder)
VALUES (1, 'My library', 'Movies', 'TV Shows');

CREATE TABLE media_items (
    id INTEGER PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('movie', 'series')),
    tmdb_id INTEGER NOT NULL CHECK (tmdb_id > 0),
    title TEXT NOT NULL CHECK (length(title) > 0),
    year INTEGER CHECK (year BETWEEN 1800 AND 9999),
    monitored INTEGER NOT NULL DEFAULT 1 CHECK (monitored IN (0, 1)),
    status TEXT NOT NULL DEFAULT 'missing' CHECK (status IN ('missing', 'downloading', 'available')),
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    UNIQUE (kind, tmdb_id)
);
