-- Manual playlist matching. Many shows publish playlists whose videos carry no
-- episode number ("X1", "infinity", an intro "XX"), so no automatic mapping can
-- place them. A source with mapping='manual' queues only the videos the owner
-- explicitly binds to a chosen episode of the show.
--
-- SQLite cannot widen a CHECK constraint in place, so the sources table is
-- rebuilt. youtube_series_entries references it with ON DELETE CASCADE and
-- deferred constraints do not defer cascades, so the entries are copied aside
-- and restored after the rename.
PRAGMA defer_foreign_keys=ON;

CREATE TEMP TABLE youtube_series_entries_saved AS SELECT * FROM youtube_series_entries;

CREATE TABLE youtube_series_sources_next (
    id TEXT PRIMARY KEY,
    media_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
    source_url TEXT NOT NULL,
    title TEXT NOT NULL,
    mapping TEXT NOT NULL CHECK(mapping IN ('episode','order','manual')),
    season_number INTEGER NOT NULL DEFAULT 0 CHECK(season_number >= 0),
    start_episode INTEGER NOT NULL DEFAULT 1 CHECK(start_episode > 0),
    quality TEXT NOT NULL DEFAULT 'best',
    subtitles_json TEXT NOT NULL DEFAULT '{"languages":[],"automatic":false,"quality":"best"}',
    auto_import INTEGER NOT NULL DEFAULT 1 CHECK(auto_import IN (0,1)),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    interval_hours INTEGER NOT NULL DEFAULT 6 CHECK(interval_hours BETWEEN 1 AND 168),
    initialized INTEGER NOT NULL DEFAULT 0 CHECK(initialized IN (0,1)),
    revision INTEGER NOT NULL DEFAULT 1 CHECK(revision > 0),
    requested INTEGER NOT NULL DEFAULT 0 CHECK(requested IN (0,1)),
    next_run INTEGER NOT NULL DEFAULT 0,
    last_checked INTEGER NOT NULL DEFAULT 0,
    state TEXT NOT NULL DEFAULT 'idle',
    message TEXT NOT NULL DEFAULT '',
    target_snapshot TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0
);
INSERT INTO youtube_series_sources_next SELECT id,media_id,source_url,title,mapping,season_number,start_episode,quality,subtitles_json,auto_import,enabled,interval_hours,initialized,revision,requested,next_run,last_checked,state,message,target_snapshot,created_at,lease_owner,lease_until FROM youtube_series_sources;
DROP TABLE youtube_series_sources;
ALTER TABLE youtube_series_sources_next RENAME TO youtube_series_sources;
CREATE UNIQUE INDEX youtube_series_source_url ON youtube_series_sources(media_id, source_url);

INSERT INTO youtube_series_entries SELECT * FROM youtube_series_entries_saved;
DROP TABLE youtube_series_entries_saved;

-- Explicit video -> episode bindings for a manual source. The owner picks the
-- episode, so these rows replace the title/position heuristics entirely.
CREATE TABLE youtube_series_matches (
    source_id TEXT NOT NULL REFERENCES youtube_series_sources(id) ON DELETE CASCADE,
    video_id TEXT NOT NULL,
    episode_id INTEGER NOT NULL REFERENCES episodes(id) ON DELETE CASCADE,
    position INTEGER NOT NULL DEFAULT 0,
    title TEXT NOT NULL DEFAULT '',
    channel TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY(source_id, video_id)
);
CREATE INDEX youtube_series_matches_episode ON youtube_series_matches(source_id, episode_id);

PRAGMA defer_foreign_keys=OFF;
