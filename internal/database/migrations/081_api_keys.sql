-- Scoped API keys for the Sonarr/Radarr/Prowlarr compatibility layer.
-- Only the SHA-256 digest is stored. The token itself is shown once.
CREATE TABLE api_keys (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL CHECK(length(name) BETWEEN 1 AND 80),
    prefix TEXT NOT NULL UNIQUE,
    token_hash TEXT NOT NULL UNIQUE,
    scopes TEXT NOT NULL DEFAULT '[]',
    created_at INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0,
    revoked_at INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX api_keys_active ON api_keys(revoked_at, expires_at);
