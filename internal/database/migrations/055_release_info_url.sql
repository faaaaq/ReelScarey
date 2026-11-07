-- The provider's details page for a release selection. Empty means the source
-- did not supply one; the application never constructs or fetches this URL.
ALTER TABLE release_candidates ADD COLUMN info_url TEXT NOT NULL DEFAULT '';
