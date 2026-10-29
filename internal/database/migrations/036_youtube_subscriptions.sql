CREATE TABLE youtube_subscriptions (
 id TEXT PRIMARY KEY, source_url TEXT NOT NULL, title TEXT NOT NULL,
 collection_id INTEGER NOT NULL REFERENCES youtube_collections(id),
 enabled INTEGER NOT NULL DEFAULT 0, mode TEXT NOT NULL DEFAULT 'review' CHECK(mode IN ('review','acquire')),
 interval_hours INTEGER NOT NULL DEFAULT 24 CHECK(interval_hours BETWEEN 1 AND 168),
 subtitles_json TEXT NOT NULL DEFAULT '{"languages":[],"automatic":false}',
 target_snapshot TEXT NOT NULL, initialized INTEGER NOT NULL DEFAULT 0,
 revision INTEGER NOT NULL DEFAULT 1, requested INTEGER NOT NULL DEFAULT 0,
 next_run INTEGER NOT NULL DEFAULT 0, last_checked INTEGER NOT NULL DEFAULT 0,
 lease_owner TEXT NOT NULL DEFAULT '',lease_until INTEGER NOT NULL DEFAULT 0,
 state TEXT NOT NULL DEFAULT 'paused',message TEXT NOT NULL DEFAULT 'Subscription is paused.',
 created_at INTEGER NOT NULL, UNIQUE(source_url,collection_id)
);
CREATE TABLE youtube_subscription_entries (
 subscription_id TEXT NOT NULL REFERENCES youtube_subscriptions(id) ON DELETE CASCADE,
 video_id TEXT NOT NULL, title TEXT NOT NULL, channel TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'new', review_id TEXT REFERENCES youtube_reviews(id),
 job_id TEXT REFERENCES youtube_jobs(id), discovered_at INTEGER NOT NULL,
 PRIMARY KEY(subscription_id,video_id)
);
