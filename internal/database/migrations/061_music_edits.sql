ALTER TABLE music_files ADD COLUMN root TEXT NOT NULL DEFAULT '';

CREATE TABLE music_edit_jobs (
    id INTEGER PRIMARY KEY,
    state TEXT NOT NULL CHECK(state IN ('running','completed','failed','rolled_back')),
    file_count INTEGER NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE music_edit_files (
    id INTEGER PRIMARY KEY,
    job_id INTEGER NOT NULL REFERENCES music_edit_jobs(id) ON DELETE CASCADE,
    music_file_id INTEGER NOT NULL REFERENCES music_files(id) ON DELETE CASCADE,
    source_path TEXT NOT NULL,
    destination_path TEXT NOT NULL,
    stage_path TEXT NOT NULL DEFAULT '',
    backup_path TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL CHECK(action IN ('move','retag','move_retag')),
    tags_json TEXT NOT NULL DEFAULT '{}',
    previous_tag_json TEXT NOT NULL DEFAULT '{}',
    state TEXT NOT NULL CHECK(state IN ('pending','completed','failed','rolled_back')),
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX music_edit_files_job ON music_edit_files(job_id);
