-- Add source provenance without rebuilding the shared media_files parent.
-- Catalog imports use its existing-file registration contract after verified
-- publication; this table records the actual YouTube source alongside it.
CREATE TABLE youtube_files (
 job_id TEXT PRIMARY KEY REFERENCES youtube_jobs(id),
 collection_id INTEGER REFERENCES youtube_collections(id),
 media_file_id INTEGER UNIQUE REFERENCES media_files(id),
 video_id TEXT NOT NULL,
 title TEXT NOT NULL,
 relative_path TEXT NOT NULL UNIQUE,
 size_bytes INTEGER NOT NULL CHECK(size_bytes>0),
 sha256 TEXT NOT NULL,
 probe_json TEXT NOT NULL,
 imported_at INTEGER NOT NULL,
 CHECK((collection_id IS NULL) <> (media_file_id IS NULL))
);
CREATE UNIQUE INDEX youtube_collection_video ON youtube_files(collection_id,video_id) WHERE collection_id IS NOT NULL;
