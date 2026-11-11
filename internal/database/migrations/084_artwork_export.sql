-- Optional movie poster sidecars. The worker only writes a missing poster.jpg
-- beside a registered movie; it never replaces an existing image.
ALTER TABLE settings ADD COLUMN artwork_export INTEGER NOT NULL DEFAULT 0 CHECK(artwork_export IN (0,1));
