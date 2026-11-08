# Existing-library import

Open **Find unmatched files** or **Import existing** from Movies/TV shows. This is a
matching and in-place registration workflow for media already inside the configured
media root. It does not move, rename, copy, replace or delete media files.

## Prepare and discover

When adding a movie or show from its search details, you can choose **Create the
destination folder now**. ReelScarey uses the current library and naming rules;
it leaves existing files untouched. Folder creation is a separate step after
adding the title, so a permissions or naming error does not undo the add. A
movie naming template must provide a separate title directory for this option.

Mount the existing library inside `REELSCAREY_MEDIA_DIR`, then set the movie and
TV folder names in Settings. The page displays the exact server paths it will
scan. Paths refer to the ReelScarey server/container, not the browser's computer.
Only these configured folders can be scanned; the API accepts a media type,
never an arbitrary filesystem path. Importing from external folders or the
downloads root by copying/moving files is outside this milestone.

Choose Movies, TV Shows, or Anime under **Look for** and click **Scan and match library**. Automatic matching
is checked by default; uncheck it for discovery and manual review only. Nothing runs
automatically on first visit. Discovery reads names, size, modification time and
stable file identity, without hashing/probing every video. It skips hidden
entries, symbolic links, samples/trailers, non-regular/empty files, unsupported
extensions and already registered paths. Supported videos are MKV, MP4, M4V,
AVI, MOV, WEBM, TS and M2TS. Disc folders and archive extraction are not supported.

**Music** is listed in the same Discover panel and opens the dedicated
tag-based **Music Import** scanner. Music files are matched to MusicBrainz
artists, albums and tracks rather than TMDb video metadata; see
[Music](MUSIC.md).

Discovery is durable and processes one directory at a time. Restarted work
resumes using a lease and unique discovered-path records. Stop discovery leaves
its previous results available for review; already queued registrations continue.
There is no total cap on videos, folders or examined entries per scan. Individual
folders are still limited to 20,000 entries and paths to twelve nested separators.
Hitting either safeguard or encountering an inaccessible or changed folder produces an explicit failed/partial scan,
retaining previous results. It never silently labels a partial scan complete.
Each directory is read in batches of 128 entries. Twenty scan histories are kept;
forget a finished scan to make room. Forgetting review records never removes
registered files or their catalog entries and refuses active verification work.
Each scan shows counts of files needing a match and needing attention; the review
opens to files needing a match. Counts are snapshots from that scan, not a live
filesystem inventory. Run a new scan to discover later changes.

## Automatic matching (0.27)

New scans can automatically register clear matches across the entire library.
For an older completed scan, choose **Match clear files automatically**. Opening
this page never starts matching. Existing API callers remain discovery-only unless
they explicitly send `automatic_matching:true`.

The movie/show title folder is authoritative, including when release filenames
use an alternate, abbreviated or obfuscated title. Its match is cached for the
scan and shared across episodes; loaded seasons are reused. An explicit
`[tmdb-123]` / `{tmdb-123}` ID identifies that provider item without title search
or a repeated details fetch. Without an ID, matching requires one unique exact
normalized title (and year when supplied). Loose movie files still require a year
or TMDb ID. TV uses explicit canonical season/episode labels such as `S01E02`,
`1x02`, or `S01E02-E03`. Conflicting explicit IDs, years or season numbers,
ambiguous title results, date/absolute Anime numbering and nonconsecutive episodes
stay in review. Multiple movie files can attach to the same movie, preserving each
resolution or edition. Overlapping TV episode files stay in review; the app does
not choose an arbitrary episode version. Clear canonical Anime numbering works
in the configured Anime collection. Anime discovery scans series directly in the
configured Anime folder (for example, /data/anime). Scan films through Movies,
including anime films.

Matching first checks saved titles, then TMDb when needed. New titles default to
future-only monitoring: a title releasing today or later is monitored, while an
already-released or undated title is paused. Unloaded canonical season episodes
are fetched once and reused. Existing monitoring choices are preserved.
Movie/show searches are
bounded to three complete provider pages; broader results need review or an
explicit TMDb folder ID. Incomplete saved season definitions may need refresh.

Progress separates files already in the library, matched files waiting to attach,
unmatched files and attention items. Matched files have their own filter and no
longer count as needing a match. A whole-scan duplicate check precedes queueing;
metadata resolution/queueing then runs independently of the single disk worker.
The bounded file/FFprobe inspection below runs before availability changes.
**Stop automatic matching** prevents further matching writes; already queued file
verification continues. Network failures retry after a minute and pause after five
consecutive failures. Correct the connection and use the matching button to retry.
Jobs resume after ordinary restarts. Restoring a backup pauses them for explicit
restart. Failed/partial discovery cannot automatically register files.

## Review the identity

Open **Review match** for a discovered file. The original path and size are shown.
Filename hints only prefill a search; they never select a title or episodes.
Search/browse saved catalog titles offline, or explicitly search TMDb and choose
**Add & choose**. New canonical titles default to future-only monitoring; an
already-aired title starts paused. A provider outage does not prevent linking to
a previously saved catalog title.

For TV, choose a show, load a season's canonical episode metadata if necessary,
and select every episode covered by this file. One file can cover up to twenty
consecutive episodes in one season. Absolute anime numbering and uncertain
season mappings are not inferred. Already registered episodes cannot be chosen.

Check the explicit identity-review box and choose **Verify & add in place**.
The request stores catalog/episode identities and queues durable verification.
It never accepts a client-supplied path. Skip leaves a review item out of the
library. A blocked file can be reviewed/retried after the reported problem is
resolved; a changed source requires a new scan.

## Verification and registration

The worker opens the file through an anchored `os.Root`, rejects link components
and checks discovery identity, size and modification time. It records file identity,
size, modification/change timestamps and a SHA-256 of three 64 KiB samples
(beginning, middle, end), then inspects the video with FFprobe and checks the
observation again. The visible path and identity are checked immediately before
the atomic registration. This is bounded change detection, not a full-content
integrity claim. Files up to 192 KiB retain an actual full SHA-256 directly.

Reviewed organization appends the inspected quality tag to every renamed file:
resolution plus an explicit HDR or SDR transfer label (for example
`[1080p HDR]`). A review refreshes one missing transfer fact through a bounded
FFprobe read before naming; files probed before HDR detection therefore acquire
the transfer label on their first reviewed rename. If that probe fails, the file
keeps resolution only, and unverified files carry no quality tag at all — the
catalog never claims a quality it did not measure. Embedded-title updates replace
the file in place and record the previous title and old/new checksums; no
duplicate video copy is retained. When the owner preference **Register videos that
ffprobe cannot verify** (Settings → Library preferences, default off) is
enabled, a file whose video cannot be inspected is still registered: it
records no resolution or embedded-title facts, its review row says "Added
without video verification", and embedded-title rewrites and subtitle matching
refuse it. Files ffprobe reads but finds no video stream in are never
registered. Replacing a corrupt file and reviewing the new copy is usually the
better fix.

Linux FFprobe reads the already-open descriptor as a seekable regular file,
including MP4/MOV metadata at the end. Only supported video demuxers and local
file access are permitted; playlists, network protocols and external MOV track
references are disabled. Its timeout remains thirty seconds. No video is copied,
renamed, rewritten, or read twice end to end just to add it to the library.

For larger existing videos the full SHA-256 is deferred. Explicit organization,
replacement, trash, embedded-title and integrity-check operations establish it
from an unchanged observation before their usual full-checksum review/journal
safeguards. Offline root migration establishes full digests before copying.
Subtitle inspection uses the saved observation and its provider-specific small
movie hash, so enabling subtitles does not reintroduce a full-library checksum
read. Old registrations and downloaded imports keep their existing full hashes.
There is no background full-hash sweep after a scan.

Already authorized scans and queued registrations resume after upgrading.
Previously reviewed exceptions can be retried with **Match clear files
automatically**; a new scan skips paths that are already registered.

Registered paths are compared conservatively without case distinctions. Existing
paths, episode registrations and active episode download reservations prevent a
conflicting import. Separate movie copies can register even while a movie download
is active. These conditions and canonical episode identities are checked
again inside the registration transaction. An expired/lost lease cannot commit.
Crashes before commit retry verification; a committed registration is not added
twice. This workflow writes application state only. Normal filesystem reads may
update access time; file contents, names and modification times are preserved.

The detail page shows **Registered in its existing location**, original filename,
path, verified size/technical facts and registration time. No download, indexer
release or Activity job is fabricated. Existing files use the same availability,
quality-upgrade suggestions and subtitle inspection as downloaded files. A local
filename is not treated as verified original-release evidence: automatic subtitle
selection requires provider hash evidence; uncertain timing remains manual.
Your configured subtitle preferences may therefore create new sidecars after
registration, while existing videos/sidecars remain protected.

The page records an import snapshot; it does not continuously detect later edits,
deletions or renames. Reviewed file replacement/removal and multiple movie versions
are supported; see [movie versions](MOVIE_VERSIONS.md). Reassigning a registered
file to another title and arbitrary-root imports remain future work.
Ownership of filesystem mounts and their permission/access controls
remains the host administrator's responsibility.

## Data and recovery

Migration 043 adds `verification_json` to preserve deferred observations across
restart, backup and restore without altering existing hashes or references.
Migration 009 preserves media file IDs, existing download provenance and all
episode/subtitle references, while allowing genuine existing files to have no
download ID. The migration runner checks all foreign keys before committing.
Discovery directories, review items, choices, fingerprints and leases live in
SQLite and are included in verified configuration backups. Restored workers stay
held behind the existing recovery review. Media root changes invalidate old scan
registrations and require a new scan. No new dependency is introduced.

Implementation follows Go's [traversal-resistant filesystem API](https://go.dev/blog/osroot)
and SQLite's [schema migration/foreign-key guidance](https://www.sqlite.org/lang_altertable.html).
