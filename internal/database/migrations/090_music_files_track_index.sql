-- Index music_files.track_id.
--
-- The music-server song queries resolve each track's playable file with a
-- correlated "MIN(f2.id) WHERE f2.track_id = t.id" subquery. Without an index
-- on track_id SQLite scans every row of music_files for every track, which is
-- quadratic: on an ~11k-track / ~11.8k-file library getRandomSongs took ~10s.
-- This index turns that lookup into a covering index seek (measured 9.1s ->
-- 0.006s) and also speeds up album, directory and search song listings, which
-- use the same lookup.
CREATE INDEX music_files_track ON music_files(track_id);
