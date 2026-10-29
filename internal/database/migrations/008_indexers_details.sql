CREATE TABLE indexers (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 80),
 kind TEXT NOT NULL CHECK(kind IN ('torznab','iptorrents','xspeeds','nyaa','rss')),
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1,
 generation TEXT NOT NULL,
 ciphertext BLOB NOT NULL,
 updated_at INTEGER NOT NULL,
 last_test_at INTEGER NOT NULL DEFAULT 0,
 last_search_at INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '',
 last_result_count INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE media_enrichment (
 media_id INTEGER PRIMARY KEY REFERENCES media_items(id),
 payload TEXT NOT NULL CHECK(length(payload)<=262144),
 updated_at INTEGER NOT NULL
);
ALTER TABLE downloads ADD COLUMN indexer_name TEXT NOT NULL DEFAULT '';
