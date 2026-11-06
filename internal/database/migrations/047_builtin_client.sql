-- The app-supervised built-in qBittorrent connection is a first-class named
-- client: the same paused start, per-protocol defaults and job binding rules
-- apply. The flag marks the row so generic client editing cannot fight the
-- application's process supervisor.
ALTER TABLE download_clients ADD COLUMN builtin INTEGER NOT NULL DEFAULT 0 CHECK(builtin IN (0,1));
