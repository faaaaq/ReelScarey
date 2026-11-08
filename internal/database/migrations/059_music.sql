CREATE TABLE music_artists (
    id INTEGER PRIMARY KEY,
    musicbrainz_id TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    sort_name TEXT NOT NULL DEFAULT '',
    disambiguation TEXT NOT NULL DEFAULT '',
    country TEXT NOT NULL DEFAULT '',
    artist_type TEXT NOT NULL DEFAULT '',
    overview TEXT NOT NULL DEFAULT '',
    image_url TEXT NOT NULL DEFAULT '',
    monitored INTEGER NOT NULL DEFAULT 1 CHECK(monitored IN (0,1)),
    status TEXT NOT NULL DEFAULT 'missing' CHECK(status IN ('missing','available')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE music_albums (
    id INTEGER PRIMARY KEY,
    artist_id INTEGER NOT NULL REFERENCES music_artists(id) ON DELETE CASCADE,
    musicbrainz_id TEXT NOT NULL,
    title TEXT NOT NULL,
    primary_type TEXT NOT NULL DEFAULT '',
    secondary_types TEXT NOT NULL DEFAULT '',
    year INTEGER,
    release_date TEXT NOT NULL DEFAULT '',
    cover_url TEXT NOT NULL DEFAULT '',
    monitored INTEGER NOT NULL DEFAULT 1 CHECK(monitored IN (0,1)),
    status TEXT NOT NULL DEFAULT 'missing' CHECK(status IN ('missing','available')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(artist_id, musicbrainz_id)
);
CREATE INDEX music_albums_artist ON music_albums(artist_id);

CREATE TABLE music_tracks (
    id INTEGER PRIMARY KEY,
    album_id INTEGER NOT NULL REFERENCES music_albums(id) ON DELETE CASCADE,
    musicbrainz_id TEXT NOT NULL DEFAULT '',
    recording_id TEXT NOT NULL DEFAULT '',
    disc INTEGER NOT NULL DEFAULT 1 CHECK(disc > 0),
    number INTEGER NOT NULL CHECK(number > 0),
    title TEXT NOT NULL,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(album_id, disc, number)
);
CREATE INDEX music_tracks_album ON music_tracks(album_id);
