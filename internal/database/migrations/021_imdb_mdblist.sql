CREATE TABLE request_lists_next (
    id INTEGER PRIMARY KEY,
    account_id INTEGER NOT NULL REFERENCES request_accounts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK(provider IN ('tmdb','trakt','imdb_mdblist')),
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
INSERT INTO request_lists_next SELECT * FROM request_lists;
DROP TABLE request_lists;
ALTER TABLE request_lists_next RENAME TO request_lists;
