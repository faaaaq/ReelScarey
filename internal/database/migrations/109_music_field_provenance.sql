ALTER TABLE music_albums ADD COLUMN title_overridden INTEGER NOT NULL DEFAULT 0 CHECK(title_overridden IN (0,1));
ALTER TABLE music_albums ADD COLUMN year_overridden INTEGER NOT NULL DEFAULT 0 CHECK(year_overridden IN (0,1));
ALTER TABLE music_artists ADD COLUMN name_overridden INTEGER NOT NULL DEFAULT 0 CHECK(name_overridden IN (0,1));
-- Empty means ordinary retagging. Explicit catalog corrections persist their
-- field mask so a restart cannot turn them into, or lose them as, overrides.
ALTER TABLE music_edit_jobs ADD COLUMN override_fields TEXT NOT NULL DEFAULT '';
