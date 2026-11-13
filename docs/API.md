# REST API — media catalog, downloads, imports, automation and operations

## Quality, diagnostics and artwork additions

Administrator GET/POST `/api/v1/subtitles/language-profiles` and
PUT/DELETE `/api/v1/subtitles/language-profiles/{id}` manage ordered subtitle
tracks. GET/PUT `/api/v1/subtitles/language-defaults` manages Movies/TV/Anime
defaults. PUT/DELETE `/api/v1/library/items/{id}/language-profile` assigns or
removes a title override. The worker overlays the selected profile's tracks on
the global preferences; audio languages are stored but release selection still
uses the quality profile language.

GET `/api/v1/quality-profiles` and PUT `/api/v1/quality-profiles/{id}` accept
`scoring.custom_formats`, an array of
`{name,patterns,weight,required,excluded}` objects. Patterns are case-insensitive
regular expressions. `required` and `excluded` can block a release; other
weights only reorder eligible releases. At most 20 formats and 20 patterns per
format are accepted; invalid expressions and profiles fail validation.

`GET /api/v1/library/profile-defaults` returns `{items,profiles}`;
PUT replaces the Movies, TV and Anime defaults with `{items:[{library_key,
profile_id,revision}]}`. A null `profile_id` keeps the search schedule default.
New titles inherit the matching default; existing titles and explicit per-title
profiles are unchanged.

`GET /api/v1/metadata/movies/{id}/watch?region=US` returns region-scoped TMDb
provider suggestions. `GET /api/v1/diagnostics` is a read-only aggregation of
failed downloads, blacklist entries, failed events, indexer errors and
unfinished automation runs, plus read-only write-ahead-log size counters. `GET /api/v1/storage/insights` reports registered
file counts/sizes, duplicate groups, unresolved scan files and analyzed files
below their assigned cutoff. Neither page scans disk or changes state.

The optional `settings.artwork_export` preference writes a missing `poster.jpg`
beside registered movies from TMDb artwork. Existing images are never replaced;
TV episode artwork is not written.

Indexers with `kind: "cardigann"` accept a bounded `definition` YAML string and
the tracker homepage URL. The adapter supports a conservative Cardigann subset:
HTML/CSS search rows, common field filters, cookie/user-agent sessions and
torrent or magnet links. Definitions are never returned by the list endpoint,
redirects are refused, and non-torrent definitions are not claimed as working.

## Integration surface (schema 81–87)

Administrator GET/POST `/api/v1/api-keys` and DELETE `/api/v1/api-keys/{id}`
manage scoped compatibility keys. The token is returned once; only its digest is
stored. `/sonarr`, `/radarr`, `/prowlarr` and `/api/v3` accept `X-Api-Key`,
`?apikey=` (GET/HEAD reads only; mutations must send the header) or a Bearer
token. Compatibility reads and the four allowed `POST
/command` names are documented in [integrations](INTEGRATIONS.md). Unsupported
operations return an explicit 404/501; the layer never invents history.

Administrator `/api/v1/notifications` manages durable notification channels
(`email`, `ntfy`, `gotify`, `pushover`, `telegram`, `discord`, `webhook`).
`/api/v1/notifications/{id}/test` sends synchronously; `/outbox` lists deliveries
and `/outbox/{id}/retry` explicitly retries an uncertain or failed one.
Administrator `/api/v1/jellyfin` and `/api/v1/plex` manage encrypted read-only
media-server connections with `/activity`, `/refresh` and (Plex) `/sections`.
See [integration limits](INTEGRATIONS.md). These routes require an administrator
session; API keys do not reach them.

Torrent detail accepts `?section=general|content|trackers|peers|http` to load
only the requested data. Omitting the section returns the full detail response.
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

Administrator GET `/api/v1/events?before={cursor}` returns up to 50 recorded state
changes and an opaque chronological cursor for older entries. Migration 028
backfills known creation times and last saved states without inventing
intermediate actions. GET/POST/DELETE
`/api/v1/library/items/{id}/related` reads or changes explicit mutual links
between saved titles; writes accept `{other_id}` and require CSRF.
GET `/api/v1/library/items/{id}/franchise` automatically discovers movie and
TV relationships using Wikidata's explicit media-franchise statements and exact
TMDb identifiers. It returns at most 200 provider records, marks truncation,
and associates exact saved titles without modifying them. Provider data may be
incomplete, community-edited or unavailable; results are suggestions.

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
schedule a check. Providers are `tmdb`, `trakt`, `imdb_mdblist`, `tmdb_private`,
`trakt_private`, `trakt_watchlist` and `trakt_watchlist_private`; a Trakt
watchlist uses the literal `watchlist` list ID and the profile owner as `owner`.
POST `/api/v1/requester/imdb-csv` accepts a bounded CSV snapshot. `/api/v1/requester/discord` provides write-only webhook PUT, status
GET and DELETE; POST `/api/v1/requester/requests/{id}/notification/retry`
explicitly retries an uncertain delivery. PUT `/api/v1/requester/password`
changes the requester's password and revokes their sessions. Requesters are denied all other
administrator APIs. Authenticated mutations require CSRF and same origin. See
[request accounts and limits](REQUESTS.md).

Administrator GET/PUT `/api/v1/discord-bot` reads or saves the companion-bot
configuration; POST `/api/v1/discord-bot/register` registers its commands, and
POST `/api/v1/discord-bot/notices/{id}/retry` retries an uncertain notice.
POST `/api/v1/discord/interactions` is the companion bot's external endpoint:
it is intentionally not cookie-authenticated and verifies Discord's Ed25519
signature instead. See [request accounts and limits](REQUESTS.md).

## Music catalog

Administrator GET `/api/v1/metadata/music/artists?query=&page=` searches
MusicBrainz. GET/POST `/api/v1/library/music/artists` lists saved artists or
adds one with `{musicbrainz_id, monitored?}`; a repeated add returns 200 with
`created:false`. GET `/api/v1/library/music/artists/{id}` returns the artist and
its albums. GET `/api/v1/library/music/artists/{id}/tracks?page=&limit=`
returns only tracks with registered files, in newest-album order; `limit`
defaults to 15 and is capped at 100. GET
`/api/v1/library/music/files/{id}/stream` serves one registered audio file
with its media type and HTTP Range support; the response never exposes the
stored path. GET `/api/v1/library/music/albums/{id}`
returns the album and its tracks; the first read loads tracks from MusicBrainz.
GET `/api/v1/library/music/artists/{id}/image` and
`/api/v1/library/music/albums/{id}/cover` serve downloaded, cached artwork from
fixed providers; missing artwork returns 404 so the UI can fall back. The cache
is refreshed after 30 days and never exposes provider URLs to the browser.

GET `/api/v1/library/music/artists/{id}/top-tracks?limit=20` returns overall
Deezer top tracks with local library matches, falling back to Deezer track
search and iTunes search when the artist's top list is empty. GET
`/api/v1/library/music/artists/{id}/discography?limit=` returns the full public
Deezer album list with local album matches. GET `/api/v1/library/music/albums`
and `/api/v1/library/music/tracks` power the unified Albums and Tracks views;
both accept `page`, `query` and an optional `limit`.
POST `/api/v1/library/music/albums/{id}/acquire` queues every missing catalog
track for one album. POST
`/api/v1/library/music/provider-albums/{id}/acquire` resolves a Deezer album
and queues its tracks. Both accept `{root,format,reviewed:true}` and run in the
durable acquisition worker. POST
`/api/v1/library/music/links/preview` accepts `{url}` and returns public
Spotify/Deezer/YouTube metadata; a non-URL query returns YouTube search
candidates. POST `/api/v1/library/music/links` accepts
`{url,track_indices,root,format,reviewed:true}` and queues selected tracks
without downloading from the request. GET
`/api/v1/library/music/acquisitions?page=` lists link/chart/top-track jobs and
GET `/acquisitions/{id}` returns one. GET
`/api/v1/library/music/charts?limit=` returns Deezer chart tracks. GET/PUT
`/api/v1/library/music/monitor` manages the paused-by-default monitor schedule;
GET/POST `/monitor/sources` and PUT/DELETE `/monitor/sources/{id}` manage
saved playlist/artist links; POST `/monitor/run` requests one bounded run.

GET/PUT `/api/v1/library/music/server` reads or changes the opt-in
Subsonic-compatible endpoint. POST/DELETE `/server/token` creates or removes the
client token; the token is returned only on creation. The token is 20 characters
of Crockford's Base32 alphabet. The plaintext-password form accepts it in any
case and ignores spaces and dashes; clients that send `md5(password+salt)` need
the token exactly as shown. The optional `/rest/*`
endpoint uses that token on the same ReelScarey port; it is disabled by default
and never opens another listener.

GET/POST `/api/v1/library/music/scans` lists scans or starts one with
`{roots:[...],full?}`. A legacy `{root}` is still accepted for one contained
folder, but the UI and configured-root path send `roots`. Scans report their root
snapshot and keep each registered file's own root. `full:true` re-examines every
eligible file, including registered ones; a normal scan skips unchanged
registered paths and carries matched-but-unregistered paths forward without
repeated work. Neither path duplicates `music_files` rows for one relative path.
GET `/scans/{id}?page=` returns its state and files, plus `full_scan`, `phase`,
`progress_total`, `progress_done`, `current_path`, `message`, `skipped_count`,
`request`, `requested_at` and `work_until`. The scan list reports the same
fields. GET `/scans/{id}?page=&state=` returns one page of scan files and
accepts `state=all|matched|unresolved|registered|skipped`; the import UI uses
`unresolved` for large scans. POST `/scans/{id}/match-all` accepts `{reviewed:true}` and returns `202`
with the queued scan (`request:"match"`); the durable worker first matches the
saved catalog and the files' own tags, creates tag-derived local catalog rows
when those tags are complete (no MusicBrainz or AcoustID request), then
fingerprints still-unresolved files through AcoustID when configured, and only
then runs MusicBrainz discovery for files without a complete local tag identity.
The final message reports local, fingerprint and provider match counts. It never
registers, moves or tags media. POST
`/scans/{id}/finalize` accepts `{reviewed:true}` and returns `202` with
`request:"finalize"`; the worker registers the reviewed matched cohort in place
and applies each reviewed edit plan sequentially, waiting for every edit job.
Files registered by earlier scans are never re-organized by a later finalize.
The cohort survives a restart. A partial failure leaves the scan in `review`
with `phase:"ready"` and an explicit count; unmatched files stay for manual
review. Both requests are rejected (409) while the scan is running or another
request already owns it. POST
`/scans/{id}/register` accepts `{file_ids,reviewed:true}`; POST `/discover`
accepts `{file_ids,reviewed:true}` or `{all:true,reviewed:true}` and performs a
bounded, reviewed MusicBrainz catalog discovery for unresolved files. It uses a
tagged release-group or album-artist MBID, or one exact normalized artist and
album title (with a bounded discography fallback), and is idempotent on rerun;
unmatched or ambiguous evidence stays unresolved with a warning or file message.
Its response reports `selected`, `matched`, `artists_added`, `albums_added`,
`unresolved`, `remaining`, `deferred` and `warnings`. POST
`/scans/{id}/fingerprint` accepts `{file_ids,reviewed:true}` or
`{all:true,reviewed:true}` and performs bounded AcoustID/Chromaprint matching
for unresolved files. It processes at most 20 files and 10 album groups, sends
only the fingerprint and whole-file duration, requires a score of at least 0.85
and exactly one distinct release group, and never moves or edits media. Its
response adds `fingerprinted` and `provider_calls`. GET/PUT/DELETE
`/api/v1/library/music/acoustid` returns
`{configured, updated_at, fingerprint_available, message}`, saves `{api_key}`
after basic format validation or removes the key; the key is never returned.
GET/PUT/DELETE `/api/v1/library/music/musicbrainz` returns
`{base_url, default_base_url, using_default, cooldown_until, message,
cache_entries}`, saves an encrypted optional `{base_url}` or resets to the
public server. A saved URL is a server prefix; `/ws/2` is appended once when
missing. Plain HTTP is accepted only for localhost, loopback, RFC1918 and IPv6
ULA hosts; credentials, query strings, fragments and path traversal are
rejected. The status never contains a secret. No MusicBrainz mirror is bundled.
POST `/cancel` and `/skip` handle review exceptions. Cancel clears any queued
workflow request so the worker cannot resume it.
GET `/api/v1/library/music/files?page=` lists registered files. POST
`/api/v1/library/music/edits/preview` accepts `{file_ids}` and returns a plan
with `plan_hash`; POST `/edits/apply` accepts
`{file_ids,plan_hash,reviewed:true}`; GET `/edits/{id}` returns the job; POST
`/edits/{id}/rollback` accepts `{reviewed:true}`.

POST `/api/v1/library/music/albums/{id}/releases` accepts `{query?}` and returns
ranked audio-indexer results without queueing. POST
`/api/v1/library/music/albums/{id}/import` accepts `{source_path,root}` and
starts a reviewed folder import; GET `/imports?page=` lists jobs and GET
`/imports/{id}` returns one. Imports map a complete set of audio files to the
album's tracks, copy and tag them without modifying the source, and publish
without overwrite.

POST `/api/v1/library/music/albums/{id}/download` accepts `{release_id,root}`
and queues the candidate through the selected download client. GET
`/downloads?page=` lists music jobs; GET `/downloads/{id}` returns one; POST
`/downloads/{id}/cancel` and `/retry` manage it. GET
`/downloads/{id}/files` probes the completed folder and POST
`/downloads/{id}/mapping` accepts `{mappings:[{file_index,track_id}]}` to
resume a blocked import with explicit track assignments. GET/PUT `/preferences` manages
lossless, minimum-bitrate and preferred-format rules. GET `/wanted?page=` lists
monitored albums with no registered files. GET/PUT `/schedule` manages the
wanted-album schedule and POST `/schedule/run` performs one bounded run.
GET/PUT/DELETE `/albums/{id}/preferences` reads, saves or resets a per-album
preference override. GET `/upgrades` lists albums whose registered files fall
below their effective preference; it is a suggestion only.

These routes are administrator-only. Edits stage the source, verify the result,
publish without overwriting an existing destination, retain a backup before a
tag rewrite and support rollback. The AcoustID application key is stored
encrypted in the integration store and covered by the existing `secrets.key`
backup validation; only the Chromaprint fingerprint and duration are sent to
AcoustID. MusicBrainz responses and rate-limit cooldowns are stored locally;
`music_rate_limited` responses include `Retry-After` and a matching workflow
pauses in `review`/`ready` with its matched rows intact. See
[music scope and limits](MUSIC.md).

## YouTube sourcing

Authenticated POST `/api/v1/youtube/inspect` accepts `{url}` for a bounded
public YouTube video, playlist or channel inspection. GET/POST
`/api/v1/youtube/collections` lists or creates a named custom collection.
POST `/api/v1/youtube/reviews` accepts an explicit `video_id`, target
(`custom`, `movie` or `episode`) and `subtitles` (`languages`, `automatic` and
`quality`: `best`, `1080` or `720`); the server independently inspects the video
and returns a short-lived review. POST `/api/v1/youtube/reviews/{id}/queue`
applies that review. GET `/api/v1/youtube/jobs` returns state, target label,
quality, progress and speed/ETA for each job; POST `.../cancel` also stops a
running download. GET `/api/v1/youtube/jobs/{id}/import-review` returns the
verified media and subtitle evidence and destinations; POST `.../import` submits
that exact review. GET `/api/v1/youtube/files` lists custom-library files.

Per-show sources use GET/POST
`/api/v1/library/series/{id}/youtube-sources` (create/update requires
`reviewed: true`; new sources start paused), POST `.../youtube-sources/preview`
for the explicit video-to-episode mapping, and POST/DELETE
`.../youtube-sources/{source}` for a fenced check, an explicit baseline
backfill (`{"action":"backfill"}`) or removal. A source with
`mapping: "manual"` stores explicit video-to-episode bindings: GET
`.../youtube-sources/{source}/matches` lists the source's videos beside the
show's loaded episodes, and PUT the same path with
`{revision, queue, matches: [{video_id, episode_id, title, channel}]}` replaces
the bindings; `queue` requests one run so the bound episodes are queued. An
explicit binding overrides monitoring, and an episode that already has a file is
queued as a reviewed replacement.

The playlist batch uses POST `/api/v1/youtube/playlist/preview` with
`{source_url}`, optional `show_id` and `season_number`. It returns ranked saved
show matches, loaded seasons and per-video episode matches (missing, available,
active or unmatched), with the longest video per episode preselected. POST
`/api/v1/youtube/playlist/queue` accepts `source_url`, `show_id`,
`season_number`, `quality`, `subtitles`, explicit `entries` (video IDs),
`replace_existing` and `keep_source`; it re-inspects the playlist, verifies every
mapping and queues only the selected missing episodes. Existing files require
`replace_existing` and still go through the acquired-replacement review.

Mutations require CSRF and same origin. See [YouTube sourcing](YOUTUBE.md) for
limits and setup.

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

Library browsing accepts kind=anime for explicitly classified series. Movies
includes all films, including previously classified anime films. The ordinary TV
catalog excludes Anime series. Adds accept optional category=anime. Item responses add category/category_revision, counts add anime.
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
Per-show settings also include english_dub_only. GET/PUT
`/library/anime/preferences` manages the global dub rule (one `/regex/i` or
phrase per line) with a revision; POST `/library/anime/dub-test` tests
`{rule?, title}`; POST `/library/anime/english-dub` accepts
`{enabled, reviewed:true}` and applies the toggle to every Anime show.
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
Calendar additionally accepts month YYYY-MM (default is the current month in the
server's configured timezone)
and a calendar-only `release` filter: `all`, `theatrical`, `digital` or
`physical` (default `all`). Supplying `release` to `/api/v1/wanted` is rejected.
Unknown/repeated keys and invalid values return 400.
Wanted defaults to status missing; Calendar to all; monitoring defaults to
effectively monitored. Query is literal, up to 200 UTF-8 bytes without controls;
page is 1–100000.

Responses contain items, status counts, coverage.unloaded_seasons/undated,
total/page/limit (50), as_of_date and effective month/kind/status/query/
monitored_only. Counts ignore the selected status but follow other filters and
the view's scope (Wanted excludes registrations; Calendar includes dated items
in its month). Undated coverage counts untracked, unavailable undated items that
can be reviewed with Wanted's undated filter. Entries contain kind/media_id,
nullable episode_id/year/season/episode, title/episode_title, normalized date,
`release_kind` (empty, theatrical, digital or physical), the saved
`theatrical_date`/`digital_date`/`physical_date` (empty when unknown; TV entries
always empty), poster_url, monitored/monitoring_reason, available, status and
non-secret download_id/download_state/protocol. Calendar lists one movie row per
known release kind in the month and falls back to one `release_kind:""` row from
the canonical release_date when no detailed kind exists. Wanted remains one row
per movie using the canonical release_date. No source/client secrets or paths
appear. Reads do not initiate searches, scans or jobs. See
[planning](PLANNING.md) for status precedence, cached-date coverage and calendar
semantics.

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
| GET | `/api/v1/library/imports` | Recent scans, configured roots and movie/TV server roots |
| POST | `/api/v1/library/imports` | `{kind:"movie"|"series",library_key?:string,root?:string,automatic_matching?:boolean}`; queue discovery for one configured root or every root; matching defaults false; 202 |
| GET | `/api/v1/library/roots` | Persisted scan roots for Movies, TV Shows, Anime and Music with availability and defaults |
| PUT | `/api/v1/library/roots` | `{revision,libraries:[{key,roots:[...]}]}`; replaces supplied root sets; authenticated mutation with CSRF; 409 on stale revision |
| GET | `/api/v1/library/imports/{id}?page=1&state=` | Scan status/counts, its root snapshot, plus 50 discovered files; optional review/matched/blocked/registered/skipped filter |
| POST | `/api/v1/library/imports/{id}/cancel` | `{}`; stop discovery; prior reviewed registrations remain independent |
| GET | `/api/v1/library/imports/{id}/duplicates` | Up to 100 unresolved same-episode duplicate groups with every claiming file; read-only |
| POST | `/api/v1/library/imports/{id}/duplicates/resolve` | `{key,keep_file_id,reviewed:true}`; queue the chosen file and mark the other variants skipped; 202 |
| DELETE | `/api/v1/library/imports/{id}` | `{}`; forget finished review records; refuses active discovery/matching/verification; never deletes registered media |
| POST | `/api/v1/library/import/backup/preview` | Raw Radarr or Sonarr backup ZIP (≤256 MiB) or iCalendar export (≤5 MiB); returns source, title list and `in_library`; reads only Movies/Series or SUMMARY/DTSTART/UID |
| POST | `/api/v1/library/import/backup/apply` | `{items:[{kind,tmdb_id,tvdb_id,monitored}]}`; adds the reviewed titles from a backup or calendar; returns added/skipped/errors |
| GET | `/api/v1/library/imports/catalog?kind=movie&query=title` | Up to 20 saved catalog title matches; offline read |
| POST | `/api/v1/library/import-files/{id}/register` | `{media_id,episode_ids:[],reviewed:true}`; queue verified in-place registration; 202 |
| POST | `/api/v1/library/import-files/{id}/skip` | `{}`; skip a review/blocked item |

Paths are immutable server snapshots selected through opaque discovered IDs;
requests never accept an arbitrary root or file path. `root` on scan creation
must exactly match one configured root for that library, and the response's
`roots` array records every root the scan walks. `PUT /library/roots` rejects
absolute paths, `..`, missing folders, symbolic links, duplicates and overlapping
roots; it never changes organization destination folders or naming templates.
Reads never scan. Several scans may be queued, but the worker leases one at a
time. File responses include
relative/full path, size, mtime snapshot, filename search hint, state/message and
nullable media/file IDs. Stable physical file identities remain internal.
The backup preview/apply routes read only the Movies or Series title list and
monitored flag; indexers, clients, credentials, paths, quality profiles and other
backup tables are never read. See [Sonarr/Radarr import](ARR_IMPORT.md).
Scans expose kind/folder/state/message, timestamps, discovery/registration/queued
counts and directory-completion examination/skip counts. Limits and exact
semantics are in [existing-library import](EXISTING_LIBRARY.md).

Manual registration requires explicit review and canonical catalog identities. TV
targets must be loaded, consecutive episodes of one season (maximum 20).
Source identity/size/mtime, bounded content observation, FFprobe, active download reservations,
duplicate paths/targets and current leases are checked before commit. There is
no client submission or filesystem publication. Automatic registration uses the
separate matching-job authorization described below. Existing
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
`rss`, `torznab`, `newznab`), `url`, `enabled`, `revision`, `user_agent`, `freeleech_only`,
optional write-only `cookie`, `api_key`, `feed_url`. Omitted secrets preserve saved
values; an explicit empty string clears a supported optional secret. Changed
origin/agent requires fresh associated secrets. Maximum twelve managed sources.
Status returns `has_cookie`, `has_api_key`, `has_feed`, safe homepage/endpoint,
revision and test/search timestamps/count/error; no credentials or feed passkeys.
Revision conflicts return 409. The legacy connection edits through its original
integration API; it supports testing/removal from this list.

Movie and TV release responses now add `warnings` for unavailable sources.
Each release may add `indexer_id`, `indexer`, `freeleech`, `pack` and `info_url`, the
provider's own details page when one was supplied. `info_url` is empty when the
source gave no page; it is never fabricated and the server never fetches it.
`pack` marks a season, range or complete-series release found by a season search;
packs are listed first.
Selections remain opaque, encrypted and expiring. Sources are rechecked at queue
time. Search errors from all sources fail the request; partial failures retain
healthy results.

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
not persistent monitoring; reports reset on restart. The administrator backup
manager uses opaque server-issued archive IDs, never client filesystem paths.

## Configuration and database backups

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/system/backups` | Archives, persistent schedule/status, storage directory, size limit, recovery state and restore result; no credentials; no-store |
| POST | `/api/v1/system/backups` | `{}`; queue a live manual snapshot; 202 started; 409 if another backup operation owns the manager |
| PUT | `/api/v1/system/backups/schedule` | `{enabled,interval_days,keep_count,keep_days,revision}`; compare revision and persist; returns current state |
| POST | `/api/v1/system/backups/upload` | Raw `application/zip`, up to 512 MiB; bounded extraction and full snapshot verification before 201 archive metadata |
| GET | `/api/v1/system/backups/{id}/download` | Private ZIP attachment; administrator only; no-store |
| DELETE | `/api/v1/system/backups/{id}` | Remove that managed archive; refuses conflicting operations and foreign file types |
| POST | `/api/v1/system/backups/{id}/preview` | `{}`; verify archive and return manifest, digest, movie/show/file counts and restored administrator |
| POST | `/api/v1/system/backups/{id}/restore` | `{sha256,confirmation:"RESTORE"}`; verify reviewed digest, prepare candidate and request controlled restart; 202 restarting |

Scheduling starts disabled; intervals are 1–30 days, count 1–100 and age 1–365
days. Retention covers scheduled archives and preserves the newest. Manual,
uploaded and pre-restore snapshots require explicit deletion. Status timestamps
are Unix seconds; archive manifests retain their original RFC3339 timestamp,
application version and schema. SHA-256 describes the complete stored ZIP.
Schedule running/last attempt/success/error survive restarts; interrupted jobs
report incomplete status. Invalid archives/settings use 400 `backup_refused`;
busy operations use 409 `backup_busy`; stale settings/digests use 409
`backup_changed`. Mutations during the restore restart use
503 `restore_restarting`. Upload/preview/restore can take longer than ordinary
requests. Restore drains active work and closes SQLite, saves a safety snapshot,
uses a crash-recoverable file-switch journal, revokes both session types and
restarts with workers held. It never changes media, host mounts or environment.
See [backup behavior and recovery limits](BACKUPS.md).

## Common behavior

All routes are same-origin. JSON uses snake_case. All `/api/v1` routes require
an administrator session by default. The exceptions are the public auth routes
below, the requester-scoped `/api/v1/requester/*` routes (bound to the resolved
request account), and the signature-verified `/api/v1/discord/interactions`
endpoint. Mutations
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
| POST | `/api/v1/library/monitoring/preview` | Read current monitoring/revisions for explicit kind/ids; authenticated + CSRF; 1–5000 distinct IDs, up to 1 MiB; no writes |
| PUT | `/api/v1/library/monitoring` | Apply desired parent monitoring to exact expected items atomically; stale selection returns 409; 1–5000 items, up to 1 MiB |
| POST | `/api/v1/library/profiles/preview` | Review explicit kind/ids and chosen profile/default, with current assignments and full target rules |
| PUT | `/api/v1/library/profiles` | Atomic assignment from expected item/profile/schedule revisions; stale review returns 409 |
| GET | `/api/v1/auth/session` | Public setup/auth status; authenticated response includes username, csrf_token, expires_at |
| POST | `/api/v1/auth/setup` | Public, rate-limited first admin creation; username, password, setup_token, optional import_mode (`hardlink` default or `copy`); returns 201 + cookie |
| POST | `/api/v1/auth/login` | Public, rate-limited username/password login; returns 200 + cookie |
| POST | `/api/v1/auth/logout` | Revoke current session and clear cookie; requires CSRF |
| GET | `/api/v1/providers/tmdb` | configured + updated_at + using_default/default_available; never the token |
| PUT | `/api/v1/providers/tmdb` | `{ "token": "..." }`; validate remotely, encrypt and save; failure preserves previous token |
| DELETE | `/api/v1/providers/tmdb` | Remove saved credential; retain all library metadata |
| POST | `/api/v1/providers/tmdb/test` | Test the saved credential against real TMDb |
| GET | `/api/v1/metadata/movies?query=title&page=1` | Real TMDb movie search; per-result in_library flag |
| GET | `/api/v1/metadata/movies/{tmdb_id}` | Canonical movie details from TMDb |
| GET | `/api/v1/metadata/people/{id}/credits` | Latest 24 cast roles for a TMDb person; `exclude_kind`/`exclude_tmdb_id` omit the title being viewed; each item adds `in_library` |
| POST | `/api/v1/library/movies` | Add canonical metadata by TMDb ID; omitted `monitored` defaults to future-only; 201 created or 200 existing |
| GET | `/api/v1/library/movies/{id}` | Saved movie by catalog ID, without a TMDb request |
| GET | `/api/v1/metadata/series?query=title&page=1` | TMDb TV search with per-result in_library |
| GET | `/api/v1/metadata/series/{tmdb_id}` | Canonical show details and season summaries |
| POST | `/api/v1/library/series` | Canonical show add by tmdb_id and optional `monitor` (`none`, `missing`, `current`, `future`); legacy `monitored` still works; omitted defaults to future-only; 201 new/200 existing |
| POST | `/api/v1/library/refresh` | `{ "kind": "movie" \| "series" \| "anime" }`; queues a full metadata refresh for every title in that library; 202 `{ "queued": n }` |
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
| GET | `/api/v1/downloads?page=1` | Activity queue: items, page, total, limit 24; completed jobs are excluded and remain readable through the direct job route |
| POST | `/api/v1/downloads` | `{ "release_id": "..." }`; 202 queued or 200 existing; item + created |
| POST | `/api/v1/library/movies/{id}/manual-torrent[?movie_version=additional]` | Raw `.torrent` body (≤4 MiB); registers the upload as this movie's selected release and queues it through the normal pipeline; 202 queued or 200 existing |
| POST | `/api/v1/library/series/{id}/manual-torrent?season=1[&episodes=2,3]` | Raw `.torrent` body; with no episodes the torrent covers the season's selected missing episodes; labeled packs import automatically, unmatched or ambiguous files keep the mapping review |
| GET | `/api/v1/downloads/{id}` | One durable job with progress/error fields, including a completed job no longer listed in the queue |
| POST | `/api/v1/downloads/{id}/retry` | `{}`; retry/resume blocked, failed or stopped tracking |
| POST | `/api/v1/downloads/{id}/stop` | `{}`; stop blocked/failed local tracking, leaving client/files untouched |
| POST | `/api/v1/downloads/{id}/file` | `{ "index": 3 }`; select a file from a choose_file job |
| GET | `/api/v1/torrents` | Built-in qBittorrent overview: availability, transfer rates/limits, server switches, state and bounded torrent list with `origin`/`job_id`; see [Torrents page API](#torrents-page-api) |
| POST | `/api/v1/torrents` | `{ "magnet": "magnet:?..." }` or a raw `.torrent` body (`application/x-bittorrent`, ≤8 MiB); adds a manual torrent under the built-in client |
| GET | `/api/v1/torrents/{hash}` | One torrent with files, properties, redacted trackers/web seeds, peers and downsampled pieces |
| POST | `/api/v1/torrents/{hash}/{action}` | `pause`, `resume`, `recheck`, or `remove` with `{ "reviewed": true, "delete_files": false }` |
| POST | `/api/v1/torrents/actions` | `{ "hashes": [...], "action": "...", ... }`; bulk action for 1–500 torrents; 204 |
| POST | `/api/v1/torrents/{hash}/files` | `{ "indexes": [0], "priority": 0 }`; file priorities of manual/external torrents; 204 |
| POST | `/api/v1/torrents/speed-limits` | `{ "alt_enabled"?, "download_limit"?, "upload_limit"? }`; global limits; 204 |
| GET/PUT | `/api/v1/usenet` | Bundled SABnzbd status + bounded queue; enable/disable with `{revision, enabled}` |
| GET/PUT | `/api/v1/usenet/news` | Safe news-server status; save encrypted host/port/TLS/username/password/connections |
| POST | `/api/v1/usenet/news/test` | `{}` or submitted host/port; short TCP reachability check only |
| GET/POST | `/api/v1/usenet/queue` | Queue list, or a raw `.nzb` body (`application/x-nzb`, ≤4 MiB) |
| POST | `/api/v1/usenet/queue/actions` | `{ "ids": [...], "action": "pause\|resume\|delete", "reviewed", "delete_files" }`; 204 |
| POST | `/api/v1/usenet/speed-limits` | `{ "download_limit": 0 }`; absolute bytes/s or 0 for no limit; 204 |
| GET | `/api/v1/library/series/{id}/seasons/{number}/releases?episode=1&query=title` | Search TV category 5000 for one missing episode; omit episode for selected missing episodes in a monitored season |
| POST | `/api/v1/downloads/{id}/mapping` | Confirm a TV job's completed file/episode mapping and explicit skips |
| GET/PUT | `/api/v1/automation` | Saved interval schedule, revision, next run, safe integration presence and state |
| GET/PUT | `/api/v1/automation/rss` | Feed-check preferences and status: enabled, interval, revision, next check, last run, checked/grabbed counts and the newest 25 grab/rejection rows |
| POST | `/api/v1/automation/rss/run` | `{}`; request one feed check, 202 or 409 paused/already requested/running |
| POST | `/api/v1/automation/run` | `{}`; request one durable run using saved mode, 202 or 409 already requested/running |
| POST | `/api/v1/automation/search-item` | `{ "media_id": 7, "episode_id"?: 10 }`; scope the next run to one title or episode; 202 |
| POST | `/api/v1/automation/search-bulk` | `{ "scope": "movie" \| "series" }`; scope the next run to every missing monitored movie or TV episode; 202 |
| GET | `/api/v1/automation/item-results?media_id=7&episode_id=10` | Latest recorded outcome, message, download ID and run for that target |
| PUT | `/api/v1/library/series/{id}/monitor-all` | `{ "revision" }`; monitor the show and regular episodes airing today or later in the server timezone, plus seasons with such an episode or a future season air_date; past/undated regular episodes are explicitly unmonitored; specials unchanged |
| POST | `/api/v1/library/monitor-all-episodes` | `{ "category"?: "standard" \| "anime" }`; same future-date rule for every show in scope; `{ "shows": n }` |
| POST | `/api/v1/library/series/{id}/episodes/{episodeID}/remove-file` | `{ "revision", "reviewed": true, "delete_file"?: true }`; remove one episode from the library and, when asked, delete its media copy (seeding sources and shared files are protected) |
| GET | `/api/v1/automation/runs` | `{items}`; latest 30 search runs with state, message, timestamps, mode, checked, matched and queued counts |
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

`register_unverified_video` (default false) lets existing-library discovery and
matching register files whose video ffprobe cannot inspect. Registered files
carry no resolution or embedded-title facts, their scan rows say "Added without
video verification", and embedded-title rewrites and subtitle matching refuse
them. Files ffprobe reads but finds no video stream in are never registered.

`import_mode` is `hardlink` (default) or `copy`. `hardlink` attempts a download
to library hardlink and falls back to a verified staged copy when the roots are
on different mounts, the download mount is read-only, or the link is otherwise
unsupported. `copy` always writes an independent library copy. Publication and
reorganization inside the media root still use private no-overwrite hardlinks;
symbolic-link imports are not supported.

Library responses contain `items`, `page`, `total`, `limit`, and
`collection_total`. Numbered page bounds are 1–100000; `page=all` returns every
matching item up to 5000 with `page: 1` and `limit: total`, preserving the same
shape for one-list rendering. More than 5000 matches returns 400
`library_too_large`; narrow the filters or request a numbered page. Alphabetical
sorts (title, title descending, and the title tie-break in year sorts) ignore a
leading case-insensitive `The ` without changing stored or displayed titles.

Manual torrent uploads treat the owner's file as the release selection: the
server parses the metainfo (name, size, v1/hybrid infohash), refuses torrents
that are already attached to a job or that use v2-only metadata, and queues the
exact bytes through the normal reservation, client submission, reconcile-by-hash
and verified import. The upload binds to the chosen movie, or to the chosen
season and episodes for a series; the per-file TV mapping review is still
required before registration. Selections expire like searched releases.

Search responses contain `items`, `page`, `total_pages` (capped at 500), and
`total_results`. Queries accept 1–200 characters without control characters;
search pages accept 1–500.

Add movie body: `{ "tmdb_id": 11, "monitored": true }`. When `monitored` is
omitted, the future-only default applies: the movie is monitored only when a
release date is today or later. The server fetches metadata; caller-supplied
titles, artwork or paths are rejected as unknown fields. Response is
`{ "created": true, "item": {...} }`.
The `(kind,tmdb_id)` uniqueness constraint prevents duplicate rows. Existing adds
return the original item without calling TMDb or changing monitoring preferences.
New movie adds save status `missing`; selecting a release is a separate action.
Queueing sets `downloading`; a verified import transaction sets `available`.

TV adds use the same canonical/idempotent add contract and store `kind: series`.
The add body may also send `"monitor": "none|missing|current|future"` to apply
the one-time monitoring choice from the add dropdown. `none` pauses the show,
`missing` monitors regular seasons that have started and their aired episodes,
`current` monitors only the current (or next) regular season, and `future`
monitors the current and later seasons with episodes releasing today or later.
`monitored` remains supported for older clients; when both are present `monitor`
wins. Unknown modes return 400 `invalid_monitoring`. The choice adjusts only
monitoring; it does not load episodes, queue downloads or alter files.
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
seeders/peers (nullable), published_at and info_url. published_at is RFC3339 UTC
when the provider supplied a parsable date and `""` when it did not; the UI shows
a dash for unknown. info_url is the provider's details page or `""`. Selection
expiry is 30 minutes; searches/settings changes invalidate unqueued older
selections. No download URLs or magnets appear in responses. Queue payloads
accept a release ID only. Activity exposes state, progress, bytes, client_state,
attempts, safe error fields, file choices and timestamps; encrypted submission
payloads/credentials are omitted. States are queued, submitting, downloading,
importing, blocked, failed, completed and cancelled. A job that completes after
its import records one `download` activity event with state `completed` for the
Timeline; repeated reconciliation never duplicates it. See MOVIE_WORKFLOW.md for
leases, retry and import semantics.

Movie DTOs include tmdb_id, title, overview, year (nullable), release_date,
theatrical_release_date, digital_release_date, physical_release_date,
poster_path, poster_url, and runtime_minutes (nullable). The three detailed
dates are populated by the TMDb movie refresh from `release_dates`; release_date
remains the canonical primary date for compatibility. Saved library items add
id, kind, status and monitored. Poster URLs are restricted to TMDb's official
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
`revision`. New search schedules are paused/search-only. The separate release
feed check starts enabled every 15 minutes by owner decision; its PUT accepts
`enabled`, `interval_minutes` (5–1440) and `revision`. Feed matches against
monitored missing titles are grabbed automatically regardless of the search
mode, while manual release searches stay reviewable. GET additionally returns
next_run (Unix seconds), requested, state, message, indexer_connected and
client_connected. client_connected counts a usable enabled per-protocol default,
the legacy settings connection, the enabled built-in qBittorrent fallback, or the
enabled built-in SABnzbd client with a saved news server; it is not merely a
saved settings row. No enabled indexer is waiting_indexer; saving a schedule never
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

Reviewed replacement endpoints: `POST /organization/replacements` accepts
`media_file_id` and `scan_file_id`, returning an organization review. The
existing `POST /organization/plans/{id}/apply` queues the reviewed job.
`POST /organization/plans/{id}/rollback` creates a review that restores a
completed replacement's prior version; apply that review separately.
`POST /organization/plans/{id}/recover-replacement` queues recovery of an
interrupted job's retained original when the registered path is empty. All
require authenticated administrator sessions and CSRF protection. Source paths
are derived from discovered IDs rather than accepted from clients.

`PUT /library/series/{id}/seasons/{number}/episodes/monitor` accepts the
current show `revision`, a `monitored` boolean and 1–100 distinct explicit
`episode_ids` belonging to that loaded season. The update is atomic and uses
the same show revision fence as individual episode edits; it does not move or
delete media files.

`PUT /library/series/{id}/seasons/monitor` accepts the current show `revision`,
a `monitored` boolean and 1–100 distinct `season_ids` belonging to that show.
It updates the selected seasons atomically without changing their episode choices.

`POST /organization/files/{id}/health` with `{}` checks one registered file's
saved size and SHA-256, without changing the file. A fast registration first
establishes its deferred full checksum from an unchanged observation. Results are `verified`, `missing`,
`changed`, or `unsafe`. A check reads the full file and is cancellable; only
one such check runs at a time.

`POST /library/files/{id}/inspect` with `{}` verifies the registered video's
saved checksum (or bounded observation for a fast registration), runs FFprobe
and updates its saved stream facts. The response
includes bounded `audio_streams` with codec, embedded language tag and title.
Existing registrations made before audio inspection can use this operation; the
check reads the whole file. Tags describe file metadata and do not prove the
spoken language. A changed file is rejected without updating its saved facts.

`POST /organization/plans/{id}/cancel` with `{}` returns a queued job to
review only if no file work has started. It refuses journaled outputs or a
folder that may already have moved; those jobs must follow recovery instead.

`embedded_title` in the inspected file's probe is its container title tag; null
means it has not been inspected with this version and an empty string means no
title was reported. Inspection does not edit the file or rewrite tags. A separate reviewed
embedded-title update is available; selected-file, whole-title and whole-folder
organization reviews can request it with `update_embedded_title: true`. Updates
run sequentially after the move. Each plan records the previous title, previous
size/checksum and the new size/checksum; no duplicate video copy is retained.

`POST /organization/embedded-title` with `media_file_id` creates a review of a
catalog-derived movie or episode title for a registered MP4, M4V, Matroska, MOV, AVI or WebM video.
Every rewrite must preserve packet timing and non-title metadata; unsupported
codec combinations fail safely. TS/M2TS title tags remain unsupported.

Applying the plan writes a verified independent stage on the video's own
filesystem, checks stream metadata, packet data and timestamps, chapters and
existing container tags, then atomically replaces the original path. The stage
temporarily needs free space equal to the video; the rename consumes it, so it is
not retained. The original is untouched until the verified stage is published,
and a torrent seeding hardlink continues to reference the old bytes.
No prior video copy is retained: `previous_title`, `before` and `after` in the
plan snapshot are the durable change record, and metadata-only plans refuse
`rollback`. Unsupported or changed streams fail without replacing the original.
YouTube-origin provenance is retained and updated to the replacement checksum.

POST `/api/v1/organization/plans/{id}/rollback` with `{}` accepts completed
file/title/folder moves and media replacements. Metadata-only embedded-title
plans keep no video copy and refuse rollback; review a new title update instead.
The endpoint returns a 201 review plan with `restores_plan` and
inverse paths, without moving files. Apply requires the normal explicit review.
Current collection and naming preferences remain unchanged for future imports.
Changed registered versions/mappings and occupied original paths are rejected;
whole-folder reviews include current extras and use the existing copy journal.

### Acquired replacements and catalog removal (schema 30)

POST `/downloads` accepts optional positive `replacement_file_id` with a manual
release selection. Normal and automatic downloads still refuse registered files.
TV replacement searches accept `replacement_file_id`; the selected file must
belong to that show/season. Reservation binds its current SHA and exact episode
set. A shared video requires one reviewed completed file covering every episode;
skipping episodes or splitting that replacement is rejected. Both qBittorrent
and SABnzbd retain the existing complete-output and source-containment gates.

Verified downloads pause as `blocked`/`replacement_review` with their import journal
published but uncommitted. POST `/organization/acquisition` with `{kind,job_id}`
(`download` or `youtube`) creates/reuses an immutable replacement review. Apply
uses the existing organization journal, independent copies, retained versions,
no-overwrite checks and rollback. Completion commits source provenance, download
history and episode associations atomically. Different video extensions use a
new destination extension and refuse collisions; rollback restores the old one.

YouTube acquisition for an existing catalog target is labeled as replacement in
its initial review. The first import publishes a retained candidate and selected
subtitles, then returns to `awaiting_review`. A separate organization review is
mandatory before replacing video. Selected VTT copies have source-specific paths;
existing sidecars and both versions remain. Rollback restores video provenance
and retains subtitle bytes without claiming timing synchronization.

POST `/organization/removal` with `{media_id,mode}` (`keep` or `trash`) returns a
201 review containing title, registered paths and hashed dependent-record
snapshots. It accepts at most 100 files and 10,000 dependent rows. Apply rechecks
the complete dependency closure and refuses pending media work, scans and active
automation. `keep` removes catalog rows/history only; `trash` journals moves of
registered files and recognized subtitle companions under `.reelscarey-trash`.
Unregistered extras, client sources and retained versions are preserved. No
permanent file-purge endpoint exists. Partial work retains verified outputs;
failed cleanup remains fenced for recovery. Removed titles must be re-added
and their retained media explicitly registered if desired.

Requester list creation accepts providers `tmdb_private`/`trakt_private` with
write-only `access_token`; Trakt also needs `client_id` and owner/list slug. Tokens
are encrypted per list, bound to the resolved account, sent only to fixed provider
hosts and omitted from responses/events. Expired tokens fail with a visible
generic error. Public providers and existing credential formats remain compatible.

## Version 0.22 additions

All routes below require an administrator. Mutations require the existing CSRF
header and same-origin checks. Domain reviews expire after 30 minutes and stale
snapshots return conflicts. See [behavior and limits](SEVEN_FEATURES.md).

| Route | Methods | Contract |
| --- | --- | --- |
| `/system/metadata-refresh` | GET, PUT | Schedule fields: enabled, interval_hours, batch_size, only_monitored, revision. |
| `/system/metadata-refresh/run` | POST | Empty object requests one bounded pass; busy requests conflict. |
| `/library/refresh` | POST | `{ "kind": "movie" \| "series" \| "anime" }` queues a full pass for that library independently of the schedule; 202 with queued count. |
| `/library/items/{id}/anime-order/refresh` | POST | Current revision creates a fresh-facts order review; existing apply route commits it. |
| `/library/series/{id}/subtitles` | GET, PUT, DELETE | Show preferences with override revision and global_revision; DELETE returns to inheritance. |
| `/subtitles/files/{id}/edits` | GET, POST | History or review: language, variant, action offset/replace/rollback plus offset_ms/content/previous_edit_id. |
| `/subtitles/edits/{id}/{apply\|retry}` | POST | Reviewed apply queues the journaled edit; retry resumes a blocked operation. |
| `/youtube/bulk/review` | POST | source_url, video_ids, target, subtitles; returns individual immutable reviews. |
| `/youtube/bulk/queue` | POST | review_ids and reviewed=true; atomically queues at most 25. |
| `/youtube/subscriptions` | GET, POST, PUT | Paused creation or revision-checked updates; acquire mode requires initialized baseline and reviewed=true. |
| `/youtube/subscriptions/{id}` | GET, POST, DELETE | Entry history, revision-checked check request, or subscription deletion. |
| `/library/imports/{id}/matches/suggest` | POST | file_ids and optional include_provider; bounded suggestions only. |
| `/library/imports/{id}/matches/review` | POST | Explicit items: file_id, media_id, episode_ids; returns review_id. |
| `/library/imports/{id}/matches/apply` | POST | review_id and reviewed=true atomically queue verified in-place registration. |

Quality profile JSON adds `scoring` with `sources`, `codecs`, `preferred_terms`,
`max_usenet_age_days` and `min_usenet_age_minutes`. Existing profile revisions
fence these rules. Calendar entries add `runtime_minutes`, `series_status` and
`metadata_updated_at`; reads do not initiate refreshes.
## Version 0.23 storage routes

`GET /system/storage?root=media&path=Movies&page=1` lists a bounded mounted folder.
Only root/path/page query keys are supported. The reply contains roots, selected
root/path, items (name/path/kind/size_bytes/modified_at), total/page/limit. Kinds are
file, directory or blocked; file contents and mutations are unavailable. Hidden,
linked, protected and escaped paths are refused. The maximum is 10,000 entries,
100 per page; unavailable/invalid folders return 400 and excessive folders 409.

`GET /system/storage/migration` returns the saved offline migration summary:
state, digest, source/destination, file/directory counts, bytes, safety backup and
temporary-copy folder. A missing plan returns state=none; an invalid plan returns
409. Both routes require an administrator; requester accounts are denied. There
is no live migration mutation API. Offline commands and bounds are documented in
[STORAGE.md](STORAGE.md).
## Named download clients (0.24)

All routes require an administrator; writes require CSRF and same origin.
Credentials are never returned. Save/test have bounded connection timeouts.

- `GET /api/v1/download-clients` returns `{items,defaults,downloads_root}`. Items
  include ID, name, kind, enabled, revision, URL, username, remote `download_path`,
  relative `local_folder` and count of unfinished jobs. Defaults contain torrent
  and Usenet client IDs (empty means the existing Settings connection), revision.
- `POST /api/v1/download-clients` tests and creates a paused connection. Supply
  name, kind (`qbittorrent`, `transmission`, `sabnzbd`, `nzbget`), URL, completed download_path,
  optional local_folder, username/password or SABnzbd api_key. Creation revision
  is zero and enabled must be false. A blank `download_path` for SABnzbd reads
  and saves its reported absolute completed folder. Other named clients need
  an explicit absolute path.
- `PUT /api/v1/download-clients/{id}` tests and saves with the current revision;
  kind is immutable. Omitted secrets retain saved values. Changing URL/username
  requires entering the secret again. Enabling is explicit; default clients
  cannot be paused, and unfinished jobs protect address/folder changes.
- `PUT /api/v1/download-clients/defaults` compares revision and validates enabled
  clients of the matching protocol. Empty explicitly chooses Settings.
- `POST /api/v1/download-clients/{id}/test` accepts `{}` and tests saved settings.
- `DELETE /api/v1/download-clients/{id}` requires `{revision,reviewed:true}` and
  refuses default clients or clients with unfinished jobs (including stopped).
  Completed history remains. It never deletes external jobs or source files.
- - `GET /api/v1/vpn/mullvad` reports the opt-in tunnel:
  `{present,enabled,state,message?,interface,address?,endpoint?,revision}` with
  state `up`, `starting`, `stopped` or `failed`. The private key is never
  returned. `PUT` with `{revision,enabled?,private_key?,address,peer_key,endpoint,allowed_ips?}`
  saves the encrypted configuration; enabling awaits the interface and reverts
  to disabled when it does not come up. `address` accepts one or more
  comma-separated CIDRs (Mullvad supplies IPv4 and IPv6); `allowed_ips` is
  optional. The built-in torrent client binds to the tunnel and stops while it
  is down.

`GET /api/v1/download-clients/builtin` reports the app-supervised built-in
  qBittorrent daemon: `{present,enabled,state,message?,revision,peer_port,active_jobs,name}`;
  state is `running`, `starting`, `stopped` or `failed` with its honest message.
  `PUT` with `{revision,enabled?,peer_port?}` enables it (the daemon starts, its
  loopback API is awaited and tested before the connection is enabled),
  disables it (refused while jobs are active or it is the torrent default), or
  changes the peer port (the daemon restarts briefly and resumes from
  fastresume). The generic save/delete endpoints refuse the built-in
  connection; see [Download clients](DOWNLOAD_CLIENTS.md) for the writable
  download folder and peer-port requirements.

The bundled Usenet client is a `builtin:true` row with the fixed id
`6f1c2b3a4d5e4f60718293a4b5c6d7e8` and kind `sabnzbd`. It starts paused and only
appears after a news server is saved. It is selectable as the Usenet default and
is managed by the `/api/v1/usenet` routes above; generic save/delete endpoints
refuse it. Enabling it never changes the Usenet default automatically, and
existing jobs keep their bound client. See
[Usenet](USENET.md#bundled-sabnzbd).

Download rows additionally expose client_key, client_kind and client_label.
Each new job stores its original remote root and local folder privately. See
[Download clients](DOWNLOAD_CLIENTS.md) for mounting and Usenet category setup.

## Subtitle output format and expanded episode labels (0.25)

`GET/PUT /api/v1/subtitles/preferences` now includes `download_format`: `srt`
(default, including omitted legacy writes) or `original`. Original format permits
SRT, VTT, ASS and SSA provider files. Candidate responses expose `format` for the
output extension. Format changes carry the existing preference revision and
invalidate unused reviews. Already journaled imports retain their destination
and recover without another provider request. Existing CSRF, administrator,
provider-generation, media-hash and lease checks apply.

Standard TV identity labels additionally accept S01EP02, S01 Episode 02 and
Season 1 Ep 2. File-review suggestions accept bounded consecutive spaced and
long-form ranges/chains. These hints never authorize import or turn packs into
single-episode automatic downloads. Cross-season single-file mappings and
inferred date/absolute numbering remain unsupported.

## Additional providers and alignment (0.26)

All existing administrator, CSRF, same-origin and revision checks still apply.

- Named download clients accept `kind=transmission` (torrent protocol), using the
  same URL, username/password, download_path and local_folder fields. Only
  Transmission 4 with compatible RPC and scripts disabled passes connection tests.
- `GET /api/v1/metadata/anime?query=...&provider=kitsu` selects Kitsu; omitted
  provider means `anilist`. Unknown or repeated provider parameters are rejected.
  Anime-order preview accepts `provider` and provider_ids; returned settings
  include `provider`. All identities in a saved set use that provider. Refresh
  dispatches by saved identity provider and never applies changed facts directly.
- The compatible `/api/v1/providers/opensubtitles` route now manages the selected
  subtitle connection. PUT accepts `provider=opensubtitles|subdl`, defaulting to
  OpenSubtitles for legacy callers. SubDL requires api_key; username/password and
  user_agent are cleared. Changing provider requires a new key. Status includes
  `provider`; no secrets are returned. The existing `/test` and DELETE apply to
  the selected connection. SubDL candidates include provider, download_url and
  require_review; the URL is public and is not a credential.
- Saved subtitle tracks include `provider`; unknown/local tracks use an empty
  value. Existing downloaded records migrate to `opensubtitles`. Import journals
  retain the original provider through a later connection change.
- `POST /api/v1/subtitles/files/{id}/edits` accepts `action=align`, existing track
  fields, `content` containing the reference text and
  `reference_format=srt|vtt|ass|ssa`. offset_ms must be zero. The returned review
  message includes match count, estimated starting offset and timing scale.
  Existing edit apply/retry/rollback routes and review expiration are unchanged.

### Automatic existing-library matching (0.27)

`POST /api/v1/library/imports/{id}/matches/automatic` accepts only
`{"action":"start"}` or `{"action":"stop"}` and returns 202 with matching status.
Start requires completed discovery; stop prevents further matching writes but
queued file verification continues. Administrator authentication, CSRF and
same-origin checks apply. GET never starts a job. Scan objects optionally contain
`automatic_matching:{state,phase,message,resolved,queued,review}`. States are
pending/running/waiting/completed/cancelled/paused; phases are resolve/queue.
A completed matching job may still have file verification outstanding. Manual
registration and bulk apply continue to require explicit reviewed consent.
See [matching rules and recovery](EXISTING_LIBRARY.md).

### Fast existing-library registration (0.27.5)

Scan `matched` counts resolved files waiting for the duplicate check/attachment;
`needs_match` excludes those files. `queued` counts files waiting for or undergoing
inspection; `registered` counts attached files. The `matched` file filter and
projected file state distinguish these rows without changing the durable review
state. Automatic matching status `queued` remains cumulative, including files
already attached. No GET starts work. Folder identity is primary; explicit
provider/season conflicts and overlapping episode files remain review exceptions.

Schema 043 preserves bounded file observations separately from SHA-256. A large
existing file has an empty full checksum until an explicit operation requires it;
the sample digest is never represented as a full-file SHA. See
[registration and integrity guarantees](EXISTING_LIBRARY.md#verification-and-registration).

### Multiple movie versions (0.28)

- `GET /api/v1/library/movies/{id}/versions` returns `enabled`, `hd_profile_id`,
  `uhd_profile_id`, `revision`, `hd_files`, `uhd_files`, `hd_download` and
  `uhd_download`. Download values are the current job state or an empty string.
- `PUT` on that route accepts the first four fields. Revision 0 creates the
  movie's initially disabled policy; stale revisions return 409. Enabling requires
  an HD profile cutoff up to 1080p and a 4K cutoff of 2160p. Saving never starts
  work or enables monitoring/schedules. Only movie IDs are accepted.
- `POST /api/v1/downloads` optionally accepts `movie_version` as `hd`, `uhd` or
  `additional`. The managed targets require an enabled movie policy and a matching
  release resolution label. Additional is an explicit extra-copy request. This
  field cannot be combined with `replacement_file_id`. Default callers retain
  the original behavior that protects existing movie files.
- Download jobs expose `movie_version`; file details expose `version_label`.
  Wanted/Calendar entries expose optional `movie_version` for enabled dual targets.
  Use kind, media/episode ID and movie_version together as a planning row key.

All routes retain administrator authentication, same-origin and CSRF checks.
Files remain distinct by file ID/path, and replacements require a specific file.
See [behavior, recovery and limits](MOVIE_VERSIONS.md).

## IRC announce feeds and IPTorrents automatic grabbing (0.34)

- `GET /api/v1/announce-sources` returns `{items,definitions}`. Items expose
  `id`, `tracker`, `name`, `enabled`, `revision`, `auto_grab`, `nick`,
  `account`, `server`, `port`, `tls`, `channel`, `require_account`,
  `has_password`, `has_key`, `key_label`, `skip_tls_verify`, `last_error` and
  `last_event_at`; passwords and keys are never returned.
- `POST /api/v1/announce-sources` with
  `{tracker,name,nick,account,password,invite,key,enabled,auto_grab,skip_tls_verify}`
  creates a source. `PUT /api/v1/announce-sources/{id}` requires the current
  `revision` and returns `409 revision_conflict` when it is stale. A blank
  `password`, `invite` or `key` retains the saved value; omitting
  `skip_tls_verify` or `auto_grab` keeps the saved value, so a pause/resume
  update that sends only `enabled` preserves both. `skip_tls_verify` (default
  false) connects to a tracker whose IRC certificate is expired or untrusted;
  the link stays encrypted but the server is not authenticated.
- `auto_grab` is the per-source automatic-grabbing opt-in. It defaults to
  **false**, is supported **only for IPTorrents**, and is refused for any other
  tracker (`invalid_settings`). Enabling it requires a saved `key` and is
  otherwise rejected with `Save a tracker passkey before enabling automatic
  grabbing.` The `key` is the tracker download credential (IPTorrents passkey,
  TorrentLeech/BeyondHD RSS key, FileList/AnimeBytes passkey, MyAnonamouse
  session cookie); it is stored encrypted with the application key, is never
  returned by the API, and is used only to build the IPTorrents download URL.
  Leaving it blank on edit keeps the saved key.
- Every save writes a new `revision`. Capture fences on the captured revision:
  a source whose settings change reconnects at the new revision, and announces
  captured under an older revision are stored as `capture_only`. Toggling
  pause/resume also writes a revision but does not change `auto_grab`.
- `DELETE /api/v1/announce-sources/{id}` requires `{revision}` and removes the
  encrypted settings and captured events.
- `GET /api/v1/announces?page=1` returns `{items,page,total,limit}` with the
  newest 50 parsed events. Each event exposes `id`, `source_id`, `tracker`,
  `release_name`, `category`, `size_bytes`, `freeleech`, `torrent_id`,
  `info_url`, `received_at`, `outcome`, `message`, `attempts`, `processed_at`
  and `download_id`. Raw IRC lines are not stored. `outcome` is one of
  `capture_only` (the tracker, source or release was not eligible),
  `unmatched` (no monitored missing movie or ordinary single episode),
  `ambiguous` (matches more than one saved title), `rejected` (no candidate
  satisfies the profile), `queued`, `already_tracked`, `needs_attention`,
  `retry`, or `stale_policy`/`stale_source` when the saved decision changed
  before the grab; work still awaiting processing reports an empty `outcome`.
- Automatic grabbing matches only an **exact monitored missing movie** or a
  **single ordinary TV episode** (positive season and episode). Season packs,
  multi-episode ranges, specials (season zero), Anime, titles that already have
  a registered file and ambiguous titles stay capture-only for review. Announce
  lines carry no seeder count, so a missing count satisfies the profile's
  minimum seeder count for the announce; the minimum still applies to scheduled
  searches, which report a real count. See
  [announce feeds](ANNOUNCE.md).
- Supported definitions: IPTorrents, TorrentLeech, FileList, BeyondHD,
  AnimeBytes and MyAnonamouse. IPTorrents needs no NickServ account and no
  invite command — only a unique nick (and optionally its details-page
  passkey). The source starts paused unless `enabled` is true. Automatic
  grabbing is implemented only for IPTorrents; the other five trackers remain
  capture-only and an opt-in for them is refused.

## Torrents page API

All `/api/v1/torrents` routes are administrator-only (request accounts receive
`403 administrator_required`), require an authenticated session, and every
`POST` requires the CSRF header and same-origin validation. They manage only the
supervised built-in qBittorrent (4.5.x, Web API 2.8.x); when it is not enabled,
mutations return `409 builtin_unavailable` after input validation. The native
qBittorrent Web UI is not proxied. Tracker and web seed URLs are always reduced
to `scheme://host[:port]` (pseudo entries such as `** [DHT] **` are unchanged,
unparseable values become `(hidden)`), and `magnet_uri` is never returned.
See [Torrents](TORRENTS.md) for the page and its intentional limits.

`GET /api/v1/torrents`:

```json
{
  "available": true, "state": "running|disconnected|disabled", "message": "",
  "transfer": { "dl_info_speed": 0, "up_info_speed": 0, "dl_info_data": 0,
    "up_info_data": 0, "connection_status": "connected|firewalled|disconnected",
    "dht_nodes": 0, "dl_rate_limit": 0, "up_rate_limit": 0 },
  "server": { "alt_speed_enabled": false, "queueing_enabled": true,
    "alt_dl_limit": 0, "alt_up_limit": 0 },
  "items": [{ "hash": "", "name": "", "state": "", "save_path": "", "progress": 0,
    "amount_left": 0, "total_size": 0, "completed": 0, "tags": "", "category": "",
    "downloaded": 0, "uploaded": 0, "dlspeed": 0, "upspeed": 0, "eta": 0,
    "ratio": 0, "num_seeds": 0, "num_leechs": 0, "added_on": 0,
    "completion_on": 0, "content_path": "", "priority": 0, "num_complete": 0,
    "num_incomplete": 0, "dl_limit": 0, "up_limit": 0, "seq_dl": false,
    "f_l_piece_prio": false, "force_start": false, "super_seeding": false,
    "auto_tmm": false, "time_active": 0, "seeding_time": 0, "last_activity": 0,
    "availability": 0, "ratio_limit": -2, "seeding_time_limit": -2,
    "downloaded_session": 0, "uploaded_session": 0,
    "tracker": "https://tracker.example", "origin": "job",
    "job_id": "0123456789abcdef0123456789abcdef" }]
}
```

`server` is `null` while the built-in client is unavailable. Overview limits are
bytes per second and `0` means unlimited; `dl_rate_limit`/`up_rate_limit` are
the currently effective global limits (the alternative limits while
`alt_speed_enabled` is true). Per-torrent `dl_limit`/`up_limit`, `priority`,
`ratio_limit` and `seeding_time_limit` are qBittorrent's values unchanged
(limits ≤0 are unlimited, queue `priority` is 0 when not queued, share limits
use -2 for global and -1 for unlimited). Only `queueing_enabled`, `alt_dl_limit`
and `alt_up_limit` are read from the client preferences (qBittorrent 4.5
reports them in bytes per second despite its documentation saying KiB/s). `origin` is derived from the tags: `reelscarey-<job id>`
is `job` (with `job_id`), `reelscarey-manual` is `manual`, and anything else is
`external`; `job_id` is omitted for the other origins.

`GET /api/v1/torrents/{hash}` returns `{ torrent, files, properties, trackers,
peers, webseeds, pieces }`: `torrent` has the list item shape; `files` are
`{index, name, size, progress, priority, availability, is_seed}`; `properties`
are `{save_path, creation_date, piece_size, comment, total_wasted,
total_uploaded, total_downloaded, up_limit, dl_limit, time_elapsed,
seeding_time, nb_connections, nb_connections_limit, share_ratio, addition_date,
completion_date, created_by, dl_speed_avg, dl_speed, up_speed_avg, up_speed,
eta, last_seen, peers, peers_total, seeds, seeds_total, pieces_have, pieces_num,
reannounce, total_size}` as reported by qBittorrent (integers; `share_ratio` is
a number; `comment` ≤2000 characters); `trackers` are `{url, status, tier,
num_peers, num_seeds, num_leeches, num_downloaded, msg}` (`tier` is -1 for
pseudo entries, `msg` ≤500 characters with URLs redacted); `peers` are `{ip,
port, client, country_code, connection, flags, progress, dl_speed, up_speed,
downloaded, uploaded, relevance}`, ordered by combined rate then address and
capped at 2000; `webseeds` are `{url}`; `pieces` holds at most 512 buckets of
near-equal size where `2` means every piece in the bucket is complete, `0`
means every piece is missing, and `1` means any other mix (downloading or
partially complete). Invalid or oversized client lists (more than 10000
files/trackers/web seeds or 1,000,000 piece states) fail with
`502 client_response`; a torrent removed meanwhile returns `404 not_found`.

`POST /api/v1/torrents/actions` (body ≤128 KiB, 204 on success):

```json
{ "hashes": ["<40 hex>"], "action": "set_share_limits",
  "enabled": true, "limit": 0, "ratio_limit": -2, "seeding_time_limit": -2,
  "delete_files": false, "reviewed": true }
```

`hashes` contains 1–500 info hashes (case-insensitive, deduplicated). Actions:
`pause`, `resume`, `recheck`, `reannounce`, `queue_top`, `queue_bottom`,
`queue_up`, `queue_down`; `force_start`, `sequential`, `first_last_piece` and
`super_seeding` require `enabled`; `set_download_limit`/`set_upload_limit`
require `limit` (0–2000000000 bytes/s, 0 = unlimited); `set_share_limits`
requires `ratio_limit` (-2 global, -1 unlimited, or 0–9998) and
`seeding_time_limit` in minutes (-2, -1 or 0–525600); `remove` requires
`reviewed: true` (else `400 review_required`) and honours `delete_files`.
Parameters an action does not use are ignored. `sequential` and
`first_last_piece` change only torrents whose current state differs from
`enabled` (`404 not_found` if a selected torrent is missing). Queue actions
return `409 queueing_disabled` when queueing is off in the client. Unknown
actions or invalid parameters return `400 invalid_selection`.

`POST /api/v1/torrents/{hash}/files` with `{ "indexes": [0, 2], "priority": 0 }`
(0 do not download, 1 normal, 6 high, 7 maximum; 1–10000 unique indexes that
exist in the torrent; body ≤128 KiB) returns 204. Torrents with origin `job` are
refused with `409 managed_torrent`; `409 file_priority_rejected` means
qBittorrent has not loaded the metadata yet or a file no longer exists.

`POST /api/v1/torrents/speed-limits` with any of `{ "alt_enabled": true,
"download_limit": 0, "upload_limit": 0 }` (at least one) returns 204. The
alternative mode is toggled only when it differs from `alt_enabled`; the limits
(bytes/s, 0 = unlimited, ≤2000000000) then apply to whichever mode is active
afterwards, as qBittorrent's own global limit endpoints do.

`GET /api/v1/torrents/share-policy` returns the saved automatic seeding limit:

```json
{ "enabled": false, "action": "pause|remove|remove_and_delete",
  "max_ratio": 2.0, "max_seeding_minutes": 0,
  "include_reelscarey": false, "revision": 1, "updated_at": 1700000000 }
```

`max_ratio` accepts 0 (disabled) or 0.1–1000; `max_seeding_minutes` accepts
0 (disabled) or 0–525600; an enabled policy needs at least one rule.
`updated_at` is a Unix timestamp. `PUT` with the same shape (CSRF and
same-origin required) replaces the policy and returns the saved row; `revision`
must match or the call returns `409 revision_conflict`. Invalid actions, bounds
or an enabled policy without a rule return `400 invalid_selection`.

`POST /api/v1/torrents/share-policy/run` evaluates the saved policy once and
returns `{ "checked": 0, "acted": 0 }`. `checked` counts completed torrents
evaluated against the rules; `acted` counts applied actions. A disabled policy
returns zeros. When the policy is enabled but the built-in client is not
configured or enabled, the call returns `409 builtin_unavailable`. The
supervised pass that runs at most once a minute never reports that as an error.
See [Torrents](TORRENTS.md#automatic-seeding-limit) for the safety rules,
including the job-torrent skip and the no-delete guarantee.

`GET /api/v1/torrents/public-pause` returns the auto-pause option for finished
public-source torrents:

```json
{ "enabled": false, "revision": 1, "updated_at": 1700000000 }
```

`PUT` with the same shape (CSRF and same-origin required) replaces the option
and returns the saved row; `revision` must match or the call returns
`409 revision_conflict`. See [Torrents](TORRENTS.md#auto-pause-public-trackers)
for what counts as a public source and how the supervised pass behaves.

## Usenet page API (bundled SABnzbd)

All `/api/v1/usenet` routes are administrator-only; request accounts receive
`403 administrator_required`. Every write requires CSRF and same-origin
validation. They manage only the application-supervised SABnzbd daemon: the API
key and NNTP password are never returned and completed output is never deleted
by these routes. External SABnzbd/NZBGet connections keep their own job-level
controls. See [Usenet](USENET.md#bundled-sabnzbd) for setup and limits.

`GET /api/v1/usenet` returns `{status, queue}`:

```json
{
  "status": { "present": true, "installed": true, "enabled": true,
    "configured": true, "state": "running|starting|stopped|failed",
    "message": "", "revision": 3, "active_jobs": 0, "name": "Built-in SABnzbd",
    "host": "news.example.com", "username": "fixture", "port": 563,
    "tls": true, "connections": 8 },
  "queue": { "available": true, "state": "Downloading", "message": "",
    "paused": false, "speed": 0, "remaining": 0, "eta": "0:10:00",
    "limit_bytes": 0, "limit_percent": 100,
    "items": [{ "id": "SABnzbd_nzo_x", "name": "", "size": 0,
      "remaining": 0, "progress": 0.25, "status": "Downloading",
      "speed": 0, "eta": "0:05:00", "category": "reelscarey",
      "priority": "Normal" }] }
}
```

`present` means a paused or enabled `builtin_usenet` row exists; `installed`
means the binary is present; `configured` means a valid news server is saved;
`state` is the supervised process state. `queue.available` is false with an
honest message when the daemon is disabled, unconfigured, not installed or not
answering; that case is still `200`, not an error. `queue.items` is bounded to
1,000 jobs. SABnzbd downloads one job at a time, so only the active item carries
a non-zero `speed`. Progress is a 0–1 fraction; sizes are bytes.

`PUT /api/v1/usenet` with `{ "revision": 3, "enabled": true|false }` starts or
stops the daemon. Enabling requires a saved news server and waits for its
loopback API before returning; disabling refuses while tracked jobs are active
(`409 active_downloads`) and clears this client's Usenet default. A stale
revision returns `409 revision_conflict`. The response is the status object.
Turning it on never makes it the Usenet default; use
`PUT /api/v1/download-clients/defaults`.

`GET /api/v1/usenet/news` returns
`{configured, host, username, port, tls, connections}` (no password).
`PUT /api/v1/usenet/news` accepts
`{host, port, tls, username, password?, connections}`. Host/port/connections are
validated; password is omitted to retain the saved value and is required again
when the host or username changes. Saving creates the paused `builtin_usenet`
row on first use and does not contact the provider. The response is the status
object.

`POST /api/v1/usenet/news/test` with `{}` tests the saved provider, or with
`{host, port, ...}` tests submitted values. It is a short TCP reachability check
only; a missing server returns `502 upstream_unavailable`, never a crash.

`GET /api/v1/usenet/queue` returns the queue object alone.

`POST /api/v1/usenet/queue` requires `Content-Type: application/x-nzb` and an
NZB body ≤4 MiB; it returns `202 {"added":true,"id":"SABnzbd_nzo_..."}`. The
server never fetches a caller-supplied URL, uses category `reelscarey`, and
does not link the upload to a library job or import it. Invalid descriptors
return `400 unsupported_nzb`.

`POST /api/v1/usenet/queue/actions` (body ≤128 KiB, 204 on success):

```json
{ "ids": ["SABnzbd_nzo_x"], "action": "pause|resume|delete",
  "delete_files": false, "reviewed": true }
```

`ids` contains 1–500 SABnzbd ids (deduplicated). Delete requires
`reviewed:true` (else `400 review_required`) and sends SABnzbd's `del_files=1`
when requested. A job still tracked by an unfinished ReelScarey download is
refused with `409 active_downloads`; use Activity to stop that download.
Disabled/unconfigured clients return `409 builtin_unavailable` or
`409 client_not_configured`.

`POST /api/v1/usenet/speed-limits` with `{ "download_limit": 0 }` sets an
absolute SABnzbd limit in bytes per second for the running daemon; `0` removes
it. Non-zero values must be 1024–2000000000 (SABnzbd reads bare 1–100 as a
percentage, so ReelScarey rejects that ambiguous range). SABnzbd's own saved
percentage is not changed and applies again after a restart. Returns 204.

# Release reliability, duplicate review, metrics and account security (0.51)

Administrator GET `/api/v1/releases/blacklist` returns up to 500 blocked release
identities from the persistent blacklist. DELETE
`/api/v1/releases/blacklist/{id}` removes one entry. Entries are created after
terminal workflow failures and expire automatically after seven days; an entry
with `expires_at: 0` is permanent. Blocked releases are refused when a download
is queued with `release_blacklisted`. The release key is a normalized torrent
infohash or SHA-256 NZB identity, never a credential.

Administrator GET `/api/v1/duplicates?limit=100` returns a read-only duplicate
review. Groups are joined by movie or episode identity; `probable_duplicate`
requires every file to match on size, SHA-256 and inspected resolution.
Anything else is `needs_attention` with the first failed check. The endpoint
never deletes, moves or changes files. The limit is bounded to 500.

Administrator GET `/api/v1/metrics` returns Prometheus text exposition for
current download and music queue state. It exposes no titles, paths or
credentials and keeps no in-process counters across restarts.

Administrator GET `/api/v1/auth/totp` returns `{configured, enabled,
recovery_codes_remaining}`. POST `/api/v1/auth/totp/provision` returns a
one-time `{secret, otpauth_uri}`; nothing is enabled until POST
`/api/v1/auth/totp/confirm` with `{code}` succeeds, which returns ten one-time
recovery codes. POST `/api/v1/auth/totp/disable` requires a current code.
The secret is encrypted through the existing secure store and recovery codes
are stored only as hashes. Login accepts `{username,password,totp_code}` when two-factor
authentication is enabled; the code may be an authenticator code or a
single-use recovery code. The unauthenticated `/api/v1/auth/session` response
does not reveal whether two-factor authentication is enabled: a sign-in with
the correct credentials is refused with `invalid_totp` when a code is
required, and the sign-in form then asks for one.

Quality profiles now include `grab_delay_minutes` (0-1440). Automatic grabs,
scheduled searches and release-feed matches remain visible as queued but are
not submitted until the delay elapses. Manual selections are immediate.
Soft ranking penalties for unknown source/codec, very low seeders and a size
above 80% of the profile limit appear in `scoring_reasons` and never make an
otherwise valid release ineligible.

Settings `nfo_export` enables opt-in Jellyfin-compatible `.nfo` sidecars written
beside registered media. Existing sidecars are never replaced. Background
FFprobe analysis records bounded `analysis_state`, `analysis_json` and
`analysis_at` on `media_files`; it never changes `probe_json` or media bytes.
Stalled downloading jobs are recorded once in the activity feed after thirty
minutes without byte progress. SABnzbd/NZBGet completion payload parsers are
bounded and reconcile terminal state, but no unauthenticated webhook endpoint
is exposed.

`POST /api/v1/torrents/queue-limits` saves the built-in client's queueing
settings and applies them to a running client. The body accepts
`{queueing_enabled,max_active_downloads,max_active_uploads,max_active_torrents,
revision}`; each limit is 1-100. A stale revision returns
`revision_conflict`. The same values are returned in the `queue` object from
`GET /api/v1/torrents` and are persisted across supervised restarts.
