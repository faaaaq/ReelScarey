ALTER TABLE music_artists ADD COLUMN editor_fields TEXT NOT NULL DEFAULT '{}';
ALTER TABLE music_artists ADD COLUMN custom_artwork BLOB;
ALTER TABLE music_artists ADD COLUMN custom_artwork_type TEXT NOT NULL DEFAULT '';
ALTER TABLE music_artists ADD COLUMN custom_artwork_key TEXT NOT NULL DEFAULT '';
ALTER TABLE music_albums ADD COLUMN editor_fields TEXT NOT NULL DEFAULT '{}';
ALTER TABLE music_albums ADD COLUMN custom_artwork BLOB;
ALTER TABLE music_albums ADD COLUMN custom_artwork_type TEXT NOT NULL DEFAULT '';
ALTER TABLE music_albums ADD COLUMN custom_artwork_key TEXT NOT NULL DEFAULT '';
ALTER TABLE music_tracks ADD COLUMN editor_fields TEXT NOT NULL DEFAULT '{}';
