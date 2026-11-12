-- Per-user Subsonic accounts. Before this migration the music server had a
-- single shared token and accepted any username, so every client signed in as
-- the same invisible account with the same rights. Households and shared
-- servers need named accounts with independent credentials and independent
-- permissions, so each account now carries its own username, password and role
-- set.
--
-- Passwords are stored encrypted rather than hashed. Subsonic's token scheme
-- (t=md5(password+salt)) requires the server to reproduce md5(password+salt),
-- which a one-way hash cannot do; the ciphertext is protected by the same
-- AES-GCM secrets.key that guards every other credential. Any Subsonic client
-- that only sends p= still verifies against the decrypted value in constant
-- time.
--
-- password_generated marks a password this server created from the Crockford
-- alphabet. Generated passwords keep the forgiving comparison the shared token
-- used (case-insensitive, separators ignored) so a hand-typed phone password
-- works; a password a person chose is compared exactly.
CREATE TABLE music_server_users (
    id INTEGER PRIMARY KEY,
    username TEXT NOT NULL COLLATE NOCASE UNIQUE CHECK(length(username) BETWEEN 1 AND 64),
    email TEXT NOT NULL DEFAULT '' CHECK(length(email) <= 254),
    password_sealed BLOB NOT NULL,
    password_generated INTEGER NOT NULL DEFAULT 0 CHECK(password_generated IN (0,1)),
    scrobbling_enabled INTEGER NOT NULL DEFAULT 1 CHECK(scrobbling_enabled IN (0,1)),
    admin_role INTEGER NOT NULL DEFAULT 0 CHECK(admin_role IN (0,1)),
    settings_role INTEGER NOT NULL DEFAULT 0 CHECK(settings_role IN (0,1)),
    download_role INTEGER NOT NULL DEFAULT 1 CHECK(download_role IN (0,1)),
    upload_role INTEGER NOT NULL DEFAULT 0 CHECK(upload_role IN (0,1)),
    playlist_role INTEGER NOT NULL DEFAULT 1 CHECK(playlist_role IN (0,1)),
    cover_art_role INTEGER NOT NULL DEFAULT 1 CHECK(cover_art_role IN (0,1)),
    share_role INTEGER NOT NULL DEFAULT 0 CHECK(share_role IN (0,1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL DEFAULT 0
);
