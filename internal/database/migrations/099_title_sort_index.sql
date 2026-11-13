-- Library browsing sorts by a de-leading-title expression that no index could
-- serve (RS-0001): every page view materialized the whole catalog into a temp
-- B-tree sort. The same expression becomes a virtual generated column, kept
-- identical to the in-query CASE, with a kind-prefixed index so the default
-- title sort is served by an index range (SQLite materializes the expression
-- into the index; the table itself stores nothing extra). SQLite cannot add a
-- STORED generated column with ALTER TABLE, hence the virtual form. Title
-- updates are reflected immediately in both the column and the index.
ALTER TABLE media_items ADD COLUMN title_sort TEXT GENERATED ALWAYS AS (
  CASE WHEN lower(title) LIKE 'the %' THEN substr(title,5) ELSE title END
) VIRTUAL;
CREATE INDEX media_items_title_sort ON media_items(kind, title_sort COLLATE NOCASE, id);
