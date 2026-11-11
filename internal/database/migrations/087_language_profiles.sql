-- Ordered subtitle language profiles. Global subtitle preferences remain the
-- fallback when no library or item profile is assigned.
CREATE TABLE language_profiles (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
    subtitle_tracks TEXT NOT NULL DEFAULT '[]' CHECK(length(subtitle_tracks)<=4096),
    audio_languages TEXT NOT NULL DEFAULT '[]' CHECK(length(audio_languages)<=1024),
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
CREATE TABLE library_language_defaults (
    library_key TEXT PRIMARY KEY CHECK(library_key IN ('movies','tv','anime')),
    profile_id INTEGER REFERENCES language_profiles(id) ON DELETE SET NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO library_language_defaults(library_key) VALUES('movies'),('tv'),('anime');
CREATE TABLE item_language_profiles (
    media_id INTEGER PRIMARY KEY REFERENCES media_items(id) ON DELETE CASCADE,
    profile_id INTEGER NOT NULL REFERENCES language_profiles(id) ON DELETE CASCADE,
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
