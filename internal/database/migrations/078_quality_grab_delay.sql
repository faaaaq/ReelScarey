-- Optional per-profile grab delay. Zero keeps the historical immediate
-- behaviour; callers own the first-sighting timestamp and their own fencing.

ALTER TABLE quality_profiles ADD COLUMN grab_delay_minutes INTEGER NOT NULL DEFAULT 0 CHECK(grab_delay_minutes BETWEEN 0 AND 1440);
