ALTER TABLE settings ADD COLUMN import_mode TEXT NOT NULL DEFAULT 'hardlink'
    CHECK (import_mode IN ('hardlink', 'copy'));
