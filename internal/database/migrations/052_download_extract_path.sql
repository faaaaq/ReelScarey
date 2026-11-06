-- Relative extraction folder for a completed archive-only download. Set only
-- after a verified extraction; cleared after the extracted copies are removed.
ALTER TABLE downloads ADD COLUMN extract_path TEXT NOT NULL DEFAULT '';
