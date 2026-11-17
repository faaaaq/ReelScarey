-- Owner-corrected version labels for registered movie files. Inspection
-- derives a resolution label only ("4K (2160p)"); which edition or cut a file
-- actually is (for example the 2019 version of a movie registered under the
-- 1994 catalog entry) can never be verified from the stream itself, so the
-- owner may set the label explicitly. The empty string keeps the derived
-- resolution label.
ALTER TABLE media_files ADD COLUMN version_label TEXT NOT NULL DEFAULT '';
