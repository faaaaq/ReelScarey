# Library browsing and interface (0.13 development)

Movies and TV shows support saved-title search, status and monitoring filters,
sorting, and poster/table views. These operations read the saved catalog; they do
not scan files, call providers, change monitoring or submit downloads. Existing
automation continues independently.

## Browsing

- Search matches a literal substring of the saved title. `%`, `_` and backslash
  are ordinary characters. Matching ignores ASCII case; it is not fuzzy search
  or a TMDb query. Queries allow 200 UTF-8 bytes and no control characters.
- Status uses the saved parent catalog status: missing, available or downloading.
  For shows, available is labeled **Episode files registered**, which does not
  assert that every episode is available. Use the episode list or Wanted for
  individual episode facts. Registered status is not a disk check.
- Monitoring filters the parent movie/show preference. It does not recalculate
  child season/episode selections; Wanted provides effective episode monitoring.
- Sort by title A–Z/Z–A, year newest/oldest, or recently added. Unknown years sort
  last in either direction. Recently added uses descending catalog insertion ID,
  not release date. Equal titles/years use stable title/ID tie breakers.
- Pages contain up to 48 titles. Choose **Show all** to display all matching titles.
  Bulk selection is available in the paginated view. The heading shows the unfiltered collection
  count; the result bar shows the filtered total and displayed range. Empty
  filtered or later pages offer explicit recovery without silently changing filters.
  Reviewed parent monitoring accepts up to 5000 explicit distinct catalog IDs in
  one journaled selection, so a whole bounded library view can be paused or
  resumed. The review still checks the complete selection before writing, and a
  changed title refuses the whole batch.

Filters, ordering, view and page live in the URL. Browser back/forward and shared
bookmarks retain them. Filter/sort changes return to page one in paginated mode. Show all remains selected. Switching between
posters and the table preserves the page and does not fetch the catalog again.
These are per-URL choices, not a new account preference. Existing search/detail,
episode, release and Activity links continue to use their normal workflows.

GET `/api/v1/library` accepts kind=movie/series/anime and page=1–100000 or page=all. Optional
parameters are query, status=all/missing/available/downloading,
monitored=all/monitored/paused and sort=title/title_desc/year/year_desc/added.
Defaults are no query, all statuses/monitoring and title ascending. Responses add
collection_total alongside items/page/limit/total. View is a frontend URL option
and is not sent to the API. Unknown/repeated/malformed API parameters return 400.
Counts and items share a transaction, with a five-second HTTP read budget.

## Interface

The shared interface uses a compact catalog layout: charcoal surfaces, restrained
blue accents, square edges, aligned rules, larger body text and artwork as the
main visual content. Shared slogan bars, decorative page labels, welcome art,
promotional cards and tagline footers are removed, including from sign-in and
setup. Functional descriptions, safety explanations and TMDb credit remain.
External providers' movie taglines are metadata, not application slogans.

The overview links actual movie/show totals, monitored missing items, tracked
work counts, saved upcoming dates for the current UTC month and configured storage
roots. Each preview shows at most four entries with links to the full views;
errors remain visible per section. Calendar retains its saved-metadata limits.

Each Movies, TV Shows and Anime toolbar includes **Refresh**, which queues a
metadata pass for every title in that collection. The durable worker runs it even
when the scheduled refresh is paused. Added, registered and imported titles queue
automatically; the button is for existing catalogs and manual rechecks.

Native table semantics, labeled controls, keyboard focus, reduced-motion support
and phone navigation remain. Narrow tables scroll inside a labeled, focusable
region. No external fonts, application dependencies or migrations are added.
Schema remains 10. This milestone does not add bulk edits, removal, disk health
scans, replacement imports or private-account acceptance. Version 0.14 adds
[reviewed parent monitoring](BULK_MONITORING.md) and replaces olive surfaces with
neutral graphite/blue while retaining this layout. IPTorrents and XSpeeds
account testing remains deferred.
