-- Automatic embedded-title updates run from a durable queue. This single-row
-- state lets the owner pause the queue (and resume it) without discarding the
-- pending work, and records when the choice last changed.
CREATE TABLE title_queue_state (
	id INTEGER PRIMARY KEY CHECK (id = 1),
	paused INTEGER NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL DEFAULT 0
);
INSERT INTO title_queue_state(id,paused,updated_at) VALUES(1,0,unixepoch());
