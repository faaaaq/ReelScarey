-- Track the HDR/SDR transfer characteristics of registered videos. The column
-- is generated from the stored probe, so re-inspection updates it without a
-- rewrite. Files probed before HDR detection have no color_transfer record:
-- their value is NULL (unknown), not SDR, and the naming engine omits the tag.
ALTER TABLE media_files ADD COLUMN video_hdr INTEGER GENERATED ALWAYS AS (
  CASE WHEN json_valid(probe_json) THEN json_extract(probe_json,'$.hdr') END
) VIRTUAL;
