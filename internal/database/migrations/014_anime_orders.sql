CREATE TABLE anime_orders (
 media_id INTEGER PRIMARY KEY REFERENCES media_items(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL CHECK(revision>0)
);
CREATE TABLE anime_provider_links (
 media_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
 provider_id INTEGER NOT NULL CHECK(provider_id BETWEEN 1 AND 2147483647),
 identity_json TEXT NOT NULL,
 PRIMARY KEY(media_id,provider_id)
);
CREATE TABLE anime_order_entries (
 media_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
 episode_id INTEGER NOT NULL UNIQUE REFERENCES episodes(id) ON DELETE CASCADE,
 provider_id INTEGER NOT NULL DEFAULT 0,
 release_title TEXT NOT NULL,
 label TEXT NOT NULL,
 PRIMARY KEY(media_id,episode_id)
);
CREATE TABLE anime_order_reviews (
 id TEXT PRIMARY KEY,
 media_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
 payload TEXT NOT NULL,
 expires_at INTEGER NOT NULL,
 applied INTEGER NOT NULL DEFAULT 0 CHECK(applied IN (0,1))
);
CREATE INDEX anime_order_reviews_expiry ON anime_order_reviews(expires_at);
ALTER TABLE downloads ADD COLUMN anime_order_snapshot TEXT NOT NULL DEFAULT '';
