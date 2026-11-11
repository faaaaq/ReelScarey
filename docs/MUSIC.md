# Music catalog, import and organization


## Recovery and scan performance

Music submissions reserve a job before calling the download client. If a
response is lost or ReelScarey restarts, Retry tracks the same torrent hash
and job tag or the saved Usenet identity. An explicit client rejection is the
one exception: Retry resubmits that saved selection once the client is fixed,
re-reading the encrypted candidate and re-checking the indexer generation and
info hash first. An uncertain submission is never resubmitted automatically.
Stop tracking releases the reservation without changing the client's queue.

Imports save the staged file checksum before publishing, then commit each
registration with its import progress. On restart the worker can finish
registering its own staged output. A path conflict keeps the downloaded source
and staged copy intact for review.

Rollback verifies the edited file, the retained original and the catalog
entry. It refuses an occupied original path and can continue an interrupted
rollback from its saved phase. Edits created before 0.39.9 do not have the
required checksums and must be restored manually from verified originals.

The music schedule skips albums with unresolved download jobs, rotates its
batch through the wanted catalog, and rechecks the schedule, monitoring and
quality preferences when reserving each download. A rescan reuses tags and
hashes only when the saved file observation still matches.
Status: **implemented end to end.** The Music tab catalogs MusicBrainz artists,
albums and tracks, scans and registers existing files, applies reviewed
organization and tag rewrites with rollback, fingerprints unresolved files
through AcoustID/Chromaprint, searches audio releases, queues them through the
selected download client, reconciles them with a leased worker, imports the
completed folder, and runs wanted-album searches on a schedule.

## What works

- Search MusicBrainz artists and add one idempotently.
- Browse saved artists as a tile grid grouped A–Z, then open an artist for
  album tiles. An artist page also lists up to 15 tracks that have registered
  files, newest albums first, with an explicit all-tracks view. This is a
  library selection, not a play-count or provider popularity ranking.
- Browse albums and on-demand album tracks.
- Stream registered tracks in the authenticated web player with HTTP Range
  seeking, queue/next/previous, volume, and a persistent player bar. Playback
  addresses catalog file IDs; the stored media path is never sent to the
  browser.
- Scan a folder below the media root:
  - bounded walk (20,000 audio files, 24 folder levels, no symlinks);
  - a name-only pre-count before the walk feeds a live progress bar;
  - FFprobe tag reading for MP3, FLAC, M4A/MP4, OGG/Opus, WAV, AIFF and WMA;
  - SHA-256 evidence (up to 512 MiB per file);
  - matching against the local catalog by MusicBrainz release-group ID or by
    album title plus artist name;
  - review of matched and unresolved files. A well-tagged file can then be
    matched locally by **Match All** without a MusicBrainz or AcoustID call.
- Resume an interrupted import: a normal rescan skips unchanged registered files
  and carries matched-but-unregistered files forward without repeated work,
  while a full rescan re-examines every eligible file. Match All and Finalize
  are durable, resumable worker requests with live phase and progress fields.
- Discover missing catalog entries for selected unresolved files through an
  explicit reviewed action. It uses a tagged release-group ID, a tagged album
  artist/artist MBID, or one exact normalized artist name and album title,
  creates only the matching artist/album, loads the album tracks when the
  catalog has none and re-matches those files. If a tag-derived local artist or
  album with the same normalized name/title already exists, discovery reuses it
  instead of adding a duplicate. Ambiguous or missing evidence stays unresolved.
  - Exact-name searches are bounded to five pages of 25 results so a common
    artist name is not lost behind the first page.
  - If a release-group search is incomplete or misses an exact title, discovery
    compares the artist's bounded discography instead. This is still an exact
    normalized-title comparison, never title similarity.
  - An album-artist MBID (`MUSICBRAINZ_ALBUMARTISTID`) is preferred over the
    track-artist MBID. The track-artist MBID is used only when the album artist
    and track artist tags name the same artist, so a compilation track is never
    attached to one featured artist.
  - Common Unicode equivalents, accents, typographic quotes/dashes, punctuation
    and repeated whitespace are folded for comparison. A leading English "The"
    and the `Various`/`Various Artists` compilation aliases are comparison-only;
    a provider-created catalog row stores the MusicBrainz name, while a local row
    keeps the tagged name.
- Match selected unresolved files by audio fingerprint through AcoustID. It is
  a separate reviewed action that requires a saved application API key and
  fpcalc (Chromaprint) on the server. A request runs
  `fpcalc -json -length 120`, sends only the fingerprint and whole-file duration,
  requires a score of at least 0.85 and one release group. When AcoustID returns
  several release groups, the file's tagged release-group ID or one exact
  normalized album title may disambiguate; otherwise it stays unresolved. It
  then adds or loads that exact MusicBrainz release group and matches the track
  by AcoustID recording ID before existing tag/track evidence. Missing or
  ambiguous matches stay unresolved. Audio is never uploaded. This is native
  AcoustID/Chromaprint matching, not MusicBrainz Picard.
- Register matched files in place. Registration verifies the file size and
  modification time and records `music_files`; it never moves a file.
- Search configured indexers with the audio category, parse release names and
  rank them by album/artist match, format and seeders. Results link to the
  tracker; automatic queueing is not implemented.
- Import a completed album folder below the downloads root: probe each file,
  map a complete one-to-one set to the catalog tracks, copy and tag into the
  music root, publish without overwrite and register `music_files`. Sources are
  never modified.
- Queue a searched release through the configured client; a leased worker
  reconciles torrent/Usenet progress, imports the completed folder, and marks
  the download completed or blocked.
- A blocked download exposes its completed files and accepts an explicit
  file-to-track mapping, then resumes the normal verified import.
- Audio preferences (`allow_lossless`, minimum bitrate, preferred formats)
  filter automatic choices.
- Wanted albums (monitored with no files) and an interval schedule in search or
  download mode.
- Preview and apply reviewed organization:
  - destination `root/Artist/Album/NN Title.ext` (or `D-NN` for multi-disc
    albums);
  - actions `move`, `retag` or `move_retag`;
  - stale plans return `409` and must be previewed again.
- Write catalog tags with FFmpeg without re-encoding:
  - artist, album artist, album, title, track/total, disc/total, year;
  - MusicBrainz artist, release-group, track and recording IDs;
  - a previous copy is retained before a tag rewrite;
  - the written tags are verified with FFprobe before publication;
  - publication refuses an existing destination.
- Roll back a completed or failed edit, restoring the file and its previous
  tag record.

## AcoustID and Chromaprint matching

This is the provider-native AcoustID/Chromaprint path. It is not MusicBrainz
Picard, it does not read Picard's database, and it does not use acoustic
similarity beyond the Chromaprint fingerprint.

- The Docker image installs Debian `libchromaprint-tools`, which provides
  `fpcalc`. `REELSCAREY_FPCALC_PATH` may point at a different executable; the
  default command is `fpcalc`. The child process runs without a shell, with a
  bounded timeout and bounded output. The media path is rechecked for
  media-root containment, regular-file status and symlink escape immediately
  before it runs.
- The administrator must save an AcoustID **application** key. Create one at
  <https://acoustid.org/new-application>. The key is stored through
  `securestore` as the encrypted integration `acoustid`; it is covered by the
  existing `secrets.key` backup validation and is never returned by the API or
  written to logs. AcoustID is free for non-commercial use and its availability
  is not guaranteed.
- Only the Chromaprint fingerprint string and the whole-file duration are sent
  to AcoustID. Audio bytes, file paths, tags, library contents and account data
  are never uploaded.
- Confidence policy: only candidates with score `>= 0.85` count. The accepted
  candidates must name exactly one distinct release-group ID. Zero or multiple
  release groups leave the file unresolved with a specific message; the code
  never guesses from a low score or between albums. The highest-score recording
  for that release group is used first for track matching. Equal-score
  recordings for the same release group fall back to existing tag/track
  evidence instead of picking one arbitrarily.
- Fingerprints are cached in `music_fingerprints` by relative path together
  with the file size, mtime, duration and fingerprint. The cache is reused only
  while size and mtime still match; a changed file is fingerprinted again and
  replaces the row. A moved or renamed file is a new path and is fingerprinted
  again.
- The reviewed `POST /scans/{id}/fingerprint` action processes at most 20
  unresolved files and starts MusicBrainz work for at most 10 album groups per
  request. Excess files are deferred and reported. It stops starting work
  before the server write deadline, keeps sequential AcoustID lookups at least
  350 ms apart, and never moves, renames or edits a file.
- Re-running is safe: matched files are no longer unresolved, cached
  fingerprints are reported as fingerprinted again without running `fpcalc`,
  and catalog rows are found instead of copied.

## Provider and bounds

- Default provider: `https://musicbrainz.org/ws/2`. No credentials are stored.
  An administrator may save an optional custom base URL (for a local mirror
  they host themselves); it is encrypted through `securestore` as the
  integration name `musicbrainz`. No MusicBrainz mirror is bundled with
  ReelScarey.
- The custom URL contract is explicit: the value is a server prefix. A path
  ending in `/ws/2` is kept, and `/ws/2` is appended exactly once to any other
  path, including the server root. HTTPS is allowed for any host. Plain HTTP is
  accepted only for `localhost`, loopback (`127.0.0.0/8`, `::1`), RFC1918
  addresses and IPv6 ULA. Credentials, query strings, fragments, backslashes and
  path traversal are rejected, and redirects are never followed.
- The public server is paced at one request per second. A configured local or
  private mirror is paced at 50 ms. Both still honor the server's own `429`/`503`
  cooldown. A `429`/`503` response becomes a rate limit with `Retry-After`
  (delay-seconds or an HTTP date); it is never silently retried.
- Responses are bounded (2 MiB search/artist, 4 MiB release browse, 8 MiB release
  tracks). Redirects are refused.
- Discographies are browsed rather than read from the 25-item lookup include.
  At most 1,000 release groups are stored per artist.
- Album tracks come from the earliest **Official** release in the group. Edition
  selection is not implemented.
- Cover art uses `https://coverartarchive.org/release-group/{mbid}/front-500`;
  the browser follows the archive.org redirect.
- Fixed AcoustID provider: `https://api.acoustid.org/v2/lookup`. Requests send
  `client` (the saved application key), `duration`, `fingerprint` and
  `meta=recordings+releasegroups` and request JSON. Arbitrary URLs and
  redirects are refused. Responses and parsed lists are bounded (2 MiB, 100
  results/recordings/release groups per result). Sequential lookups are spaced
  at least 350 ms apart to stay below the published three-per-second limit.
  `429` becomes a rate limit with `Retry-After`; `400`/`401`/`403` become an
  invalid-key error; transport and server failures become unavailable. Nothing
  is silently retried.

## MusicBrainz cache, cooldown and custom mirror

Large libraries issue many repeated lookups. ReelScarey keeps a persistent,
local MusicBrainz response cache and a persistent rate-limit cooldown in SQLite.

- Successful raw JSON responses are cached by the exact base URL, endpoint and
  sorted query string. A different mirror never reuses another server's rows.
- Positive artist, release-group, release and track metadata is cached for 30
  days. Search results are cached for 7 days. An explicit `404` or an empty
  metadata result (zero search/browse rows, or a release with no tracks) is
  cached as negative evidence for 1 day only. Negative evidence is rechecked
  after that window.
- Only a complete, successfully decoded JSON body is cached. Truncated,
  oversized, invalid and non-404 error responses are never cached.
- Expired rows are deleted in bounded batches (at most 500 per pass) during
  occasional provider work, so the table cannot grow without bound.
- Cache reads, writes and cleanup fail open. A database problem is logged as a
  warning and matching continues against the provider; it never corrupts match
  state or blocks a scan.
- The cache is provider evidence, not permanent truth. Album and artist data
  change slowly, so a cached response can be incomplete or outdated; removing
  the cache or retrying later rechecks the provider.
- On `429` or `503`, the parsed retry window and an actionable message are saved
  in `musicbrainz_provider_state`. Every provider request checks that row first
  and returns `music_rate_limited` with `Retry-After` instead of sleeping inside
  an HTTP request or retrying in a tight loop.
- When the matching workflow sees that cooldown it stops in `review`/`ready`
  with an explicit warning. Files already matched are preserved; unresolved
  files stay available for a later retry. The status API and the Music Import
  page show the pause, the retry time and the cache entry count.
- The optional mirror is administrator-supplied and is **not** bundled,
  downloaded or managed by ReelScarey. There is no MusicBrainz mirror in the
  Docker image.

## Scan and registration

Open **Music → Library Import** (also linked from the Music toolbar and empty
state). Music roots are configured in **Settings → Media Management → Library
roots**, alongside the video collections. Music defaults to the exact `Music`
folder below `REELSCAREY_MEDIA_DIR`; if only a lowercase `music` folder exists,
that existing directory is shown and used. A missing folder is reported as
unavailable rather than guessed. Add more music roots to scan several folders in
one reviewed scan.
- Every scan root must be a relative folder below `REELSCAREY_MEDIA_DIR`.
  Absolute paths, `..`, symbolic links and overlapping roots are rejected.
- **Scan folder** offers the configured roots and starts one scan across the
  selected directories. Scan results record the root that supplied each file;
  registration keeps the file in place and stores its own root.
- Matching order:
  - album: MusicBrainz release-group ID, then a trusted album-artist MBID plus
    exact normalized album title, then exact normalized album title plus
    artist/album-artist name;
  - track: MusicBrainz track ID, recording ID, disc/number, then exact
    normalized title. The same order is used again after discovery.
- A missing disc tag is not a reason to skip the number match. A single-disc
  album matches its only disc; a multi-disc album uses the title to choose
  between equal track numbers instead of assuming disc 1.
- For track titles only, a trailing `(feat. ...)` or `[ft. ...]` credit is also
  compared with its base title. If both the base and credited titles exist in
  the album, the file stays unresolved instead of picking one.
- Album-artist MBIDs are read from `MUSICBRAINZ_ALBUMARTISTID` and stored in the
  scan's tag document (`tag_json`). The document carries a parser version; an
  unchanged file whose saved document predates the current parser is probed once
  more while its saved SHA-256 is reused. Older scans therefore pick up the new
  field on the next scan without reading the media again.
- Unmatched files stay `unresolved` with their tags and an explanation. Select
  those files and choose **Discover missing catalog** to review a bounded
  MusicBrainz search. Discovery only acts on strong evidence:
  - a release-group MusicBrainz ID in the file tag; or
  - a trusted album-artist/artist MusicBrainz ID in the file tag; or
  - exactly one artist whose normalized name equals the tagged album artist (or
    artist), and exactly one release group whose normalized title equals the
    tagged album. The year is used only to disambiguate multiple exact title
    matches.
- Discovery adds only the missing artist and release group, loads the album's
  tracks, and re-matches the selected files. It never moves, tags or registers a
  file, and a file it cannot prove stays unresolved with a clear message:
  missing artist or album tag, no exact artist, ambiguous artist, no exact
  release group, ambiguous release group, or album in the catalog with no
  matching track.
- Adding an artist or album is idempotent. A release group is stored once even
  if two artists list it, and a rerun finds the same catalog rows instead of
  copying them. A provider failure keeps the existing catalog, scan rows and
  registered files and reports an actionable warning.
- A request processes at most 10 album groups and 500 files, and stops starting
  new provider work before the server write deadline. The response reports
  matched files, added artists/albums, unresolved files, deferred files and
  unresolved files remaining in the scan. Select the remaining files and run it
  again to continue.
- Audio fingerprint matching is a second, separate action for selected
  unresolved files. Select them and choose **Match by audio fingerprint**. It
  requires explicit review, a saved AcoustID application API key and fpcalc
  (Chromaprint) on the server; the button stays disabled when either is
  missing. Fingerprinting computes Chromaprint evidence for the selected files,
  looks it up through AcoustID, adds only the matched artist and album, loads
  the album tracks and re-matches those files. A request processes at most 20
  files and 10 album groups and stops early within its time budget. It adds only
  the exact release group AcoustID names with a score of at least 0.85 and one
  release group after tagged-ID/exact-album disambiguation. Audio with no
  confident match, an ambiguous result or a provider warning stays unresolved;
  nothing is moved, tagged or registered. The response reports `fingerprinted`
  files, `provider_calls`, matched files, added artists/albums, unresolved files,
  deferred files, unresolved files remaining in the scan and warnings.
- The AcoustID status panel reports whether an application key is configured,
  whether fpcalc is available and the server's message. **Configure AcoustID**
  saves the key through the encrypted secure store, links to
  https://acoustid.org/new-application and can remove the saved key. The API key
  is never returned to the browser or displayed by the page. Missing keys,
  missing fpcalc, rejected keys, rate limits, ambiguous matches and no-match
  results produce an actionable warning instead of a success message.
- A scan is a reviewed job: start it, poll its state, discover catalog entries,
  fingerprint unresolved files, run **Match All** or **Finalize**, or register
  selected matched files with `reviewed: true`. An interrupted walk is marked
  failed; a queued Match All/Finalize request is re-queued and resumed by the
  durable worker after a restart.

## Tag-first local catalog

The local catalog is the first matching source, not a fallback. **Match All**
uses a reviewed tag-first path so a large, well-tagged library does not wait on
MusicBrainz or AcoustID:

1. match the file against the saved catalog, including local rows from earlier
   scans;
2. if that fails and the tags contain album artist/artist, album, title and a
   track number, create or reuse the catalog artist, album and track from those
   tags;
3. fingerprint and look up still-unresolved files through AcoustID, using the
   returned release-group and recording IDs together with the tags when the tags
   are complete enough for a local row; and
4. only then run MusicBrainz discovery for files whose tags were not complete
   enough for a local identity.

A complete local identity needs a non-empty album artist (or artist), album and
title plus a positive track number. A missing disc tag defaults to disc 1 for a
new row; an existing multi-disc album is matched by its saved disc/number, then
title. Case, punctuation, spacing, Unicode equivalents and a leading English
article are folded for identity, so different files that only differ in those
ways reuse one artist, album and track. Two different titles at the same
disc/number are never renumbered or overwritten: the first saved position wins,
and an ambiguous later file stays unresolved for review.

Identifiers are deterministic and prefixed:

- `local-artist:<hash>` when no genuine MusicBrainz artist ID is tagged;
- `local-album:<hash>` when no genuine MusicBrainz release-group ID is tagged;
- `local-track:<hash>` when no genuine MusicBrainz track ID is tagged.

A tagged genuine MusicBrainz artist, release-group, track or recording ID is
stored directly and no MusicBrainz request is made to validate it. A file with
only a recording ID gets a local track identifier plus the genuine recording ID.
If a later file proves a genuine artist, release-group, track or recording ID
for an existing local row, the row keeps its catalog ID, tracks and file
references and only the local identity key is upgraded when no other row already
uses that MusicBrainz ID. Local identifiers are catalog keys only: they are never
written into `music_artistid`, `musicbrainz_*` or any other MusicBrainz tag
field. Edit plans filter them out and write only values that are still valid
MusicBrainz IDs.

This identity is **tag-derived, not canonical MusicBrainz identity**. It records
what the files say, including a tag-provided release-group ID, but it does not
prove a release group, track list or ordering. A later reviewed MusicBrainz add
or discovery can enrich the catalog; nothing here invents canonical ordering or
replaces an existing canonical row with a local one. Provider discovery that
names the same normalized artist and album reuses the local rows instead of
adding a duplicate. New local artists and albums have `status: missing` until
the existing registration path records a file, which marks them `available`.

Cover art, overviews and other provider metadata are not required for a local
row. Finalize uses the tagged artist, album and title for folder/file names and
for a tag rewrite; a row without provider metadata still registers and
organizes.

## Resumable import workflow

The import screen drives a resumable pipeline: add a media root, scan with live
progress, run **Match All** for the remaining unresolved files, then run
**Finalize** to register and organize the matched files. Unmatched files stay in
the scan for manual review. Every long step is saved in `music_scans` and run by
one durable background worker; the browser's HTTP request never owns the work.

- **Live scan progress.** Before walking, the scan does a fast name-only count of
  eligible audio files (same extension, depth, symlink and 20,000-file rules) and
  stores it in `progress_total`. The walk writes `progress_done`, `file_count`,
  `skipped_count` and a message after every bounded batch, not after every file.
  If the pre-count fails, `progress_total` stays `0` and the message says the bar
  is approximate. `current_path` is the file being processed by a matching,
  fingerprinting or organizing phase.
- **Phases.** `phase` is one of `queued`, `scanning`, `matching`,
  `fingerprinting`, `ready`, `organizing`, `completed`, `failed` or `cancelled`.
  The existing `state` values (`scanning`, `review`, `completed`, `failed`,
  `cancelled`) are unchanged for backward compatibility.
- **Skip policy (normal scan).** With no full rescan, a path is skipped without
  repeated work when its saved observation still matches the file's size and
  saved nanosecond modification time:
  - a path already registered in `music_files` is skipped entirely and counted
    in `skipped_count`;
  - a path that was `matched` in a previous scan but was never registered is
    copied into the new scan as `matched`, with its previous artist/album/track
    IDs, and counted in `skipped_count`; it stays visible and finalizable;
  - a previously `unresolved` path is not skipped: it is observed and matched
    again against the current catalog, so a catalog entry added after the first
    scan can resolve it;
  - a path whose size or modification time changed is read and matched again.
  Skipped means "no repeated work", never "hidden from the workflow".
- **Full rescan.** `full:true` walks every eligible file and re-observes and
  re-matches it, including registered files. It does not duplicate library rows:
  registering a file again updates the existing `music_files` row keyed by
  relative path. A full rescan still reuses a saved digest/tag observation while
  the file bytes are unchanged, so it re-examines the file without needlessly
  reading or tagging it again.
- **Match All.** `POST /scans/{id}/match-all {reviewed:true}` records the
  owner's consent and returns `202` with `request:"match"` and
  `phase:"queued"`. The worker then runs `matching`: first the saved catalog,
  then local catalog creation from complete tags for the rest. Files still
  unresolved run `fingerprinting` (AcoustID, when a key and fpcalc are
  available); AcoustID results with complete tags create local rows from the
  returned release-group/recording IDs, and files without complete tags keep the
  existing provider-assisted fingerprint path. Finally a `matching` discovery
  step runs MusicBrainz only for files whose tags were not complete enough for a
  local identity. It processes bounded batches, renews its database lease and
  saves progress after each batch. Provider, rate-limit and transient errors
  keep the matched rows, leave the rest unresolved and end in `ready` with a
  warning; a missing AcoustID key or fpcalc adds an explicit warning and
  MusicBrainz discovery still runs. The final message reports how many files
  matched from the saved catalog/tags, how many matched by audio fingerprint and
  how many were discovered through MusicBrainz. Match All never registers,
  moves, renames or tags a file. `reviewed:true` is consent to create catalog
  entries only.
- **Finalize.** `POST /scans/{id}/finalize {reviewed:true}` returns `202` with
  `request:"finalize"`. The `organizing` phase registers the matched files in
  place through the existing verification path, finds the registered file IDs,
  previews each edit plan and applies move/retag/move_retag jobs one at a time,
  waiting for each before starting the next. Files that are already correct need
  no edit. The existing no-overwrite, retained-backup, journal and rollback
  safety rules still apply. A succeeded finalize ends with `phase:"completed"`
  and `state:"completed"`; any registration or edit failure leaves the scan in
  `review` with `phase:"ready"`, an explicit count and message, and the failed or
  unmatched files available for manual review. Finalize never creates download
  rows or fabricated file history for existing files.
- **Restart recovery.** `RecoverScans` fails an interrupted walk because its rows
  may be incomplete. A queued/matching/fingerprinting/organizing request is put
  back in the queue with an explicit "interrupted by a restart" message, its
  stale lease is cleared and the worker resumes it. Work is never silently
  reported as completed.
- **One worker.** A claim sets `work_owner` and a five-minute `work_until`
  lease. Only one scan is claimed at a time, and the database row — not an
  in-memory queue — is the source of truth. A request is rejected while a scan is
  running or another match/finalize request already owns the scan.

## Organization and tagging

- All edits are previewed first. The preview returns a `plan_hash`; applying a
  plan whose hash no longer matches is refused.
- A file is staged in its destination folder. Moves use a hardlink where
  possible and a verified copy otherwise. Publication uses an exclusive
  create/link, so an existing destination is never overwritten.
- A retag copies the source to `<source>.reelscarey-backup`, writes tags with
  FFmpeg `-c copy` to a staged file, verifies the staged tags with FFprobe, then
  publishes. The backup blocks a second retag until it is reviewed.
- MP3 writes use ID3v2.3; M4A uses `use_metadata_tags` so custom keys persist.
  OGG keeps comments on the stream. OGG embedded artwork is not supported.
- Rollback restores the retained backup or moves the destination back, restores
  the previous catalog path and the previous tag record, and marks the job
  `rolled_back`.

## Limits

- AcoustID matching requires the bundled `fpcalc` and an owner-supplied
  application key. It is not Picard, does not write MusicBrainz tags on its own,
  and does not guarantee a match. A provider outage leaves files unresolved.
- Only Chromaprint fingerprint and duration are sent. No audio is uploaded.
  A release group is added only from one unique result with score `>= 0.85`;
  a tagged release-group ID or one exact normalized album title can disambiguate
  multiple release groups. Low scores, unresolved multiple groups and missing
  release groups require better tags or manual review instead of a guess.
  When a file has complete tags, the AcoustID release-group and recording IDs
  are applied to a local row instead of fetching album metadata and a track list
  from MusicBrainz. A complete local identity that conflicts with a saved album
  stays for review rather than creating a second album through discovery.
- Fingerprint work through the manual `/fingerprint` endpoint is bounded to 20
  files and 10 album groups per request. Select the remaining unresolved files
  and run the action again to continue. **Match All** is not limited to one
  request: it processes bounded batches and renews its lease, and a rate limit
  stops the phase with a warning instead of looping. Cached evidence is local to
  this media path and is replaced when size or mtime changes.
- Match All never registers, moves or tags media. Finalize registers in place and
  then applies the reviewed edit plan; it does not replace an existing library
  file with a downloaded upgrade.
- The MusicBrainz cache is local evidence, not a complete offline catalog. It is
  not a permanent truth, and clearing it or waiting for its TTL rechecks the
  provider. Only successful JSON bodies and explicit 404/empty metadata are
  cached; provider errors, truncated bodies and invalid JSON are not.
- Rate-limit cooldowns are persisted, so restarting ReelScarey does not bypass a
  provider's retry window. Matching stops with a review warning instead of
  sleeping or retrying in a loop.
- A custom MusicBrainz base URL is optional. ReelScarey does not bundle, host,
  install or manage a MusicBrainz mirror; the administrator supplies and runs
  it. Public HTTP mirrors are refused, and redirects from any base URL are
  never followed.
- A full rescan re-examines registered files but never creates a second
  `music_files` row for the same relative path. It also does not delete catalog
  rows, registered files or scan history, and it does not re-tag files by itself.
- The worker imports a completed folder automatically only when every track has
  one matching disc/track file. Missing or ambiguous tags leave the job
  `blocked`; its files can be mapped explicitly from the Wanted page.
- No resolution/format upgrade suggestions and no per-album profile overrides.
- Search mode stores candidates for review; it does not queue.
- No playlists, lyrics, ratings, scrobbling or MusicBrainz editing.
- Catalog discovery requires exact normalized artist and album-title evidence,
  a trusted tagged MBID, or a release-group ID. It is not a fuzzy matcher, does
  not use title similarity as identity and does not add a complete artist
  discography. The discography fallback compares exact normalized titles and
  stores only the one match.
- A local catalog row is tag-derived identity, not canonical MusicBrainz
  identity. A tagged release-group ID is stored without validating it, and a
  local track list or ordering is not proof of the canonical release. A tagged
  ID that MusicBrainz does not know stays a local row until a reviewed add or
  discovery replaces or enriches it. The matching pass does not verify local
  rows with the provider, does not rewrite a canonical MBID row as a local row,
  and does not merge two different titles that occupy the same disc/number.
- Audio fingerprint matching is not a replacement for tag discovery and does
  not identify tracks without a usable AcoustID result. It needs the AcoustID
  application key and fpcalc; the page disables the action when either is
  missing and never displays the saved key.
- A compilation whose album-artist tag is missing is not guessed: per-track
  artist searches can fail, and the file stays unresolved until the album
  artist or a release-group ID is tagged. Featured-credit fallback applies only
  when the album artist and track artist are the same tagged name.
- `MUSICBRAINZ_ALBUMARTISTID` is persisted in the scan's tag document only;
  there is no dedicated scan column or migration. A scan created before this
  field existed needs a new scan to use it; an unchanged matched-but-unregistered
  file is probed once more by a normal rescan, and a registered file is only
  re-probed by a full rescan.
- A release MBID (`MUSICBRAINZ_ALBUMID`) is stored but not used for discovery:
  the catalog is keyed by release group. Files that carry only a release MBID
  fall back to the tagged artist and album title.
- No embedded cover-art writing and no CUE/multi-track single-file albums.
- Edits only touch registered files below the media root. Symlinks are skipped.
- A cross-device move falls back to a verified copy; it is not an atomic rename.
- Every artist and album is stored with `status: missing` until a file is
  registered, then `available`. Partial albums are not yet distinguished.
- The UI labels the catalog/import/organize pages by their actual scope; do not
  describe acquisition as available.

## API

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/metadata/music/artists?query=&page=` | MusicBrainz artist search |
| GET | `/api/v1/library/music/artists?query=&page=` | Saved artists |
| POST | `/api/v1/library/music/artists` | `{musicbrainz_id,monitored?}`; 201 created or 200 existing |
| GET | `/api/v1/library/music/artists/{id}` | Artist and albums |
| GET | `/api/v1/library/music/albums/{id}` | Album and tracks; first read loads tracks |
| GET/POST | `/api/v1/library/music/scans` | List scans or start `{roots:[...],full?}` (legacy `{root}` accepted); 202 |
| GET | `/api/v1/library/music/scans/{id}?page=` | Scan state, phase, progress and 100 files per page |
| POST | `/api/v1/library/music/scans/{id}/match-all` | `{reviewed:true}`; queues durable saved-catalog/tag matching, then AcoustID, then MusicBrainz discovery; 202 |
| POST | `/api/v1/library/music/scans/{id}/finalize` | `{reviewed:true}`; queues registration then sequential organization/tagging; 202 |
| POST | `/api/v1/library/music/scans/{id}/register` | `{file_ids,reviewed:true}` |
| POST | `/api/v1/library/music/scans/{id}/discover` | `{file_ids,reviewed:true}` or `{all:true,reviewed:true}`; bounded catalog discovery |
| POST | `/api/v1/library/music/scans/{id}/fingerprint` | `{file_ids,reviewed:true}` or `{all:true,reviewed:true}`; reviewed Chromaprint/AcoustID matching |
| GET/PUT/DELETE | `/api/v1/library/music/acoustid` | Status; PUT `{api_key}` saves or replaces the encrypted key, DELETE removes it. The key is never returned |
| GET/PUT/DELETE | `/api/v1/library/music/musicbrainz` | Status; PUT `{base_url}` saves or replaces the encrypted optional provider URL, DELETE resets to public MusicBrainz. Returns `base_url`, `default_base_url`, `using_default`, `cooldown_until`, `message` and `cache_entries`; never a secret |
| POST | `/api/v1/library/music/scans/{id}/cancel` | `{}` |
| POST | `/api/v1/library/music/scans/{id}/skip` | `{file_id}` |
| GET | `/api/v1/library/music/files?page=` | Registered files |
| GET | `/api/v1/library/music/edits?page=` | Recent edit jobs |
| POST | `/api/v1/library/music/edits/preview` | `{file_ids}`; returns the plan and hash |
| POST | `/api/v1/library/music/edits/apply` | `{file_ids,plan_hash,reviewed:true}`; 202 |
| GET | `/api/v1/library/music/edits/{id}` | Job state and files |
| POST | `/api/v1/library/music/edits/{id}/rollback` | `{reviewed:true}` |
| POST | `/api/v1/library/music/albums/{id}/releases` | `{query?}`; audio-indexer search, no queue |
| POST | `/api/v1/library/music/albums/{id}/import` | `{source_path,root}`; reviewed folder import; 202 |
| GET | `/api/v1/library/music/imports?page=` | Import jobs |
| GET | `/api/v1/library/music/imports/{id}` | Import job and files |
| POST | `/api/v1/library/music/albums/{id}/download` | `{release_id,root}`; queue through the selected client; 202 |
| GET | `/api/v1/library/music/downloads?page=` | Music download jobs |
| GET | `/api/v1/library/music/downloads/{id}` | One job |
| POST | `/api/v1/library/music/downloads/{id}/cancel` | `{}` |
| POST | `/api/v1/library/music/downloads/{id}/retry` | `{}` |
| GET | `/api/v1/library/music/downloads/{id}/files` | Completed files with tags for mapping |
| POST | `/api/v1/library/music/downloads/{id}/mapping` | `{mappings:[{file_index,track_id}]}`; resume the import |
| GET/PUT | `/api/v1/library/music/preferences` | Audio preferences |
| GET | `/api/v1/library/music/wanted?page=` | Monitored albums with no files |
| GET/PUT | `/api/v1/library/music/schedule` | Wanted-album schedule |
| POST | `/api/v1/library/music/schedule/run` | `{}`; one bounded run |
| GET/PUT/DELETE | `/api/v1/library/music/albums/{id}/preferences` | Per-album override or reset to global |
| GET | `/api/v1/library/music/upgrades` | Albums below their effective preference |

The discovery response reports `selected`, `matched`, `artists_added`,
`albums_added`, `unresolved`, `remaining`, `deferred` and `warnings`. The
fingerprint response adds `fingerprinted` and `provider_calls` to the same
shape. Every scan row also reports `full_scan`, `phase`, `progress_total`,
`progress_done`, `current_path`, `message`, `skipped_count`, `request`,
`requested_at` and `work_until`. AcoustID status reports `configured`,
`updated_at`, `fingerprint_available` and `message`; it never includes the saved
key. MusicBrainz status reports the effective `base_url`, `default_base_url`,
`using_default`, `cooldown_until` (Unix seconds, `0` when not cooling down),
`message` and the count of unexpired `cache_entries`. A workflow paused by a
provider cooldown finishes in `review`/`ready` with the matched rows intact and a
warning naming the retry window; it is not reported as failed. All routes are
administrator-only. Mutations require CSRF and same-origin validation. Request
accounts cannot read or write the music catalog.

## Data model

- `059_music.sql` — `music_artists`, `music_albums`, `music_tracks`.
- `060_music_files.sql` — `music_scans`, `music_scan_files`, `music_files`.
- `061_music_edits.sql` — `music_files.root`, `music_edit_jobs`,
  `music_edit_files`.
- `062_music_acquisition.sql` — `music_albums.revision`,
  `music_release_candidates`, `music_import_jobs`, `music_import_files`.
- `063_music_downloads.sql` — `music_downloads`, `music_preferences`,
  `music_schedule`.
- `064_music_album_preferences.sql` — per-album `allow_lossless`,
  `min_bitrate` and `preferred_formats` overrides.
- `072_music_fingerprints.sql` - `music_fingerprints` cache and the encrypted
  `acoustid` integration name.
- `073_music_scan_workflow.sql` - `music_scans.full_scan`, `phase`,
  `progress_total`, `progress_done`, `current_path`, `message`, `skipped_count`,
  `request`, `requested_at`, `work_owner`, `work_until` and the workflow/path
  indexes.
- `074_musicbrainz_cache.sql` - `musicbrainz_cache` with an expiry index,
  `musicbrainz_provider_state` seeded with `id=1`, and the encrypted `musicbrainz`
  integration name.

The migrations are additive except for the `music_files.root` column. Existing
movie/TV rows, foreign keys and consumers are untouched.

## Next phase

The core Lidarr-style path — catalog, want, search, download, reconcile, map
blocked files, import, organize and tag — is implemented, including upgrade
suggestions and per-album preference overrides. Optional future work: automatic
upgrade replacement and richer multi-profile assignment.

## Validation

- `internal/music/client_test.go` — normalization, pagination, rate limits,
  oversized bodies, redirect refusal, exact release-group search and lookup.
- `internal/music/musicbrainz_test.go` — cache hit/miss, positive/search/negative
  TTLs, expired rows and bounded cleanup, ignored invalid/error responses,
  fail-open cache reads/writes, persisted 429 cooldown across reopen,
  Retry-After seconds/HTTP-date parsing and bounds, public versus local pacing,
  URL normalization (HTTPS, private HTTP, public HTTP rejection,
  credentials/query/fragment/traversal), status fields and cache counts.
- `internal/music/normalize_test.go` — case, punctuation, typographic quotes and
  dashes, non-breaking spaces, composed/decomposed accents, leading articles,
  Various Artists aliases and featured-credit splitting.
- `internal/music/tags_test.go` — format tags, album-artist MBID, OGG stream
  tags, invalid bodies.
- `internal/music/store_test.go` — idempotent artist saves, release-group reuse
  across artists, track replacement, invalid paging.
- `internal/music/scan_test.go` - bounded scan, matching, in-place registration,
  availability, escaping roots, track evidence order, missing disc handling,
  ambiguous titles, article/Unicode album lookup.
- `internal/music/workflow_test.go` - name-only pre-count before the walk, live
  scanning progress, skip of unchanged registered files, carry-forward of
  matched-but-unregistered files, unresolved reconsideration after a catalog
  change, full rescan without duplicate library rows, restart re-queue/lease
  recovery, request validation/consent/state rejection, tag matching followed by
  AcoustID fingerprint fallback, unavailable-AcoustID warning, MusicBrainz
  cooldown stop with preserved matched rows, finalize
  registration plus sequential edit organization, and partial failure that stays
  in review.
- `internal/music/local_catalog_test.go` - a 1,200-file generated well-tagged
  library matched with a provider that fails if called (zero MusicBrainz,
  AcoustID and fingerprint calls; measured in the test log), tagged
  MusicBrainz IDs used without a provider, later genuine IDs enriching a local
  row without a duplicate, multi-disc/missing-disc and
  case/punctuation normalization, duplicate-position retention, rerun
  idempotence with no duplicate catalog rows, mixed local/AcoustID/MusicBrainz
  fallback, AcoustID provider discovery reusing a local album instead of
  duplicating it, tag-only finalize registration and rename, and a real FFmpeg
  retag proving only genuine MBIDs are written.
- `internal/music/discovery_test.go` — release-group ID creation, tagged artist
  MBID use, later artist-search pages, discography fallback (including a later
  browse page), missing disc tags, multi-disc titles, compilation album-artist
  MBIDs, Various Artists and featured-artist aliases, exact artist/album
  creation, ambiguous and missing evidence retention, unknown tagged MBIDs,
  album added without a matching track, duplicate artist reuse, provider failure
  retention, scan count refresh, rerun idempotence and re-match.
- `internal/music/acoustid_test.go` — fake fingerprinter and fake AcoustID HTTP
  server: strong unique release-group matching, recording-ID-first track match,
  score `>= 0.85`, multiple/no release groups, no match, invalid key, rate limit,
  missing key/fpcalc, 20-file deferral, cached fingerprint reuse, cache
  invalidation on size/mtime change, status without key disclosure and
  `fpcalc` output validation.
- `internal/music/edits_test.go` — move/retag/no-op plans, move apply/rollback,
  FFmpeg tag write, retained backup and rollback.
- `internal/httpapi/music_test.go` - authentication, CSRF, catalog, scan,
  discovery, AcoustID key status/save/delete without key disclosure, MusicBrainz
  status/save/reset without secrets, fingerprint
  route validation and error mapping, match-all/finalize route validation and
  progress fields, edit routes, error mapping, SPA routes.
- `internal/database/music_upgrade_test.go` — additive catalog migration.
- `web/src/test/Music.test.tsx`, `MusicImport.test.tsx`,
  `MusicOrganize.test.tsx` — catalog, import, tag discovery, AcoustID status,
  key save/remove without echoing the key, disabled fingerprint controls and
  honest fingerprint result/error summaries, and organize pages.
- `scripts/music-container-smoke.sh` — disposable Docker acceptance: add an
  artist, load an album, generate a tagged FLAC, scan, register, preview/apply
  organization and tags, verify the MusicBrainz tag and retained backup, roll
  back, then generate a complete tagged album folder, import it, and verify the
  imported files and untouched sources. This passed against the isolated
  `reelscarey:music-acq` image on the acceptance server.

Catalog discovery was validated locally with targeted Go tests
(`go test ./internal/music ./internal/httpapi`), the frontend format/type checks
and the `MusicImport` Vitest suite. The 0.46 discovery hardening added the
normalization, tagged-MBID, bounded-page, discography-fallback, compilation and
track-evidence cases above. It was not run against the live acceptance server or
a real MusicBrainz library; provider behavior is covered by local fake
MusicBrainz responses. The AcoustID status, key save/remove and fingerprint
result UI were validated with fake API responses in the `MusicImport` suite; no
real AcoustID account, fpcalc run or audio file was used. Backend
AcoustID/Chromaprint matching was validated locally with generated media and
fake `fpcalc`/AcoustID/MusicBrainz responses. It was not run against a real
AcoustID account or owner media; the service is free for non-commercial use but
its availability is not guaranteed. The Docker runtime installs
`libchromaprint-tools`; `scripts/music-container-smoke.sh` checks
`fpcalc -version` in the disposable container.

The tag-first fast path was validated locally with generated tag rows, a fake
MusicBrainz server that fails on any request and a fake AcoustID/fingerprint
pair. `TestMatchAllWellTaggedLibraryUsesNoProviders` matched 1,200 generated
well-tagged files in about 1.7 seconds on the development machine with zero
MusicBrainz, AcoustID and fingerprint calls. The retag case used a generated
FLAC and the local FFmpeg/FFprobe tools; it is skipped when those tools are not
installed. These checks use temporary directories and generated or tag-only
fixtures, never owner media. They do not prove provider behavior, a real
MusicBrainz library or a full Docker acceptance run; the Docker acceptance
script was not re-run for this change.

## Music server, link downloads and discovery

### Authenticated web player

The Music library plays registered files through an authenticated streaming
endpoint with HTTP Range support. The persistent player bar provides queue,
seek, volume, previous/next and close controls. Playback addresses catalog file
IDs; stored media paths are never sent to the browser. Album and artist views
also expose play actions. This uses the existing ReelScarey web port and
session.

### Optional Subsonic-compatible endpoint

`/rest/*` is an opt-in compatibility layer for existing music clients. It is
disabled by default and has no listener or port of its own: clients use the
same ReelScarey host and port. If a reverse proxy or firewall is in use, expose
the existing ReelScarey address; do not open an additional music port.

The administrator generates a music-server token on **Music -> Server**. The
Subsonic username can be any label (for example `reelscarey`); the password is
the generated token, never the administrator account password. The token is
stored through `securestore` and is covered by the existing `secrets.key`
backup. Enabling the endpoint does not enable the administrator browser session
for third-party clients.

Supported subset: ping, license, music folders, artists, indexes, artist,
album, album list, song, search, random songs, cover art, empty playlists,
scrobble no-op, and stream/download with Range support. Write operations such
as ratings are not implemented. Clients that require unsupported actions should
be treated as untested.

### Link downloads

**Music -> Discover** accepts public Spotify, Deezer, YouTube and YouTube Music
links for tracks, albums, playlists and artists, plus free-text search queries.
Spotify metadata is read from public embed pages; Deezer uses its public API;
YouTube and YouTube Music use yt-dlp. No Spotify account or API key is used.

A link is previewed before anything is downloaded. Selected tracks become
durable acquisition jobs. The acquisition worker searches YouTube (or uses a
direct YouTube video ID), chooses a close duration match, creates tag-derived
local catalog rows when needed, and hands the downloaded file to the existing
reviewed import worker. That worker tags and publishes without overwrite.

Single-track and unsupported-source downloads stay local and explicit. The
worker never replaces an existing registered file. YouTube/Spotify/Deezer terms,
availability and account restrictions remain external constraints.

### Charts and playlist monitor

Deezer's global chart is shown in **Music -> Discover**. Chart tracks already in
the library play in the web player; missing tracks offer the same reviewed
download action as artist top tracks.

Playlist and artist monitoring starts **paused**. When enabled, it re-resolves
saved public links on the configured interval, queues unseen tracks through the
same acquisition pipeline, and records each source's last check and error. The
monitor uses the existing server and port and starts no additional process or
listener.

### Artist images and album covers

Artwork is downloaded and cached by the server rather than hotlinked by every
browser request. The cache lives under `artwork/` in the configuration
directory and is refreshed after 30 days; a failed refresh keeps the last good
image.

- Artist images are resolved from Deezer's public artist search using an exact
  normalized artist-name match, then cached.
- Album covers use the saved Cover Art Archive/provider URL first. If a local or
  imported album has no cover URL, an exact Deezer album search supplies one;
  MusicBrainz release groups fall back to the Cover Art Archive.
- Only fixed provider image hosts are accepted. Downloads are bounded, HTTPS
  only, redirect checked, and limited to JPEG, PNG or WebP.
- The UI reads `/api/v1/library/music/artists/{id}/image` and
  `/api/v1/library/music/albums/{id}/cover`, so the authenticated same-origin
  image endpoints work without adding external hosts to the browser CSP.
- The Subsonic-compatible `getCoverArt` action serves the same cached album
  cover.

### Finalize cohort and large-scan review

A finalize request now records the exact matched files that were reviewed. The
durable worker registers and organizes only that cohort; files registered by
earlier scans are never re-organized just because a new finalize request runs.
The cohort survives restart, so a resumed finalize continues with the reviewed
files instead of walking the whole registered library.

The import page has a scan-file filter for Unresolved, Matched, Registered,
Skipped or All. A very large scan opens on Unresolved automatically, while
small scans keep the All view. The filter is a server-side query, so it does
not page through thousands of unrelated files. The API uses
`GET /api/v1/library/music/scans/{id}?page=&state=`.

### Unified music library and batch downloads

The Music index is one library page with Albums, Artists and Tracks views, matching the Movies/TV/Anime tile layout. The Albums view is the default and includes saved albums with no registered files; Tracks lists only registered tracks. Search, A-Z grouping and pagination stay on the same page.

Album tiles and the album page can queue an automatic album download. Missing tracks are matched, downloaded and imported by the durable worker; existing files are skipped and never replaced. The artist page lists the full Deezer discography even when an album is not in the local catalog, and each missing album can be downloaded. Artist top tracks can be selected in bulk and queued as one acquisition job. The confirmation popup shows the queued job's live progress, and the artist, album and library pages keep a progress panel (state, percentage, current track and failed tracks) that polls `GET /api/v1/library/music/acquisitions/{id}` until the job completes, so a download stays trackable without leaving the page.
Artist top tracks use Deezer's top list first, then Deezer track search, then
iTunes search when the earlier providers return no usable tracks. The discovery
page shows active jobs and any job that needs attention, so normal processing
needs no user intervention.

## Scan stability on network and FUSE folders

Audio observations now compare file identity, size, nanosecond mtime and the
sampled/full digest while ignoring a change timestamp that some network and
FUSE filesystems update when a read refreshes access time. This prevents healthy
files from being reported as "changed while scanning" or failing organization
verification after a probe. A full SHA-256 is still computed before any
organization write, and a real size/mtime/digest change still fails closed.

When Finalize cannot verify a registered file, the workflow now names the file,
states that nothing was overwritten, and tells the owner to rescan it or run
Finalize again. Files that were already registered or organized are left in
place; the remaining matched files can be retried without repeating successful
work.
