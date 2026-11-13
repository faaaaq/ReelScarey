-- Reliability fixes: music edit leases, manual NZB identity dedup, and
-- browse/activity indexes.

-- Music edit jobs claim a 60s lease like other durable workers, so a stale
-- claimant can never finish a job a new worker has taken over.
ALTER TABLE music_edit_jobs ADD COLUMN lease_owner TEXT NOT NULL DEFAULT '';
ALTER TABLE music_edit_jobs ADD COLUMN lease_until INTEGER NOT NULL DEFAULT 0;

-- Manual NZB submissions are identified by their NZB content checksum so a
-- repeated upload cannot silently enqueue the same job twice. Rows are
-- retained briefly as a duplicate fence; the queue itself is the source of
-- truth for delivery.
CREATE TABLE usenet_manual_nzbs (
    nzb_identity TEXT PRIMARY KEY,
    created_at INTEGER NOT NULL
);

-- Activity event dedup/matching reads filter on kind and object identity.
CREATE INDEX activity_events_kind_object ON activity_events(kind, object_id);

-- Catalog browsing filters by kind and sorts case-insensitively by title.
-- (media_items has no category column; kind is its only browse discriminator.)
CREATE INDEX media_items_browse ON media_items(kind, title COLLATE NOCASE);
