-- Optional quality-profile defaults for the built-in libraries. A NULL
-- profile_id keeps the search schedule's default, preserving existing behavior.
CREATE TABLE library_profile_defaults (
    library_key TEXT PRIMARY KEY CHECK(library_key IN ('movies','tv','anime')),
    profile_id INTEGER REFERENCES quality_profiles(id) ON DELETE SET NULL,
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
INSERT INTO library_profile_defaults(library_key) VALUES('movies'),('tv'),('anime');
