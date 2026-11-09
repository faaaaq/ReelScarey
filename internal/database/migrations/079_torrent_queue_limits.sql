-- Persist built-in qBittorrent queueing limits across supervised restarts.
-- Values mirror qBittorrent's Queueing preferences. The generated
-- qBittorrent.conf is rewritten at every start, so these live with the rest of
-- ReelScarey's client preferences instead of in that generated file.

ALTER TABLE download_client_preferences ADD COLUMN builtin_queueing_enabled INTEGER NOT NULL DEFAULT 1 CHECK(builtin_queueing_enabled IN (0,1));
ALTER TABLE download_client_preferences ADD COLUMN builtin_max_active_downloads INTEGER NOT NULL DEFAULT 3 CHECK(builtin_max_active_downloads BETWEEN 1 AND 100);
ALTER TABLE download_client_preferences ADD COLUMN builtin_max_active_uploads INTEGER NOT NULL DEFAULT 3 CHECK(builtin_max_active_uploads BETWEEN 1 AND 100);
ALTER TABLE download_client_preferences ADD COLUMN builtin_max_active_torrents INTEGER NOT NULL DEFAULT 5 CHECK(builtin_max_active_torrents BETWEEN 1 AND 100);
