# Television catalog and monitoring

Use Search → TV shows with the same TMDb connection already used for movies.
Details and adds use canonical provider metadata; the browser supplies only a
TMDb ID and optional monitoring preference. Duplicate adds return the saved show
without contacting TMDb or changing selections. Movie and TV IDs are independent.

Open Seasons & episodes from the TV library. Adding a show saves its season
summaries in one transaction. View episodes reads only local data; Load episodes
from TMDb explicitly fetches one season. This avoids fetching an entire long-running
show during an add. Each season supports up to 2,000 episodes within the existing
2 MiB provider response limit. Unsupported/invalid responses leave saved data intact.

Regular seasons and newly loaded episodes start selected. Specials (season 0)
start unselected. The show switch is a master pause; season switches pause that
season. Neither parent switch overwrites individual episode selections. Effective
monitoring requires the show, season and episode selections together. These are
preferences for future automation; no TV download starts from monitoring changes.

Refresh show details updates show metadata and season summaries. Refresh episodes
updates that season and adds new episodes. Both retain existing selections and
previously known rows missing from a later provider response; automatic removal or
renumbering would need a separate reconciliation policy. Every mutation includes
the current show revision. Concurrent or stale edits fail with a conflict instead
of silently overwriting a newer choice. Reload saved show retrieves the latest
revision without calling TMDb. Failed refreshes preserve existing local data.

Saved metadata and choices remain usable after restart and without a TMDb
connection. Dates are provider calendar dates, not a broadcast-time schedule.
Missing episode dates are displayed as unknown. Catalog adds do not inspect media
folders, claim that a file exists, or modify qBittorrent. Explicitly selected TV
releases use qBittorrent and require a file/episode review in Activity before
import. Imported episode availability comes from registered files, with season
counts and relative paths. This is not an existing-library scan. See
[TV downloads and imports](TV_DOWNLOADS.md) for the complete workflow and limits.

Migration 004 adds series_details, seasons and episodes without changing earlier
migrations, accounts, sessions, encrypted integrations or movie download jobs.
Existing series rows receive a details record; refresh their show details to load
season summaries. Back up the stopped config directory before upgrading.
Migration 005 adds TV downloads, episode/file associations and per-file import
journals. Metadata refresh now refuses changed season/episode provider identities
at existing numbers, preserving existing file and download associations for review.

Contracts: official TMDb [TV search](https://developer.themoviedb.org/reference/search-tv),
[series details](https://developer.themoviedb.org/reference/tv-series-details) and
[season details](https://developer.themoviedb.org/reference/tv-season-details).
See API.md for paths/bodies and VALIDATION.md for executed checks.
