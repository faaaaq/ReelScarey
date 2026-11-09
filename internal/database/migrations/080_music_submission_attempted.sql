-- Track whether a music submission was actually attempted. A definitive client
-- rejection may be resubmitted after the owner fixes the client; an uncertain
-- submission stays reconcile-only. Existing rows predate the column and are
-- treated as already attempted so a legacy uncertain job is never resubmitted
-- automatically after upgrade.
ALTER TABLE music_downloads ADD COLUMN submission_attempted INTEGER NOT NULL DEFAULT 1 CHECK(submission_attempted IN (0,1));
