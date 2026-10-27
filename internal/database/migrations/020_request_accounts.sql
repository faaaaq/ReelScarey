CREATE TABLE request_accounts (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash TEXT NOT NULL,
    disabled INTEGER NOT NULL DEFAULT 0 CHECK(disabled IN (0,1)),
    created_at INTEGER NOT NULL
);
CREATE TABLE request_sessions (
    token_hash TEXT PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES request_accounts(id) ON DELETE CASCADE,
    expires_at INTEGER NOT NULL,
    created_at INTEGER NOT NULL
);
CREATE INDEX request_sessions_account ON request_sessions(account_id,expires_at);
CREATE TABLE media_requests (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES request_accounts(id),
    kind TEXT NOT NULL CHECK(kind IN ('movie','series')),
    tmdb_id INTEGER NOT NULL CHECK(tmdb_id>0),
    title TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','approving','approved','declined')),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(account_id,kind,tmdb_id)
);
CREATE INDEX media_requests_state ON media_requests(state,created_at,id);
CREATE TABLE request_webhooks (
    account_id INTEGER PRIMARY KEY REFERENCES request_accounts(id) ON DELETE CASCADE,
    ciphertext BLOB NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE request_notifications (
    request_id INTEGER PRIMARY KEY REFERENCES media_requests(id),
    state TEXT NOT NULL CHECK(state IN ('pending','sending','sent','uncertain')),
    next_attempt INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);
CREATE TABLE request_lists (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES request_accounts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK(provider IN ('tmdb','trakt')),
    owner TEXT NOT NULL DEFAULT '',
    list_id TEXT NOT NULL,
    ciphertext BLOB,
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    revision INTEGER NOT NULL DEFAULT 1,
    last_error TEXT NOT NULL DEFAULT '',
    last_checked INTEGER NOT NULL DEFAULT 0,
    next_check INTEGER NOT NULL DEFAULT 0,
    lease_until INTEGER NOT NULL DEFAULT 0,
    UNIQUE(account_id,provider,owner,list_id)
);
