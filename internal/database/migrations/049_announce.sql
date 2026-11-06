-- IRC announce sources and captured events. Source settings (including the
-- IRC password) live in the encrypted integration store; this table holds only
-- non-secret metadata. Events store parsed fields and a dedupe hash, never the
-- raw IRC line, so tracker links or credentials cannot leak from the list.
CREATE TABLE announce_sources (
 id TEXT PRIMARY KEY,
 tracker TEXT NOT NULL,
 name TEXT NOT NULL,
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1,
 settings BLOB NOT NULL,
 last_error TEXT NOT NULL DEFAULT '',
 last_event_at INTEGER NOT NULL DEFAULT 0,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE TABLE announce_events (
 id INTEGER PRIMARY KEY,
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
 UNIQUE(source_id, dedupe)
);
CREATE INDEX announce_events_time ON announce_events(received_at DESC, id DESC);
CREATE INDEX announce_sources_enabled ON announce_sources(enabled);
