-- YouTube identities are distinct from provider catalog identities. No TMDb
-- IDs are fabricated for user-created collections or videos.
CREATE TABLE youtube_collections (
 id INTEGER PRIMARY KEY,
 title TEXT NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
 created_at INTEGER NOT NULL
);
CREATE TABLE youtube_reviews (
 id TEXT PRIMARY KEY,
 video_id TEXT NOT NULL,
 video_title TEXT NOT NULL,
 channel TEXT NOT NULL,
 target_kind TEXT NOT NULL CHECK(target_kind IN ('custom','movie','episode')),
 collection_id INTEGER REFERENCES youtube_collections(id),
 media_id INTEGER REFERENCES media_items(id),
 episode_id INTEGER REFERENCES episodes(id),
 target_snapshot TEXT NOT NULL,
 expires_at INTEGER NOT NULL,
 consumed_at INTEGER NOT NULL DEFAULT 0,
 CHECK((target_kind='custom' AND collection_id IS NOT NULL AND media_id IS NULL AND episode_id IS NULL) OR
       (target_kind='movie' AND collection_id IS NULL AND media_id IS NOT NULL AND episode_id IS NULL) OR
       (target_kind='episode' AND collection_id IS NULL AND media_id IS NOT NULL AND episode_id IS NOT NULL))
);
CREATE TABLE youtube_jobs (
 id TEXT PRIMARY KEY,
 review_id TEXT NOT NULL UNIQUE REFERENCES youtube_reviews(id),
 video_id TEXT NOT NULL,
 video_title TEXT NOT NULL,
 channel TEXT NOT NULL,
 target_kind TEXT NOT NULL CHECK(target_kind IN ('custom','movie','episode')),
 collection_id INTEGER REFERENCES youtube_collections(id),
 media_id INTEGER REFERENCES media_items(id),
 episode_id INTEGER REFERENCES episodes(id),
 target_snapshot TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('queued','downloading','awaiting_review','importing','completed','failed','cancelled')),
 message TEXT NOT NULL DEFAULT '',
 source_path TEXT NOT NULL DEFAULT '',
 lease_owner TEXT NOT NULL DEFAULT '',
 lease_until INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL,
 CHECK((target_kind='custom' AND collection_id IS NOT NULL AND media_id IS NULL AND episode_id IS NULL) OR
       (target_kind='movie' AND collection_id IS NULL AND media_id IS NOT NULL AND episode_id IS NULL) OR
       (target_kind='episode' AND collection_id IS NULL AND media_id IS NOT NULL AND episode_id IS NOT NULL))
);
CREATE UNIQUE INDEX youtube_active_movie ON youtube_jobs(media_id) WHERE target_kind='movie' AND state NOT IN ('completed','cancelled','failed');
CREATE UNIQUE INDEX youtube_active_episode ON youtube_jobs(episode_id) WHERE target_kind='episode' AND state NOT IN ('completed','cancelled','failed');
CREATE UNIQUE INDEX youtube_active_custom_video ON youtube_jobs(collection_id,video_id) WHERE target_kind='custom' AND state NOT IN ('cancelled','failed');
CREATE TABLE youtube_imports (
 job_id TEXT PRIMARY KEY REFERENCES youtube_jobs(id),
 source_path TEXT NOT NULL,
 destination_path TEXT NOT NULL,
 stage_name TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes > 0),
 source_mtime INTEGER NOT NULL,
 sha256 TEXT NOT NULL,
 probe_json TEXT NOT NULL,
 phase TEXT NOT NULL CHECK(phase IN ('planned','staged','published','committed')),
 updated_at INTEGER NOT NULL
);
