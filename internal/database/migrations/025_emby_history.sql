CREATE TABLE emby_monitor (
 id INTEGER PRIMARY KEY CHECK(id=1),
 enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
 revision INTEGER NOT NULL DEFAULT 1,
 last_success INTEGER NOT NULL DEFAULT 0,
 status TEXT NOT NULL DEFAULT 'paused'
 ,config_fingerprint TEXT NOT NULL DEFAULT ''
);
INSERT INTO emby_monitor(id) VALUES(1);
CREATE TABLE emby_play_history (
 id INTEGER PRIMARY KEY,
 session_id TEXT NOT NULL,
 item_id TEXT NOT NULL,
 username TEXT NOT NULL,
 title TEXT NOT NULL,
 series TEXT NOT NULL,
 kind TEXT NOT NULL,
 started_at INTEGER NOT NULL,
 last_seen INTEGER NOT NULL,
 ended_at INTEGER,
 state TEXT NOT NULL CHECK(state IN ('active','ended','uncertain')),
 paused INTEGER NOT NULL CHECK(paused IN (0,1)),
 position_ticks INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX emby_one_active_session ON emby_play_history(session_id) WHERE state='active';
CREATE INDEX emby_history_time ON emby_play_history(started_at DESC,id DESC);
