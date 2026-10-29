CREATE TABLE subtitle_edits (
 id TEXT PRIMARY KEY, media_file_id INTEGER NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
 language TEXT NOT NULL, variant TEXT NOT NULL, action TEXT NOT NULL,
 snapshot TEXT NOT NULL, content BLOB, old_sha TEXT NOT NULL, new_sha TEXT NOT NULL,
 size_bytes INTEGER NOT NULL, cues INTEGER NOT NULL, offset_ms INTEGER NOT NULL DEFAULT 0,
 destination TEXT NOT NULL, stage_path TEXT NOT NULL, previous_path TEXT NOT NULL,
 state TEXT NOT NULL DEFAULT 'review', phase TEXT NOT NULL DEFAULT 'planned',
 message TEXT NOT NULL DEFAULT '', expires_at INTEGER NOT NULL, created_at INTEGER NOT NULL,
 lease_owner TEXT NOT NULL DEFAULT '', lease_until INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX subtitle_edit_claim ON subtitle_edits(state,lease_until);
