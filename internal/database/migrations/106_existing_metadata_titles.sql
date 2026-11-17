-- Older registrations predate the title queues. Check them through the same
-- verified metadata workers used for new imports; already-correct tags are no-ops.
INSERT OR IGNORE INTO video_title_updates(media_file_id)
 SELECT id FROM media_files;
INSERT OR IGNORE INTO music_title_updates(music_file_id)
 SELECT id FROM music_files WHERE track_id IS NOT NULL;

CREATE TRIGGER music_album_metadata_refreshed AFTER UPDATE OF title ON music_albums WHEN OLD.title<>NEW.title BEGIN
 INSERT INTO music_title_updates(music_file_id) SELECT id FROM music_files WHERE album_id=NEW.id AND track_id IS NOT NULL
 ON CONFLICT(music_file_id) DO UPDATE SET revision=revision+1,next_attempt=0;
END;
CREATE TRIGGER music_artist_metadata_refreshed AFTER UPDATE OF name ON music_artists WHEN OLD.name<>NEW.name BEGIN
 INSERT INTO music_title_updates(music_file_id) SELECT id FROM music_files WHERE artist_id=NEW.id AND track_id IS NOT NULL
 ON CONFLICT(music_file_id) DO UPDATE SET revision=revision+1,next_attempt=0;
END;
