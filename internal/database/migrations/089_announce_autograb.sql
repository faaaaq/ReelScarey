-- Durable, explicitly opted-in IPTorrents announce automation. Existing
-- sources and captured history remain capture-only until the owner enables
-- automatic grabbing. Rebuild the event table with AUTOINCREMENT so pruning a
-- terminal row can never recycle an event ID used by a durable release/candidate.
ALTER TABLE announce_sources ADD COLUMN auto_grab INTEGER NOT NULL DEFAULT 0 CHECK(auto_grab IN (0,1));
PRAGMA defer_foreign_keys=ON;
CREATE TABLE announce_events_next (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 source_id TEXT NOT NULL REFERENCES announce_sources(id) ON DELETE CASCADE,
 tracker TEXT NOT NULL,
 release_name TEXT NOT NULL,
 category TEXT NOT NULL DEFAULT '',
 size_bytes INTEGER NOT NULL DEFAULT 0,
 freeleech TEXT NOT NULL DEFAULT '',
 torrent_id TEXT NOT NULL DEFAULT '',
 info_url TEXT NOT NULL DEFAULT '',
 dedupe TEXT NOT NULL,
 received_at INTEGER NOT NULL,
 source_revision INTEGER NOT NULL DEFAULT 0,
 processing_state TEXT NOT NULL DEFAULT 'done' CHECK(processing_state IN ('pending','processing','retry','done')),
 outcome TEXT NOT NULL DEFAULT 'capture_only',
 message TEXT NOT NULL DEFAULT '',
 attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0),
 next_run INTEGER,
 lease_owner TEXT,
 lease_until INTEGER,
 processed_at INTEGER,
 download_id TEXT,
 UNIQUE(source_id,dedupe)
);
INSERT INTO announce_events_next(
 id,source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,
 info_url,dedupe,received_at,source_revision,processing_state,outcome,message,
 attempts,next_run,lease_owner,lease_until,processed_at,download_id
)
SELECT id,source_id,tracker,release_name,category,size_bytes,freeleech,torrent_id,
 info_url,dedupe,received_at,0,'done','capture_only','',0,NULL,NULL,NULL,received_at,NULL
FROM announce_events;
DROP TABLE announce_events;
ALTER TABLE announce_events_next RENAME TO announce_events;
CREATE INDEX announce_events_time ON announce_events(received_at DESC,id DESC);
CREATE INDEX announce_events_claim ON announce_events(processing_state,next_run,received_at,id);
CREATE INDEX announce_events_lease ON announce_events(processing_state,lease_until);
