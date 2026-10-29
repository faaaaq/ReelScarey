ALTER TABLE request_accounts ADD COLUMN discord_user_id TEXT;
CREATE UNIQUE INDEX request_accounts_discord_user ON request_accounts(discord_user_id) WHERE discord_user_id IS NOT NULL;
CREATE TABLE discord_bot_config (
 id INTEGER PRIMARY KEY CHECK(id=1),
 application_id TEXT NOT NULL,
 public_key TEXT NOT NULL,
 guild_id TEXT NOT NULL,
 admin_user_id TEXT NOT NULL,
 admin_channel_id TEXT NOT NULL,
 ciphertext BLOB NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE TABLE discord_bot_jobs (
 id TEXT PRIMARY KEY,
 account_id INTEGER REFERENCES request_accounts(id),
 actor_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 payload TEXT NOT NULL,
 ciphertext BLOB NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('queued','running','sent','uncertain')),
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE INDEX discord_bot_jobs_actor_time ON discord_bot_jobs(actor_id,created_at);
CREATE TABLE discord_bot_choices (
 id TEXT PRIMARY KEY,
 actor_id TEXT NOT NULL,
 account_id INTEGER REFERENCES request_accounts(id),
 kind TEXT NOT NULL,
 payload TEXT NOT NULL,
 expires_at INTEGER NOT NULL,
 consumed_at INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE discord_bot_notices (
 request_id INTEGER PRIMARY KEY REFERENCES media_requests(id),
 state TEXT NOT NULL CHECK(state IN ('queued','sending','sent','uncertain')),
 updated_at INTEGER NOT NULL
);
