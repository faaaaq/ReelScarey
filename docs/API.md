# REST API — media catalog, downloads, imports, automation and operations

## Emby and title helpers

Administrator GET/PUT `/api/v1/library/series/{id}/release-rules` reads or saves
the show's required codec (`""`, `h264`, or `h265`), release group and revision.
Writes reject stale revisions. Rules apply in scheduled searches in addition to
the assigned quality profile and are checked again when reserving a download.
Manual searches are unaffected. Codec labels must be explicit; groups require a
bracketed prefix or hyphenated suffix.

Administrator GET/PUT/DELETE `/api/v1/emby` reads connection status, saves
`{url,api_key}`, or removes the encrypted connection. GET never returns the key;
an empty key on PUT retains it only for the same URL. GET `/api/v1/emby/activity`
fetches a bounded recent-play snapshot from the configured server. It is not a
complete event history; see [Emby activity](EMBY.md).
GET/PUT `/api/v1/emby/monitor` controls opt-in recording with a revision, and
GET `/api/v1/emby/history` reads the most recent 200 observed sessions. Polling
can miss short plays; uncertain gaps are explicitly marked.

Administrator GET `/api/v1/events?before={id}` returns up to 50 recorded state
changes and a cursor for older entries. Recording begins with migration 027;
older actions are not reconstructed. GET/POST/DELETE
`/api/v1/library/items/{id}/related` reads or changes explicit mutual links
between saved titles; writes accept `{other_id}` and require CSRF.

POST `/api/v1/library/items/{id}/create-folder` with `{}` creates the saved
title's configured destination folder without moving or replacing files. GET
`/api/v1/library/items/{id}/collection` fetches the TMDb movie collection and
returns matching local IDs and statuses when present. These endpoints require
administrator authentication, with CSRF and same-origin checks for mutations.

## Request accounts and notifications

Administrator GET/POST `/api/v1/request-accounts` lists or creates accounts;
PUT `/api/v1/request-accounts/{id}` enables/disables one. GET `/api/v1/requests`
lists requests; POST `/api/v1/requests/{id}/approve` or `/decline` reviews a
request. Approval adds the exact TMDb title to the catalog and is retryable.

Only requester sessions can use `/api/v1/requester/requests`, the search paths
`/api/v1/requester/search/movies` and `/series`, or `/api/v1/requester/lists`.
Lists use POST to create, DELETE `/{id}` to remove and POST `/{id}/refresh` to
schedule a check. POST `/api/v1/requester/imdb-csv` accepts a bounded CSV
snapshot. `/api/v1/requester/discord` provides write-only webhook PUT, status
GET and DELETE; POST `/api/v1/requester/requests/{id}/notification/retry`
explicitly retries an uncertain delivery. PUT `/api/v1/requester/password`
changes the requester's password and revokes their sessions. Requesters are denied all other
administrator APIs. Authenticated mutations require CSRF and same origin. See
[request accounts and limits](REQUESTS.md).

## YouTube sourcing

Authenticated POST `/api/v1/youtube/inspect` accepts `{url}` for a bounded
public YouTube video, playlist or channel inspection. GET/POST
`/api/v1/youtube/collections` lists or creates a named custom collection.
POST `/api/v1/youtube/reviews` accepts an explicit `video_id`, target
(`custom`, `movie` or `episode`) and `subtitles` (`languages` and `automatic`);
the server independently inspects the video and returns a short-lived review.
POST `/api/v1/youtube/reviews/{id}/queue` applies that review. GET
`/api/v1/youtube/jobs` and `/api/v1/youtube/files` list job and custom-library
state. GET `/api/v1/youtube/jobs/{id}/import-review` returns the verified media
and subtitle evidence and destinations; POST `.../import` submits that exact
review. POST `.../cancel` cancels jobs before publication. Mutations require
CSRF and same origin. See [YouTube sourcing](YOUTUBE.md) for limits and setup.

## Anime providers and release-order reviews

Authenticated GET `/metadata/anime?query=...` searches public AniList identities.
GET `/library/items/{id}/anime-order` reads saved identities, mappings, canonical
episodes and revisions. POST `.../anime-order/preview` reviews the complete
provider_ids/entries against revision/category_revision/numbering_revision;
PUT `.../anime-order` applies only an opaque server review_id. Preview accepts
strict JSON up to 512 KiB, max20 identities/eight new per review/max5000 mappings.
GET `.../anime-order/sources` and POST `.../anime-order/propose` offer read-only
TMDb group/subgroup proposals; exact payloads are in
[provider order documentation](ANIME_PROVIDER_ORDERS.md).
POST `/releases/anime-pack` explicitly searches 1–100 saved mapped episode IDs
including specials and mixed seasons, against order_revision. Existing download
selection/import review remains required. Paths have the `/api/v1` prefix;
mutations require authentication, CSRF and same origin. Saving an order alone
does not enable automatic matching or modify existing absolute aliases.

## Anime and organization

Library browsing accepts kind=anime (mixed canonical movie/show types); ordinary
movie/series collections exclude explicitly classified Anime. Adds accept optional
category=anime. Item responses add category/category_revision, counts add anime.
GET/PUT `/library/items/{id}/anime` reads/saves explicit section and absolute
aliases against category/show revisions; strict maximum 512 KiB/5000 entries.
GET/PUT `/library/items/{id}/anime-search` reads/saves per-show opt-in search
policy. PUT uses revision/category_revision/numbering_revision, mode
(disabled/season/absolute/mapped), search_title (empty follows canonical) and up to eight
alternate_titles. Mapped mode additionally reviews order_revision and accepts
include_specials, allow_packs and max_pack_episodes (2–100; default 20). GET
returns the current and approved order revisions, these options, mapped_count and
missing_mapping_count. A changed order requires renewed approval. Titles are bounded to 200 UTF-8 bytes; enabled query titles to
180. Strict ordinary 16 KiB JSON, authentication, CSRF and origin checks apply.
Stale revisions return 409; invalid input returns 400. Only Anime shows can save
policies; existing shows remain disabled. Returned loaded/ready/missing counts
cover loaded regular seasons and numbering coverage, not search eligibility.
Downloads may include immutable anime_search evidence. No absolute order is
inferred; completed files still require review. See [Anime searches](ANIME_SEARCH.md).

GET/PUT `/organization` reads/saves revision-checked collection/naming rules
(strict maximum 64 KiB); main_root is server-authoritative. GET/PUT
`/library/items/{id}/organization` reads/saves a compatible future-import
collection, empty library_key restoring section inheritance. GET
`/organization/files` lists registered files without filesystem work. GET/POST
`/organization/plans` lists recent saved reviews/jobs or previews explicit 1–100
file_ids and optional target_library. Alternatively, supply media_id and a required
target_library to review all 1–100 registered files for that title; file_ids and
media_id cannot be combined. Set whole_folder to true to review the entire
title folder and extras. GET `/organization/plans/{id}` reads it;
POST `/organization/plans/{id}/apply` with `{}` queues/resumes that exact review.
All paths above have the `/api/v1` prefix. Preview reads and verifies files but
moves no media; apply runs a durable worker. Mutations require auth/CSRF/origin.
Existing-library discovery GET includes configured collections; POST accepts an
optional library_key. See [exact payloads, worker invariants and limits](ANIME_ORGANIZATION.md).

## Wanted and calendar

Authenticated GET `/api/v1/wanted` and `/api/v1/calendar` accept kind
(all/movie/series), status (all/missing/upcoming/undated/downloading/attention;
Calendar also permits available), monitored (monitored/all), query and page.
Calendar additionally accepts month YYYY-MM; default is the current UTC month.
Wanted defaults to status missing; Calendar to all; monitoring defaults to
effectively monitored. Unknown/repeated keys and invalid values return 400.
Query is literal, up to 200 UTF-8 bytes without controls; page is 1–100000.

Responses contain items, status counts, coverage.unloaded_seasons/undated,
total/page/limit (50), as_of_date and effective month/kind/status/query/
monitored_only. Counts ignore the selected status but follow other filters and
the view's scope (Wanted excludes registrations; Calendar includes dated items
in its month). Undated coverage counts untracked, unavailable undated items that
can be reviewed with Wanted's undated filter. Entries contain kind/media_id,
nullable episode_id/year/season/episode, title/episode_title, normalized date,
poster_url, monitored/monitoring_reason, available, status and non-secret
download_id/download_state/protocol. No source/client secrets or paths appear.
Reads do not initiate searches, scans or jobs. See [planning](PLANNING.md) for
status precedence, cached-date coverage and calendar semantics.

## Usenet integration

`/api/v1/integrations/sabnzbd` uses authenticated GET/PUT/DELETE and the existing
CSRF/same-origin guards. PUT accepts only `{url,api_key?,download_path}`. Omit the
key to retain it; first connect needs the full API key. GET returns configured,
url/download_path, never secrets. POST `/api/v1/integrations/sabnzbd/test` with
`{}` tests the saved connection. Active Usenet jobs prevent disconnecting or
changing their client's address/remote root.

Managed `/indexers` accepts kind `newznab`, API endpoint and write-only api_key,
sharing revision/generation fences and health. Releases include protocol torrent/
usenet; NZB peer/seeder counts are null. Queueing still accepts only opaque
release_id, never caller URLs, paths or NZBs. Jobs and item history expose protocol;
jobs expose non-secret opaque client_id once known. The legacy info_hash field
contains a namespaced NZB descriptor SHA-256 (`nzb:`) for Usenet, not a torrent
hash. Payloads/output snapshots stay private. Retry preserves uncertain Usenet
submission intent. See [Usenet](USENET.md) for mapping, review and recovery limits.

## Existing-library discovery and reviewed registration

All routes require the administrator session; mutations also require CSRF and
same-origin validation. Reads never start a filesystem scan.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/library/imports` | Recent scans and configured movie/TV server roots |
| POST | `/api/v1/library/imports` | `{kind:"movie"|"series"}`; queue bounded discovery; 202 |
| GET | `/api/v1/library/imports/{id}?page=1&state=` | Scan status/counts plus 50 discovered files; optional review/blocked/registered/skipped filter |
| POST | `/api/v1/library/imports/{id}/cancel` | `{}`; stop discovery; prior reviewed registrations remain independent |
| DELETE | `/api/v1/library/imports/{id}` | `{}`; forget finished review records; refuses active discovery/verification; never deletes registered media |
| GET | `/api/v1/library/imports/catalog?kind=movie&query=title` | Up to 20 saved catalog title matches; offline read |
| POST | `/api/v1/library/import-files/{id}/register` | `{media_id,episode_ids:[],reviewed:true}`; queue verified in-place registration; 202 |
| POST | `/api/v1/library/import-files/{id}/skip` | `{}`; skip a review/blocked item |

Paths are immutable server snapshots selected through opaque discovered IDs;
requests never accept an arbitrary root or file path. File responses include
relative/full path, size, mtime snapshot, filename search hint, state/message and
nullable media/file IDs. Stable physical file identities remain internal.
Scans expose kind/folder/state/message, timestamps, discovery/registration/queued
counts and directory-completion examination/skip counts. Limits and exact
semantics are in [existing-library import](EXISTING_LIBRARY.md).

Registration requires explicit review and canonical catalog identities. TV
targets must be loaded, consecutive episodes of one season (maximum 20).
Source identity/size/mtime, checksum, FFprobe, active download reservations,
duplicate paths/targets and current leases are checked before commit. There is
no automatic matching, client submission or filesystem publication. Existing
registered files appear in item details with `origin:"existing"`, an original
filename and empty `download_id`; only downloaded files link to Activity.

## Managed indexers and title details

All routes below require the administrator session; every mutation also requires
CSRF/origin validation. See [provider behavior and limits](INDEXERS_DETAILS.md).

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/indexers` | Safe statuses, including a virtual `legacy` Torznab connection when configured |
| POST | `/api/v1/indexers` | Validate/test/encrypt a new managed source; 201 |
| PUT | `/api/v1/indexers/{id}` | Test and replace settings with matching revision; immutable kind |
| DELETE | `/api/v1/indexers/{id}` | `{revision}`; remove connection and credentials, retain queued jobs |
| PUT | `/api/v1/indexers/{id}/enabled` | `{revision,enabled}`; rotate generation without contacting provider |
| POST | `/api/v1/indexers/{id}/test` | `{}`; explicit connection test, update safe health status |
| GET | `/api/v1/indexers/status` | `configured` means at least one enabled managed or legacy source |
| GET | `/api/v1/library/items/{id}?page=1` | Cached metadata, current roots, verified file facts, unique-file totals and download history |
| POST | `/api/v1/library/items/{id}/metadata` | `{}`; refresh canonical TMDb cast/credits/genres/rating/external links and save locally |

Managed input fields: `name` (1–80), `kind` (`iptorrents`, `xspeeds`, `nyaa`,
`rss`, `torznab`), `url`, `enabled`, `revision`, `user_agent`, `freeleech_only`,
optional write-only `cookie`, `api_key`, `feed_url`. Omitted secrets preserve saved
values; an explicit empty string clears a supported optional secret. Changed
origin/agent requires fresh associated secrets. Maximum twelve managed sources.
Status returns `has_cookie`, `has_api_key`, `has_feed`, safe homepage/endpoint,
revision and test/search timestamps/count/error; no credentials or feed passkeys.
Revision conflicts return 409. The legacy connection edits through its original
integration API; it supports testing/removal from this list.

Movie and TV release responses now add `warnings` for unavailable sources.
Each release may add `indexer_id`, `indexer` and `freeleech`. Selections remain
opaque, encrypted and expiring. Sources are rechecked at queue time. Search errors
from all sources fail the request; partial failures retain healthy results.

Item details returns `item`, nullable `information`, `metadata_updated_at`,
`media_root`, `library_root`, `file_count`, `total_size_bytes`, `history_count`,
`files`, `history`, `page`. Both arrays paginate at 50. Files include path,
directory, size, import time, probe, release, download ID and grouped TV episode
labels. History contains safe job identity/title/source/state/progress/size/dates
and error. Metadata refresh failure retains the previous cache. Reads do not
contact TMDb or scan the media filesystem.

## Setup and installation health

All these routes require the administrator session and mutations also require
CSRF/origin validation. They never return credentials or contact providers.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/setup` | Actual saved-connection checklist; Torznab optional; recovery hold state |
| GET | `/api/v1/system/checks` | Last report from this process or null; current recovery_pending; no probes |
| POST | `/api/v1/system/checks` | `{}`; bounded local checks with private temporary folder probes; 409 if busy |
| POST | `/api/v1/system/recovery/resume` | `{"reviewed":true}`; validates critical checks and marker, releases restored worker hold; does not enable schedules |

Reports contain checked_at (Unix seconds), checks (id/title/state/message,
optional free_bytes) and resources (heap_bytes, go_memory_bytes, goroutines,
logical_cpus). Go memory is not total process/container RSS. Checks are snapshots,
not persistent monitoring; reports reset on restart. Backup/restore is local CLI
only; there is no web endpoint accepting arbitrary filesystem backup paths.

## Common behavior

All routes are same-origin. JSON uses snake_case. All `/api/v1` routes require
an administrator session except the three public auth routes below. Mutations
also require `X-CSRF-Token` from session status and an allowed browser Origin.
Cookies are HttpOnly/SameSite=Strict; there is no Bearer app API token yet.
The API is versioned under `/api/v1`. Changes within v1 should be additive unless
explicitly documented as breaking before the first stable release.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/healthz` | Process liveness; `{"status":"ok"}` |
| GET | `/readyz` | Database ping, 2-second deadline; 200 ready or 503 |
| GET | `/api/v1/system` | Version, schema, uptime, configured paths, explicit capabilities |
| GET | `/api/v1/dashboard` | Library name, real SQL movie/series counts, settings revision/time |
| GET | `/api/v1/settings` | Current library preferences and revision |
| PUT | `/api/v1/settings` | Validate and atomically update preferences if revision matches |
| GET | `/api/v1/library?kind=movie&page=1&query=&status=all&monitored=all&sort=title&profile=all` | Filtered movies, saved metadata, filtered/collection totals and coherent effective-profile context |
| GET | `/api/v1/library?kind=series&page=1` | Same filters and pagination for TV parent catalog status/monitoring |
| POST | `/api/v1/library/monitoring/preview` | Read current monitoring/revisions for explicit kind/ids; authenticated + CSRF; no writes |
| PUT | `/api/v1/library/monitoring` | Apply desired parent monitoring to exact expected items atomically; stale selection returns 409 |
| POST | `/api/v1/library/profiles/preview` | Review explicit kind/ids and chosen profile/default, with current assignments and full target rules |
| PUT | `/api/v1/library/profiles` | Atomic assignment from expected item/profile/schedule revisions; stale review returns 409 |
| GET | `/api/v1/auth/session` | Public setup/auth status; authenticated response includes username, csrf_token, expires_at |
| POST | `/api/v1/auth/setup` | Public, rate-limited first admin creation; username, password, setup_token; returns 201 + cookie |
| POST | `/api/v1/auth/login` | Public, rate-limited username/password login; returns 200 + cookie |
| POST | `/api/v1/auth/logout` | Revoke current session and clear cookie; requires CSRF |
| GET | `/api/v1/providers/tmdb` | configured + updated_at; never the token |
| PUT | `/api/v1/providers/tmdb` | `{ "token": "..." }`; validate remotely, encrypt and save; failure preserves previous token |
| DELETE | `/api/v1/providers/tmdb` | Remove saved credential; retain all library metadata |
| POST | `/api/v1/providers/tmdb/test` | Test the saved credential against real TMDb |
| GET | `/api/v1/metadata/movies?query=title&page=1` | Real TMDb movie search; per-result in_library flag |
| GET | `/api/v1/metadata/movies/{tmdb_id}` | Canonical movie details from TMDb |
| POST | `/api/v1/library/movies` | Add canonical metadata by TMDb ID; 201 created or 200 existing |
| GET | `/api/v1/library/movies/{id}` | Saved movie by catalog ID, without a TMDb request |
| GET | `/api/v1/metadata/series?query=title&page=1` | TMDb TV search with per-result in_library |
| GET | `/api/v1/metadata/series/{tmdb_id}` | Canonical show details and season summaries |
| POST | `/api/v1/library/series` | Canonical show add by tmdb_id and optional monitored; 201 new/200 existing |
| GET | `/api/v1/library/series/{id}` | Saved show, revision, status and season summaries |
| POST | `/api/v1/library/series/{id}/refresh` | `{ "revision": 1 }`; refresh show metadata/seasons, preserve selections |
| GET | `/api/v1/library/series/{id}/seasons/{number}` | Saved season and episodes; no provider request |
| POST | `/api/v1/library/series/{id}/seasons/{number}/refresh` | `{ "revision": 1 }`; load/refresh canonical episodes |
| PUT | `/api/v1/library/series/{id}/monitor` | `{ "monitored": false, "revision": 1 }`; pause/resume show |
| PUT | `/api/v1/library/series/{id}/seasons/{number}/monitor` | Same body; season selection |
| PUT | `/api/v1/library/series/{id}/seasons/{number}/episodes/{episode}/monitor` | Same body; episode selection |
| GET/PUT/DELETE | `/api/v1/integrations/torznab` | Safe status; test/encrypt configuration; disconnect |
| GET/PUT/DELETE | `/api/v1/integrations/qbittorrent` | Safe status; test/encrypt configuration; disconnect when no active jobs |
| POST | `/api/v1/integrations/{name}/test` | Test saved Torznab/qBittorrent connection |
| GET | `/api/v1/releases?movie_id=1&query=title` | Search enabled indexers; materialize encrypted short-lived selections |
| GET | `/api/v1/downloads?page=1` | Activity: items, page, total, limit 24 |
| POST | `/api/v1/downloads` | `{ "release_id": "..." }`; 202 queued or 200 existing; item + created |
| GET | `/api/v1/downloads/{id}` | One durable job with progress/error fields |
| POST | `/api/v1/downloads/{id}/retry` | `{}`; retry/resume blocked, failed or stopped tracking |
| POST | `/api/v1/downloads/{id}/stop` | `{}`; stop blocked/failed local tracking, leaving client/files untouched |
| POST | `/api/v1/downloads/{id}/file` | `{ "index": 3 }`; select a file from a choose_file job |
| GET | `/api/v1/library/series/{id}/seasons/{number}/releases?episode=1&query=title` | Search TV category 5000 for one missing episode; omit episode for selected missing episodes in a monitored season |
| POST | `/api/v1/downloads/{id}/mapping` | Confirm a TV job's completed file/episode mapping and explicit skips |
| GET/PUT | `/api/v1/automation` | Saved interval schedule, revision, next run, safe integration presence and state |
| POST | `/api/v1/automation/run` | `{}`; request one durable run using saved mode, 202 or 409 already requested/running |
| GET | `/api/v1/automation/runs` | `{items}`; latest 30 search runs with state, message, timestamps, mode, checked and queued counts |
| GET | `/api/v1/automation/runs/{id}` | `{items}`; up to 25 target results, outcomes, download IDs and ranked decisions |
| GET | `/api/v1/automation/items?page=1` | Profile assignments/monitoring, items/page/total/limit 24 |
| PUT | `/api/v1/automation/items/{id}` | Revision-checked profile assignment and optional movie monitoring |
| GET/POST | `/api/v1/quality-profiles` | `{items}` list or validated profile creation (201) |
| PUT | `/api/v1/quality-profiles/{id}` | Revision-checked profile edit |
| GET/PUT | `/api/v1/subtitles/preferences` | `{preferences,languages}`; revision-checked subtitle rules |
| GET/PUT/DELETE | `/api/v1/providers/opensubtitles` | Safe status; test/encrypt credentials; disconnect |
| POST | `/api/v1/providers/opensubtitles/test` | `{}`; test saved account without downloading a subtitle |
| GET | `/api/v1/subtitles/files?page=1` | Registered files, detected tracks and last/current jobs; items/page/total/limit 24 |
| GET | `/api/v1/subtitles/files/{id}` | One registered file, tracks/job and cached subtitle candidates |
| POST | `/api/v1/subtitles/files/{id}/search` | `{}`; 202 queued search for manual review, no automatic download |
| POST | `/api/v1/subtitles/files/{id}/retry` | `{}`; 202 retry preserving job intent/selection |
| POST | `/api/v1/subtitles/candidates/{id}/download` | `{ "reviewed": true }`; 202 queue an opaque, unexpired subtitle choice |

Library responses contain `items`, `page`, `total`, and `limit: 24`. Page bounds
are 1–100000; ordering is case-insensitive title then ID. Search responses contain
`items`, `page`, `total_pages` (capped at 500), and `total_results`. Queries accept
1–200 characters without control characters; search pages accept 1–500.

Add movie body: `{ "tmdb_id": 11, "monitored": true }` (`monitored` defaults
true). The server fetches metadata; caller-supplied titles, artwork or paths are
rejected as unknown fields. Response is `{ "created": true, "item": {...} }`.
The `(kind,tmdb_id)` uniqueness constraint prevents duplicate rows. Existing adds
return the original item without calling TMDb or changing monitoring preferences.
New movie adds save status `missing`; selecting a release is a separate action.
Queueing sets `downloading`; a verified import transaction sets `available`.

TV adds use the same canonical/idempotent add contract and store `kind: series`.
TV metadata shares title/year/artwork fields, using first_air_date as release_date,
and adds series_status/seasons. Saved TV details add revision/updated_at. Seasons
have local id, tmdb_id, number (0 means specials), title, overview, air_date,
episode_count, monitored, loaded_at, known_episodes, selected_episodes and
available_episodes.
Season GET returns `{ "season": {...}, "episodes": [...], "revision": 1,
"series_monitored": true }`. Episodes include id, tmdb_id, number, title,
overview, air_date, runtime_minutes, monitored, effective_monitored, available
and file_path (registered relative media path; empty when not imported).
Effective monitoring requires all three show/season/episode switches to be on.
Every TV change/refresh compares the show revision transactionally and increments
it; stale edits return 409 revision_conflict. Invalid child targets roll back the
revision. Refresh preserves selections and retains previously stored episodes
absent from a later provider response. Provider identity changes at existing
season/episode numbers are refused to preserve download/file associations.

TV search requires a loaded season, accepts 0–10000 for season and 1–100000 for an
optional episode. Season search snapshots up to 100 monitored missing episodes;
individual episode searches are manual and can ignore monitoring. Queue uses the
same opaque release_id contract and reserves episode targets transactionally.
Overlapping jobs or imported targets return 409 episode_busy. Disjoint TV jobs
can share a series. One info hash can belong to only one target set.

Activity adds kind (movie/series) and TV targets with episode_id, tmdb_id,
season, number, title, skipped and imported. Legacy movie_id/movie_title fields
identify the parent catalog item/title for both kinds. TV choices include optional
suggestions (episode IDs). Completed TV jobs block with map_episodes before any
import. Confirm with, for example:

```json
{"files":[{"file_index":0,"episode_ids":[11,12]},{"file_index":3,"episode_ids":[13]}],"skip_episode_ids":[14]}
```

Every target must be assigned exactly once or skipped; unknown indexes/IDs,
duplicates and nonconsecutive/shared groups over 20 episodes are rejected with
400 invalid_mapping. At least one file must be mapped. The server uses stored
file names, sizes and contained source paths; callers cannot supply paths.
Confirmation locks the mapping and starts importing. Later retries preserve it;
completed per-file plans are not imported twice. A parent job completes after all
mapped files commit. Stop tracking releases episode reservations while retaining
all torrents and files. See TV_DOWNLOADS.md for naming/recovery/filesystem limits.

Torznab PUT body is `{ "url": "https://indexer.example/api", "api_key": "..." }`.
qBittorrent PUT body is `{ "url": "https://qbt.example", "username": "admin",
"password": "...", "download_path": "/downloads" }`. Omitted credential fields
retain a saved credential; changed URL/username requires explicit re-entry.
Settings responses contain configured/url and, for qBittorrent, username and
download_path. Tests and saves call real services; failed saves preserve old data.

Release responses contain items with id (opaque), movie_id, title, size_bytes,
seeders/peers (nullable) and published_at. Selection expiry is 30 minutes;
searches/settings changes invalidate unqueued older selections. No download URLs
or magnets appear in responses. Queue payloads accept a release ID only.
Activity exposes state, progress, bytes, client_state, attempts, safe error fields,
file choices and timestamps; encrypted submission payloads/credentials are omitted.
States are queued, submitting, downloading, importing, blocked, failed, completed
and cancelled. See MOVIE_WORKFLOW.md for leases, retry and import semantics.

Movie DTOs include tmdb_id, title, overview, year (nullable), release_date,
poster_path, poster_url, and runtime_minutes (nullable). Saved library items
add id, kind, status and monitored. Poster URLs are restricted to TMDb's official
image host; no upstream credential is sent to the browser.

Settings additionally returns episode_naming: standard or with-title. PUT accepts
this enum; omission retains the saved value for existing clients. New TV jobs
snapshot the TV folder and naming choice. Renaming existing files is not implied.

Authentication bodies and credential PUTs use the same bounded, strict JSON
decoder as settings. Passwords require 12+ Unicode characters and at most 256
bytes. Usernames require 3–40 ASCII letters, digits, underscores or hyphens.
Sessions last 24 hours and survive restart. See AUTHENTICATION.md for recovery,
setup-code handling, deployment origin and key backup requirements.

Example settings response:

```json
{
  "library_name": "My library",
  "movie_folder": "Movies",
  "series_folder": "TV Shows",
  "revision": 1,
  "updated_at": "2026-10-22T00:00:00.000Z"
}
```

PUT sends the first four fields; `updated_at` is read-only. Requires
`Content-Type: application/json`, at most 16 KiB, exactly one JSON object, and no
unknown fields. A supplied browser Origin must match scheme and host exactly;
cross-site Fetch Metadata is rejected. No permissive CORS headers are sent.
The Vite development proxy preserves origin/host. For a reverse proxy, configure
REELSCAREY_PUBLIC_URL and preserve the original Host; forwarded headers are ignored.

Library names accept 1–80 Unicode characters without controls. Folder names
accept 1–80 ASCII letters, digits, spaces, underscores, and hyphens, start with a
letter/digit, reject reserved Windows device names, and must differ ignoring
case. Surrounding whitespace is removed. Folders are preferences only: saving
does not create, scan, move, or rename any file. Media/download roots are
process settings configured through environment variables. Imports require
writable media and readable downloads.

Errors use `{"error":{"code":"...","message":"..."}}`:
- 400 invalid input, kind, or JSON
- 401 application login required or invalid login credentials
- 403 cross-origin mutation
- 404 unknown API endpoint (never the frontend HTML)
- 405 unsupported method, with `Allow`
- 409 stale settings revision; reload before retrying
- 413 body too large
- 415 unsupported content type
- 429 authentication throttled or TMDb rate-limited; Retry-After accompanies it
- 500 unexpected server failure; details in local logs
- 502 upstream authorization, response, connectivity or availability failure;
  TMDb's own 401 does not log the administrator out
- 503 readiness or frontend unavailable

Missing TMDb configuration returns 409 `tmdb_not_configured`; stale settings use
409 `revision_conflict`. Provider errors are safe summaries, never upstream body
text, credentials, request headers or URLs containing secrets.

Planned route groups (not registered): broader library editing,
multi-indexer/client CRUD and generic `/jobs`.
Design each group's concrete request/response types with its
first complete vertical slice; do not expose placeholder 200 responses.

Subtitle PUT preferences accepts enabled, tracks (`[{"language":"en","variant":"full"}]`),
hearing_impaired (any/exclude/prefer), match_policy (hash/hash_or_release),
interval_minutes (60–10080), only_monitored and revision. Choose 1–9 unique tracks,
at most three languages from GET's supported languages list, and full/signs/forced
variants. Defaults are paused, all three English variants, any SDH, hash_or_release,
360 minutes and monitored items only. A stale revision returns 409; saving fences
running work. Manual searches/downloads work while automatic work is paused.

OpenSubtitles PUT accepts username, user_agent (registered application name and
version), api_key and password. The secret fields are required on first connect;
omitting them retains saved values. Changing username requires a password.
The account is tested before replacing any configuration. GET returns configured,
username, user_agent, state, message, cooldown_until (Unix seconds) and remaining
(nullable last reported allowance), never passwords, API keys, JWTs or signed URLs.
Provider rejection uses 502 rather than the application's login-required 401.
Quota/cooldown errors use 429 with safe messages; the status exposes the reset.

Files and candidates use server-owned IDs; request bodies cannot supply paths,
provider file IDs, URLs or timing assertions. Candidate responses include language,
variant, release/file_name, hash_match, hearing_impaired, trusted, score, automatic,
can_download and reasons. A choice lasts 30 minutes and is bound to file SHA,
provider generation and preferences revision; stale choices return 409
candidate_expired. reviewed=false returns 400 review_required. Duplicate active
requests return 409 subtitle_busy. No matching tracks is a real no_match state.
See SUBTITLES.md for detection, matching, automatic work and filesystem limits.

Automation PUT accepts only `enabled`, `mode` (search/download),
`interval_minutes` (15–10080), `batch_size` (1–25), `default_profile_id` and
`revision`. New schedules are paused/search-only. GET additionally returns
next_run (Unix seconds), requested, state, message, indexer_connected and
client_connected. No enabled indexer is waiting_indexer; saving a schedule never
connects an integration. Search now uses saved mode and never changes enabled.
Saving preferences invalidates in-progress policy guards; existing queued
downloads continue. State/message describe the last/current run, while enabled
controls recurring intent. Run retention is 100; UI/API lists the latest 30.

Profile bodies contain name (1–60 bytes), qualities (unique ascending subset of
480/720/1080/2160), cutoff (one allowed value), language (any/english/french/
german/spanish/italian/japanese/korean/chinese), min_seeders (0–10000), max_size_gib
(1–500), required_terms and rejected_terms (comma-separated, at most 200 bytes
each), plus revision for edits. id may be omitted or match the route. Create
uses no ID and returns a generated ID/revision 1. Names are unique ignoring
ASCII case; at most 20 profiles are supported. No profile deletion is exposed.

Assignments contain media_id, title, kind, monitored, profile_id (null follows
schedule default) and revision (0 until first assignment). PUT accepts profile_id,
revision and optional monitored for movies. TV monitoring uses the existing
show-revision routes to preserve season/episode semantics. Invalid profile IDs,
unknown fields and stale revisions are rejected without partial writes.

Results contain media_id, episode_id (nullable), kind, season/episode numbers,
target, outcome, message, download_id (nullable) and decisions. Each decision
includes opaque release_id, title, quality, language, score, eligible and reasons.
IDs in history are audit records and may expire; request fresh releases before
manual queueing. Upgrade suggestions cannot queue replacement downloads.
Automation uses the shared transfer service and a transaction-bound policy check;
there is no public bypass for automatic submissions. See AUTOMATION.md for
identity/ranking rules, date eligibility, retries and filesystem guarantees.
