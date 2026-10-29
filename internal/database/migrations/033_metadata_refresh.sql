CREATE TABLE metadata_refresh_schedule (
 id INTEGER PRIMARY KEY CHECK(id=1), enabled INTEGER NOT NULL DEFAULT 0,
 interval_hours INTEGER NOT NULL DEFAULT 24 CHECK(interval_hours BETWEEN 1 AND 720),
 batch_size INTEGER NOT NULL DEFAULT 5 CHECK(batch_size BETWEEN 1 AND 25),
 only_monitored INTEGER NOT NULL DEFAULT 1, revision INTEGER NOT NULL DEFAULT 1,
 requested INTEGER NOT NULL DEFAULT 0, next_run INTEGER NOT NULL DEFAULT 0,
 cursor INTEGER NOT NULL DEFAULT 0, lease_owner TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0,
 state TEXT NOT NULL DEFAULT 'paused', message TEXT NOT NULL DEFAULT 'Metadata refresh is paused.',
 last_run INTEGER NOT NULL DEFAULT 0
);
INSERT INTO metadata_refresh_schedule(id) VALUES(1);
CREATE TABLE metadata_refresh_state (
 media_id INTEGER PRIMARY KEY REFERENCES media_items(id) ON DELETE CASCADE,
 last_success INTEGER NOT NULL DEFAULT 0, season_cursor INTEGER NOT NULL DEFAULT -1,
 state TEXT NOT NULL DEFAULT '', message TEXT NOT NULL DEFAULT ''
);
