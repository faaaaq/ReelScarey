-- Duplicate review dismissals remember groups the owner reviewed and decided
-- to leave alone, so the read-only review stops flagging them on every visit.
-- The key mirrors the review grouping: one movie identity or one episode
-- identity (episode_id is 0 for movie groups).
CREATE TABLE duplicate_dismissals (
	kind TEXT NOT NULL,
	media_id INTEGER NOT NULL,
	episode_id INTEGER NOT NULL DEFAULT 0,
	dismissed_at INTEGER NOT NULL,
	PRIMARY KEY (kind, media_id, episode_id)
);
CREATE INDEX duplicate_dismissals_media ON duplicate_dismissals(media_id);
