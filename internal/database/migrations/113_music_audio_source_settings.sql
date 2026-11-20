-- Persist the requested acquisition source independently of catalog source_kind.
-- Empty means the automatic provider chain; existing jobs keep that behavior.
ALTER TABLE music_acquisition_jobs
    ADD COLUMN audio_source TEXT NOT NULL DEFAULT ''
    CHECK(audio_source IN ('','tidal','youtube'));

-- Empty quality inherits the server environment. Empty source uses automatic
-- TIDAL-first/fallback routing when TIDAL is installed, otherwise YouTube.
ALTER TABLE music_preferences
    ADD COLUMN download_source TEXT NOT NULL DEFAULT ''
    CHECK(download_source IN ('','tidal','youtube'));
ALTER TABLE music_preferences
    ADD COLUMN tidal_quality TEXT NOT NULL DEFAULT ''
    CHECK(tidal_quality IN ('','LOW','HIGH','LOSSLESS','HI_RES_LOSSLESS'));
