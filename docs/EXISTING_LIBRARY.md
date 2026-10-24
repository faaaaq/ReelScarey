# Existing-library import (0.10 development)

Open **Import library** or **Import existing** from Movies/TV shows. This is a
reviewed, in-place registration workflow for media already inside the configured
media root. It does not move, rename, copy, replace or delete media files.

## Prepare and discover

Mount the existing library inside `REELSCAREY_MEDIA_DIR`, then set the movie and
TV folder names in Settings. The page displays the exact server paths it will
scan. Paths refer to the ReelScarey server/container, not the browser's computer.
Only these configured folders can be scanned; the API accepts a media type,
never an arbitrary filesystem path. Importing from external folders or the
downloads root by copying/moving files is outside this milestone.

Choose Movies or TV shows and click **Scan configured folder**. Nothing runs
automatically on first visit. Discovery reads names, size, modification time and
stable file identity, without hashing/probing every video. It skips hidden
entries, symbolic links, samples/trailers, non-regular/empty files, unsupported
extensions and already registered paths. Supported videos are MKV, MP4, M4V,
AVI, MOV, WEBM, TS and M2TS. Disc folders and archive extraction are not supported.

Discovery is durable and processes one directory at a time. Restarted work
resumes using a lease and unique discovered-path records. Stop discovery leaves
its previous results available for review; already queued registrations continue.
Limits per scan are 10,000 candidate videos, 2,000 folders, 200,000 examined
entries, 20,000 entries in one folder and twelve nested path separators. Hitting
a limit or inaccessible/changed folder produces an explicit failed/partial scan,
retaining previous results. It never silently labels a partial scan complete.
Each directory is read in batches of 128 entries. Twenty scan histories are kept;
forget a finished scan to make room. Forgetting review records never removes
registered files or their catalog entries and refuses active verification work.

## Review the identity

Open **Review match** for a discovered file. The original path and size are shown.
Filename hints only prefill a search; they never select a title or episodes.
Search/browse saved catalog titles offline, or explicitly search TMDb and choose
**Add & choose**. New canonical titles start with monitoring paused. A provider
outage does not prevent linking to a previously saved catalog title.

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
and checks discovery identity, size and modification time. It computes SHA-256,
inspects the video with FFprobe and hashes again to detect changes while probing.
The visible path and file identity are checked again before the atomic database
registration. Verification streams data with bounded memory, renews a lease and
has a thirty-minute overall deadline. FFprobe has its own thirty-second timeout.
Large videos may take time to read twice, particularly on network storage.

Registered paths are compared conservatively without case distinctions. Existing
movie files, episode registrations and active download reservations prevent a
conflicting import. These conditions and canonical episode identities are checked
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
deletions or renames. Reassigning/removing/replacing a registered file, multiple
versions of one movie, mass automatic matching and arbitrary-root browsing remain
future work. Ownership of filesystem mounts and their permission/access controls
remains the host administrator's responsibility.

## Data and recovery

Migration 009 preserves media file IDs, existing download provenance and all
episode/subtitle references, while allowing genuine existing files to have no
download ID. The migration runner checks all foreign keys before committing.
Discovery directories, review items, choices, fingerprints and leases live in
SQLite and are included in verified configuration backups. Restored workers stay
held behind the existing recovery review. Media root changes invalidate old scan
registrations and require a new scan. No new dependency is introduced.

Implementation follows Go's [traversal-resistant filesystem API](https://go.dev/blog/osroot)
and SQLite's [schema migration/foreign-key guidance](https://www.sqlite.org/lang_altertable.html).
