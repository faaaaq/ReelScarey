-- Central identities and coarse system grants. Request and Subsonic rows stay
-- as system-local profiles so existing request history, Discord ownership and
-- playback state retain their original numeric IDs. A central account may link
-- one profile of each kind; legacy rows are never merged by username.
CREATE TABLE accounts (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL COLLATE NOCASE,
    -- Argon2id browser credential. Legacy Subsonic-only rows start empty and
    -- need an administrator password reset before they can use the web UI.
    password_hash TEXT NOT NULL DEFAULT '',
    -- Subsonic token authentication needs the original password. New/reset
    -- accounts seal the same password here and in their Subsonic profile.
    password_sealed BLOB,
    disabled INTEGER NOT NULL DEFAULT 0 CHECK(disabled IN (0,1)),
    requests_access INTEGER NOT NULL DEFAULT 0 CHECK(requests_access IN (0,1)),
    music_player_access INTEGER NOT NULL DEFAULT 0 CHECK(music_player_access IN (0,1)),
    subsonic_access INTEGER NOT NULL DEFAULT 0 CHECK(subsonic_access IN (0,1)),
    request_account_id INTEGER UNIQUE REFERENCES request_accounts(id) ON DELETE SET NULL,
    music_server_user_id INTEGER UNIQUE REFERENCES music_server_users(id) ON DELETE SET NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Browser-capable names are unique. The partial index permits a migrated
-- Subsonic-only row to share a legacy name with a separate request identity;
-- those identities are deliberately not merged without an owner decision.
CREATE UNIQUE INDEX accounts_browser_username
    ON accounts(username) WHERE password_hash <> '';
CREATE UNIQUE INDEX accounts_subsonic_username
    ON accounts(username) WHERE subsonic_access = 1;
CREATE INDEX accounts_request_profile ON accounts(request_account_id);
CREATE INDEX accounts_music_profile ON accounts(music_server_user_id);

CREATE TABLE account_sessions (
    token_hash TEXT PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX account_sessions_account ON account_sessions(account_id,expires_at);

INSERT INTO accounts(
    username,password_hash,password_sealed,disabled,requests_access,
    music_player_access,subsonic_access,request_account_id,music_server_user_id,
    created_at,updated_at
)
SELECT username,password_hash,NULL,disabled,1,0,0,id,NULL,created_at,created_at
FROM request_accounts;

INSERT INTO accounts(
    username,password_hash,password_sealed,disabled,requests_access,
    music_player_access,subsonic_access,request_account_id,music_server_user_id,
    created_at,updated_at
)
SELECT username,'',NULL,0,0,0,1,NULL,id,created_at,updated_at
FROM music_server_users;
