ALTER TABLE music_albums ADD COLUMN allow_lossless INTEGER CHECK(allow_lossless IN (0,1));
ALTER TABLE music_albums ADD COLUMN min_bitrate INTEGER CHECK(min_bitrate IS NULL OR (min_bitrate BETWEEN 0 AND 320));
ALTER TABLE music_albums ADD COLUMN preferred_formats TEXT NOT NULL DEFAULT '';
