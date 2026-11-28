ALTER TABLE music_artists ADD COLUMN editor_fields TEXT NOT NULL DEFAULT '{}';
ALTER TABLE music_artists ADD COLUMN custom_artwork BLOB;
ALTER TABLE music_artists ADD COLUMN custom_artwork_type TEXT NOT NULL DEFAULT '';
ALTER TABLE music_artists ADD COLUMN custom_artwork_key TEXT NOT NULL DEFAULT '';
ALTER TABLE music_albums ADD COLUMN editor_fields TEXT NOT NULL DEFAULT '{}';
ALTER TABLE music_albums ADD COLUMN custom_artwork BLOB;
ALTER TABLE music_albums ADD COLUMN custom_artwork_type TEXT NOT NULL DEFAULT '';
ALTER TABLE music_albums ADD COLUMN custom_artwork_key TEXT NOT NULL DEFAULT '';
ALTER TABLE music_tracks ADD COLUMN editor_fields TEXT NOT NULL DEFAULT '{}';

-- An explicit override or restoration already has its own file-write policy.
-- Only ordinary provider refreshes should queue a background retag here.
DROP TRIGGER music_album_metadata_refreshed;
CREATE TRIGGER music_album_metadata_refreshed AFTER UPDATE OF title ON music_albums
 WHEN OLD.title<>NEW.title AND OLD.title_overridden=0 AND NEW.title_overridden=0 BEGIN
 INSERT INTO music_title_updates(music_file_id) SELECT id FROM music_files WHERE album_id=NEW.id AND track_id IS NOT NULL
 ON CONFLICT(music_file_id) DO UPDATE SET revision=revision+1,next_attempt=0;
END;

DROP TRIGGER music_artist_metadata_refreshed;
CREATE TRIGGER music_artist_metadata_refreshed AFTER UPDATE OF name ON music_artists
 WHEN OLD.name<>NEW.name AND OLD.name_overridden=0 AND NEW.name_overridden=0 BEGIN
 INSERT INTO music_title_updates(music_file_id) SELECT id FROM music_files WHERE artist_id=NEW.id AND track_id IS NOT NULL
 ON CONFLICT(music_file_id) DO UPDATE SET revision=revision+1,next_attempt=0;
END;
