# Anime and media organization (0.16 development)

## Catalog and episode identity

Anime has a separate navigation entry and catalog, containing both movies and
episodic shows. Canonical `kind` stays movie/series and provider identity stays
TMDb. Upgrade never guesses Anime from language, genres or filenames. Search
uses TMDb's ordinary movie/show results; choosing Anime explicitly classifies a
new add. Duplicate adds retain their original section and monitoring choices.
Use Library section on a title's details page to review reclassification.

Loaded canonical episodes may have explicit unique absolute-number aliases,
1–100000. Blank means unknown; there is no inferred season ordering. The editor
pages episodes and retains hidden-season edits. Saving compares section and show
metadata revisions before replacing the complete alias set (maximum 5000).
Metadata identity changes are rejected rather than reinterpreting saved aliases.
Canonical episode/file IDs, monitoring and files remain unchanged. Changed
classification/aliases invalidate older editors and automatic queue decisions.

Anime episodes retain manual release search and reviewed file mappings. Version
0.17 adds [per-show opt-in automatic searches](ANIME_SEARCH.md) using canonical
season/episode labels or these explicit absolute aliases. Anime movies still use
ordinary movie automation. An explicit absolute alias can
suggest a completed filename mapping (including a bounded consecutive range or
v2 suffix), but every suggestion still requires confirmation. Conflicting,
unknown or ambiguous labels produce no suggestion. New jobs snapshot aliases;
later alias changes cannot rename an already queued job differently. Shared
files still require at most 20 consecutive canonical episodes from one season.
Existing-library automatic matching may use one unique exact normalized episode
title to map a file to a different TMDb season; it does not infer ordering from
aliases, AniList/Kitsu counts or title similarity. AniDB/AniList identity,
automatic alternate-order matching and cross-season packs are not implemented.
See [existing-library import](EXISTING_LIBRARY.md).

## Main root and collections

`REELSCAREY_MEDIA_DIR` selects the main media directory when the server starts.
Organize library displays this authoritative server path. It cannot be changed
by an API payload. Config and download roots must remain separate. Moving to a
different main root requires an offline, complete media-tree migration and
deployment change; the UI does not pretend that editing a path moves files.

Defaults preserve existing Movies/TV folder preferences and current naming.
Anime defaults to Anime/Movies and Anime/TV Shows. Mixed collections such as
Family or Kids also separate Movies and TV Shows leaves, so a media server can
use each content path independently. Up to 20 collections have stable keys,
display names, non-overlapping relative folders, compatible content types and
independent naming rules. Removing an assigned collection is refused.

Title details choose a future-import collection or follow the library section.
Inheritance is preserved through category changes; explicit assignments remain
pinned. Saving rules or assignments creates/moves no media. New movie and TV jobs
capture their rules in the queue transaction. Already queued jobs and previously
journaled imports retain their original destination behavior, including legacy
jobs created before this upgrade.

Existing-library discovery can select any configured compatible collection.
It snapshots that folder, keeps canonical movie/show registration review, and
never infers section/episode identities from the source collection. Register
first, then explicitly classify and review organization separately.

## Presets and custom templates

Plex uses `{tmdb-ID}`, Jellyfin `[tmdbid-ID]`, and Emby `[tmdbid=ID]` metadata
tags with title/year folders. Presets retain canonical Season 01/S01E01 labels
and multi-episode ranges. These are naming conventions, not claims of tested
playback-server scans. Sources: [Plex movies](https://support.plex.tv/articles/naming-and-organizing-your-movie-media-files/),
[Plex shows](https://support.plex.tv/articles/naming-and-organizing-your-tv-show-files/),
[Jellyfin identifiers](https://jellyfin.org/docs/general/server/metadata/identifiers/),
[Emby movies](https://emby.media/support/articles/Movie-Naming.html).

Custom rules specify movie relative path (without extension), show folder and
episode relative path inside it. Safe tokens include title/year/tmdb_id,
season/episode/episode_end (optional :02), episode_title/episode_range and
absolute/absolute_end (optional :03)/absolute_range. Double braces escape literal
braces. Absolute templates require explicit consecutive aliases for every mapped
episode. Include the appropriate range token for shared files. Unknown years
render as Unknown. Unsafe separators, traversal, device names, unknown tokens,
invalid UTF-8, overlapping roots and overlong components are refused. Token
values are sanitized; template expressions execute no code.

## Reviewed batch changes

Select registered files on the current page (50 shown; API maximum 100). A
persisted review lists exact old/new video and subtitle paths, sizes and no-ops.
Creating a review writes only its audit record; media stays untouched. Canceling
the UI review never queues a job. Applying requires explicit review confirmation.
Changed settings, item/category/assignment metadata, canonical episodes/aliases,
file identity/size/mtime/hash, subtitle inventory, active downloads or pending
subtitle work reject stale decisions. Destination conflicts never overwrite.
Selecting another collection also changes those titles' future-import assignment
when the batch commits; unselected files keep their current locations.

From a title's File organization panel, choose Move or organize existing files,
select a destination collection and review all registered files for that title.
The server snapshots up to 100 files across all pages, and rejects a review if
the registered set changes before commit. Larger titles can be moved in explicit
batches. This moves registered videos and recognized subtitle companions, not
unregistered files, artwork or unrelated extras.

Alternatively, choose **Review entire folder and extras** to move one title
directory with its existing names, nested extras, artwork and empty directories.
The review lists every directory and file, hashes file contents, refuses links,
shared registered titles and existing destinations, and rechecks the inventory
before committing paths. On one filesystem it uses a Linux atomic no-replace
rename. Across filesystems it creates a journaled, verified copy on the target
filesystem, publishes the complete folder without replacement, switches catalog
paths and then removes verified original links. Existing seeding links remain.
The destination needs enough free space for the copy. Titles whose registered files
span separate title folders need individual file reviews. The inventory is
limited to 2,000 entries and the review has a 30-second budget.

If a restart interrupts a rename, the worker checks the saved directory identity
and complete inventory before switching catalog references. A failed move whose
location cannot be verified keeps other media work paused until the saved job is
resolved. An unverified stage created immediately before a crash is retained for
manual review rather than assumed to belong to the job. No owner media is used
in automated tests.

The durable worker journals owned stages/output identities, prefers a hardlink,
otherwise copies to a verified stage, and publishes with no-replace hardlinks.
It verifies bytes before atomically switching all media/subtitle references and
the committed plan phase, then removes only verified old library links. Download
sources/seeding links, metadata history, original provenance and episode/file IDs
remain intact. Styled ASS/SSA and SRT/VTT companions follow the video's basename
while retaining suffixes. Subtitle inspection caches are invalidated.

A process-wide filesystem gate excludes ordinary import/discovery/subtitle work
during execution. Durable queued/running reservations retain this exclusion after
restart until the worker reconciles the journal. The normal OS config lock still
prevents two app instances owning the same configuration. Publication and cleanup
are separate recoverable phases; the UI shows recent jobs and can resume a saved
failed job, with server verification. A changed review needs a fresh plan.

A queued job can be returned to review only before file work has started. There
is no general rollback for an applied organization batch. Failed operations retain
valid canonical files and journal-owned stages/outputs for review rather than deleting
uncertain data. Matching-stem NFO/artwork/attachments or unrelated registered
sidecar locations block the individual-file operation. Whole-folder reviews have
the separate scope and limits above. Case-only renames are refused. Filesystems
without safe hardlink publication cannot perform the operation. External programs
modifying files are outside the app's process lock; any detected change stops
work and retains uncertain files. Large copies require sufficient free space.

## API and persistence

- GET library accepts kind=anime; movie/series collections exclude Anime. Counts
  add anime. Item reads add category and category_revision.
- GET/PUT `/api/v1/library/items/{id}/anime`: section and canonical alias editor;
  PUT compares revision/numbering_revision and replaces episodes aliases. Strict
  JSON maximum 512 KiB, at most 5000 entries. Ordinary APIs retain 16 KiB.
- GET/PUT `/api/v1/organization`: authoritative main_root, revision and libraries.
  PUT is revision-checked, strict JSON maximum 64 KiB.
- GET/PUT `/api/v1/library/items/{id}/organization`: effective collection and
  revision. Empty library_key restores section-based inheritance.
- GET `/api/v1/organization/files`: saved registered files, literal query,
  optional kind/media_id, stable 50-file pages. Reads do not scan disk.
- GET/POST `/api/v1/organization/plans`: last 20 saved reviews/jobs; POST accepts
  explicit file_ids and optional target_library, or media_id and a required
  target_library for a complete registered-title review (1–100 files). The two
  selections cannot be combined. Add `whole_folder: true` to review the entire
  title directory. Reviews have a 30-second preview budget.
- GET `/api/v1/organization/plans/{id}`; POST its `/apply` with `{}` queues or
  resumes the exact saved job. There is no select-all-matching endpoint.
- Existing-library POST accepts optional library_key; GET includes configured
  collections and their movie/show paths.

All application APIs remain authenticated, mutations require CSRF/same origin,
and unknown fields/trailing JSON are refused. Additive migration 011 introduces
section/absolute aliases; 012 introduces organization config, assignments,
immutable job snapshots and durable plan/output journals. Existing IDs, paths,
credentials and monitoring survive the schema-10→12 upgrade. Graphite/blue and
plain functional copy remain. IPTorrents/XSpeeds account testing stays deferred.

A registered file can be checked from **Organize library**. The check reads the
entire file and compares its SHA-256 with the saved registration; it reports a
missing file, changed content, or an unsafe path without changing the catalog or
media. Results are point-in-time, and a large file may take a long time to read.
This check is an inspection only.

## Reviewed video replacement and restore

An unregistered video found by a configured library scan can replace one
registered movie or episode file after explicit review. Choose **Replace a
registered file** on the discovered file, select one registered target, and
review both paths and every episode covered. The source must have the same media
type and file extension. A YouTube-origin target retains its source history for
rollback. Direct replacement from downloads is not yet supported.

The job verifies both files and the incoming video, makes independent copies, and
keeps the prior video under `.reelscarey-versions` in the media root. This can
require space for two full copies; retained versions are not automatically
removed. The selected source, torrent sources and existing external subtitles
are retained. Existing subtitles may be out of sync with the new video; incoming
sidecars are not imported. Embedded subtitle facts are refreshed.

Completed replacement jobs offer a reviewed restore of the prior version; the
current version is retained in turn. If a swap is interrupted after the original
path becomes empty, retry the saved job or choose **Restore original after
interruption**. Restoration refuses an occupied path and keeps the library
reserved while the result is uncertain. Include retained versions in media-root
migrations and backups.

On **Organize library**, select files on the current page and choose **Check
selected files** to inspect them one at a time. Each result is a point-in-time
checksum check; the batch can be stopped, and completed results remain visible.

Selected MP4, M4V, Matroska, MOV, AVI or WebM files, a whole title, or a whole folder can include
**Also update embedded titles after organization** in the review. The move
finishes first, then durable jobs sequentially write and verify a metadata-only
stage on the video's filesystem and atomically replace the original. The stage
temporarily needs free space equal to the video and is consumed by the rename.
The saved movie title or canonical series/episode labels supply the titles.
Folder extras are preserved and are not retagged.
A separate embedded-title review is also available for an individual file.

MOV, AVI and WebM use their own container muxers; M4V uses its iPod-compatible
muxer to retain its existing brand metadata. Rewrites preserve original timestamps
and refuse any change to packets, streams, chapters or non-title metadata. Format
support does not guarantee every codec combination can be rewritten: generated
AVI/AAC and WebM/Opus fixtures change packet timing and are rejected without
changing the original. AVI does not retain audio language labels in the fixture.
Transport streams (TS/M2TS) have no supported container title tag and remain
unavailable for this operation. No duplicate video copy is retained: each job
records the previous title, previous and new sizes/checksums in its plan
snapshot. Torrent seeding sources and external subtitles are preserved for all
supported formats. A completed metadata-only update cannot restore the old
container bytes, so reviewing another title update is required to change it
again.

Completed file, title and folder moves offer **Review undo move** in Recent
organization jobs. This creates a new reviewed job returning videos and subtitle
references to the saved original paths. Folder rollback includes the current
reviewed extras and empty directories, using the same verified copying and
recovery when crossing filesystems. Current collection assignments and naming
preferences remain in force for future imports. Occupied destinations, changed
episode mappings or changed registered media refuse rollback. Embedded-title
rewrites keep no video copy, so undoing a move restores paths but leaves the
current titles in place. If video versions changed after the move, restore those
versions first. No existing file is overwritten; undoing an undo also requires a
new review.

From a title's details, **Organize or remove this title** opens scoped controls.
**Review catalog removal** defaults to keeping files. Alternatively, move verified
registered videos and recognized subtitle companions to retained trash, preserving
unregistered extras, client sources and previous versions. Review every affected
path and the record count before applying. Catalog associations and dependent
download/import history are removed in one transaction; a title can be added again
and retained files explicitly registered. There is no permanent trash purge.
Reviews are bounded to 100 files/10,000 dependent records and reject pending work
or changes to the reviewed catalog/history. Failed cleanup keeps media work fenced
until the saved job recovers; failed-safe jobs require a new review.
