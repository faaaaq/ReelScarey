# Wanted and release calendar (0.12 development)

Wanted and Calendar are read-only views of the saved catalog. They work without
an indexer, download client or live TMDb connection. Opening or refreshing these
pages does not scan disks, refresh metadata, change monitoring, queue searches or
submit downloads. Existing automation settings continue independently.

## Wanted

Open Wanted from navigation or the dashboard. It defaults to effectively monitored,
released missing items. Movies are single entries; TV entries are loaded episodes,
not whole shows or invented season placeholders. A show, its season and its episode
must all be monitored to appear with Monitored only. Include paused shows their
saved selections and explains which monitoring level pauses them.

Choose a media type, status, monitoring scope or literal title/episode search.
Filter changes reset pagination; URLs retain filters for bookmarks and browser
back/forward. Counts follow title, kind and monitoring filters, independently of
the currently selected status. Each page returns up to 50 entries.

Statuses have this precedence:

1. **In library:** an existing file registration, including shared episode files.
   These entries appear in Calendar; Wanted excludes them.
2. **Needs attention:** a tracked blocked/failed download reserves this target.
3. **In progress:** another active queued/submitting/downloading/importing job.
4. **No date:** no valid full calendar date in saved metadata.
5. **Upcoming:** the saved date is later than the current UTC day.
6. **Missing:** a saved date is today or earlier, without a registration/active job.

Completed/cancelled jobs do not keep a target in progress. Stopped tracking with
no file registration makes the target missing/upcoming/undated again. Available
episode associations count once per episode even when a video covers several.
Availability is a catalog fact, not a fresh filesystem-health check. Missing is
not a promise that a release exists or satisfies an automation profile.

Find releases opens the existing movie or individual-episode search for explicit
review. View activity opens the actual tracked job. View movie/show opens saved
details. These links do not automatically search providers or submit downloads.
Manual release searches can be used for paused/undated/upcoming targets; automatic
searches still require valid dates, monitoring, policies and a connected source.

## Calendar

Calendar is a chronological monthly agenda of saved movie release dates and loaded
episode air dates, including registered items. Previous/next month, the month
picker and Current month retain other filters. Months are validated as YYYY-MM,
years 0001–9999, and default to the server's current UTC month. Pagination remains
explicit so a busy month is not silently truncated. Dates are civil dates displayed
without shifting the date to the browser's time zone.

Movie dates may describe theatrical releases, not streaming/disc availability.
Episode dates do not include broadcast times or establish download availability.
Only cached metadata is used; refresh movie/show/season metadata through its normal
screen when needed. Unloaded-season coverage and untracked missing items with no
valid date are called out. The latter link opens the same scope in Wanted.
Episodes not yet saved cannot be forecast. Partially known seasons and future
metadata changes can also make the agenda incomplete.

Dates must have ten-character YYYY-MM-DD form, years 0001–9999, and normalize to
the same day in SQLite. Impossible dates such as February 30, timestamps and
unknown values are undated. Scheduled searches use this same date validity rule;
invalid dates cannot authorize provider work. Comparing today uses UTC consistently
with automation. There is no scheduling or monitoring mutation in these pages.

## API and limits

GET `/api/v1/wanted` and `/api/v1/calendar` require the administrator session.
Supported parameters: kind (all/movie/series), status, monitored (monitored/all),
query (up to 200 UTF-8 bytes, no controls), page (1–100000), plus month for Calendar.
Unknown or repeated parameters and invalid filters return 400. SQL parameters
escape LIKE wildcards, so `%`, `_` and backslash searches are literal; SQLite's
built-in case-insensitive matching applies to ASCII letters.

Responses include items, filtered status counts, coverage, total/page/limit,
as_of_date, month and effective filters. Items contain catalog IDs, normalized date,
monitoring explanation, registration status and non-secret tracked job ID/state/
protocol. Credentials, client addresses, metainfo, NZBs and filesystem paths are
not exposed. Reads use a transaction for a coherent response and a five-second
request budget. Sorting is date (undated last), title ignoring ASCII case, parent
ID and episode ID. New requests reflect catalog changes; bookmarks are filters,
not frozen snapshots.

No new migration or application dependency is needed; schema remains 10. This
milestone does not add changed/deleted-file checks, automatic metadata refresh,
future episode prediction, timezone-specific broadcasts, upgrade replacement,
bulk actions, media requests or private-account acceptance.
