-- TMDb release_dates supplies separate theatrical, digital and physical dates.
-- release_date remains the canonical primary date for existing consumers.
ALTER TABLE media_items ADD COLUMN theatrical_release_date TEXT NOT NULL DEFAULT '';
ALTER TABLE media_items ADD COLUMN digital_release_date TEXT NOT NULL DEFAULT '';
ALTER TABLE media_items ADD COLUMN physical_release_date TEXT NOT NULL DEFAULT '';
