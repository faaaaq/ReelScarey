-- extend preferences (single seeded row id=1, see migration 063 pattern)
ALTER TABLE music_preferences ADD COLUMN download_profile TEXT NOT NULL DEFAULT 'original';
ALTER TABLE music_preferences ADD COLUMN streaming_quality TEXT NOT NULL DEFAULT 'balanced';
ALTER TABLE music_preferences ADD COLUMN original_retention TEXT NOT NULL DEFAULT 'keep';   -- keep|delete|ask
ALTER TABLE music_preferences ADD COLUMN fallback_policy TEXT NOT NULL DEFAULT 'best';      -- best|skip|approve
ALTER TABLE music_preferences ADD COLUMN transcoding_enabled INTEGER NOT NULL DEFAULT 1;
ALTER TABLE music_preferences ADD COLUMN cache_limit_mb INTEGER NOT NULL DEFAULT 512;
ALTER TABLE music_preferences ADD COLUMN max_conversions INTEGER NOT NULL DEFAULT 2;

CREATE TABLE music_quality_profiles (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL,
  key TEXT NOT NULL DEFAULT '',            -- builtin key or '' for custom
  codec TEXT NOT NULL,                     -- original|flac|mp3|aac|opus
  container TEXT NOT NULL DEFAULT '',
  bitrate_kbps INTEGER NOT NULL DEFAULT 0, -- 0 = lossless
  vbr INTEGER NOT NULL DEFAULT 1,
  compression_level INTEGER NOT NULL DEFAULT 0,
  require_lossless INTEGER NOT NULL DEFAULT 0,
  keep_original INTEGER NOT NULL DEFAULT 0,
  custom INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE music_conversion_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  kind TEXT NOT NULL,                      -- download|library|export
  file_id INTEGER NOT NULL DEFAULT 0,      -- catalog music_files id when kind<>download
  profile TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'queued',    -- queued|processing|completed|failed|cancelled
  priority INTEGER NOT NULL DEFAULT 5,
  source_path TEXT NOT NULL DEFAULT '',
  output_path TEXT NOT NULL DEFAULT '',
  stage_path TEXT NOT NULL DEFAULT '',
  progress INTEGER NOT NULL DEFAULT 0,     -- 0..100
  attempts INTEGER NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '',
  work_owner TEXT NOT NULL DEFAULT '',
  work_until INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  completed_at INTEGER
);
CREATE INDEX idx_music_conversion_jobs_state ON music_conversion_jobs(state, priority, id);

CREATE TABLE music_stream_cache (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  file_id INTEGER NOT NULL,
  profile_key TEXT NOT NULL,               -- e.g. "opus-128"
  relative_path TEXT NOT NULL,             -- inside the stream-cache dir
  size_bytes INTEGER NOT NULL,
  source_sha256 TEXT NOT NULL DEFAULT '',
  codec TEXT NOT NULL,
  bitrate_kbps INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  last_used_at INTEGER NOT NULL,
  UNIQUE(file_id, profile_key)
);

ALTER TABLE music_files ADD COLUMN variant_role TEXT NOT NULL DEFAULT 'primary';   -- primary|archive|variant
ALTER TABLE music_files ADD COLUMN variant_profile TEXT NOT NULL DEFAULT '';

ALTER TABLE music_server_users ADD COLUMN stream_quality TEXT NOT NULL DEFAULT '';  -- '' = follow global
ALTER TABLE music_server_users ADD COLUMN download_profile TEXT NOT NULL DEFAULT '';

-- The per-album override row gains a download profile; empty = follow global.
ALTER TABLE music_albums ADD COLUMN download_profile TEXT NOT NULL DEFAULT '';
