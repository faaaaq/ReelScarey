CREATE TABLE music_acquisition_attempts (
    id INTEGER PRIMARY KEY,
    track_id INTEGER NOT NULL REFERENCES music_acquisition_tracks(id) ON DELETE CASCADE,
    attempted_at INTEGER NOT NULL,
    source_id TEXT NOT NULL DEFAULT '',
    source_url TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL DEFAULT 'selected',
    evidence_json TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX music_acquisition_attempt_history ON music_acquisition_attempts(track_id,id);
