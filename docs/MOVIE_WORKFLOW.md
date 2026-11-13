# Movie releases, downloads and imports

ReelScarey supports one Torznab endpoint and one qBittorrent Web UI. Add a movie
through TMDb, choose Find releases in Movies, search, review the title/year,
then select Download. Activity shows durable progress and recovery actions.
Monitoring is saved; it does not schedule searches or choose releases.

## Connections and shared files

Enter the full Torznab API endpoint from Prowlarr, Jackett or a compatible
indexer in Settings. Public endpoints may omit the API key. Save & test checks
capabilities before replacing the connection. Generic search with q is required.
Searches use movie category 2000, extended attributes and at most 50 results,
respecting smaller advertised limits. A blank query uses the saved title/year.

Enter the qBittorrent Web UI URL, username, password and downloads root as
qBittorrent sees it. Web API 2.8.3+ (qBittorrent 4.3.3+) is required. Testing is
read-only: authentication and API compatibility. Global client settings and
existing torrents are not changed. TLS verification remains enabled, and
redirecting login pages/SSO gateways are not traversed automatically.

Configuration documents and credentials are encrypted using secrets.key. Status
never returns passwords or API keys. Leave a credential blank on an existing
connection to retain it. A changed URL/username requires re-entry, preventing
silent forwarding of old credentials. Failed validation preserves the old
configuration. Back up the database and secrets.key together before upgrades.

A Web UI URL provides control, not file access. Mount qBittorrent's downloads
on the ReelScarey server. For example, if qBittorrent sees `/downloads` and the
same folder is mounted at `D:/SharedDownloads`, enter `/downloads` in client
settings and set REELSCAREY_DOWNLOADS_DIR to `D:/SharedDownloads`. Windows
remote paths such as `D:/Downloads` are also supported on a Linux server.

The worker requests `<remote download root>/ReelScarey/<job id>` for each add,
with a unique job tag, automatic torrent management disabled for that add and
normal torrent verification enabled. Content must remain in this directory.
Compose mounts downloads writable (needed by the built-in clients) and media
writable; the `:ro` downloads variant is an optional hardening for external-only
clients. UID/GID 10001 (or your
override) must read downloads and create files in media. Native imports require
FFprobe on PATH; Docker includes it. Config/download/media roots must exist,
remain separate and not overlap after resolving configured root symlinks.
Never run two application processes against one config directory.

## Persistent download jobs

Selections expire after 30 minutes and are invalidated by a new search for that
movie or an indexer settings change. The browser receives opaque selection IDs
and display metadata. Secret-bearing URLs, magnets and torrent payloads remain
encrypted in SQLite. v1 and hybrid torrents are supported; v2-only metadata is
rejected. Hashes come from magnet BTIH or the original bencoded info dictionary.
Torrent fetches must remain on the configured indexer's origin. Redirects and
off-origin URLs are refused; use an indexer-proxied torrent or magnet instead.

Queue writes are transactional and unique by release, info hash and active movie.
Duplicates return the existing job; a hash cannot belong to two movies. The
worker checks the expected hash before submission, records its intent durably
before the POST, and tracks only the torrent with this job's unique tag.
Unrelated torrents are not adopted, modified or removed.

A lost add response is reconciled by hash/tag without resubmission. If the
torrent remains absent for 90 seconds, Activity asks for review. Explicit Retry
can submit the same hash again after checking the client. This is idempotent
recovery, not exactly-once network delivery. No generic in-memory queue is used:
the downloads table holds atomic claims, random owners, 60-second leases,
15-second renewals and fenced state writes. Jobs poll about every five seconds.
Temporary failures back off from five seconds to five minutes and need manual
retry after eight failures. Expired leases are reclaimed after a crash.

## Verified imports

Only complete torrents in a seeding/completed state are considered. A file must
be complete, selected, regular, and MKV, MP4/M4V, AVI, MOV, WebM, TS or M2TS.
Samples/trailers are excluded, and archives are not extracted. Multiple eligible
videos require an explicit file choice in Activity instead of a size-based guess.

Source/destination paths use anchored os.Root handles. Traversal, alternate data
streams and internal symlinks are rejected. FFprobe uses the already-open source,
pipe input only, bounded output and a timeout. A usable video stream and positive
duration are required. Size, modification time, SHA-256, probe details and
destination are journaled before writes. Paths are deterministic:
`<movie folder>/<sanitized title> (<year>) [tmdb-<id>]/<same name>.<extension>`.
Preferences apply when planning; changing them does not move files or an existing
import plan. Library folder settings alone do not scan or mutate files.

Linux first attempts a hardlink between anchored source/destination handles when
the **Library import method** is *Hardlink when possible* (the default, chosen
during first-run setup and editable in Settings). *Always copy* skips that
attempt. Other hosts, cross-device operations, read-only source mounts and
unsupported links use a staged copy. A partial copy resumes only after its prefix
matches the unchanged source. Full size and SHA-256 are verified before
publication.

Publication is an exclusive atomic hardlink from the verified staging file
inside the destination directory. It never overwrites an existing file. The
media filesystem must therefore support hardlinks (such as NTFS, ext4, XFS or
ZFS); unsupported publication stops safely and retains staging for review.
Download-to-library hardlinks additionally require both roots on one writable
mounted filesystem. The default container mounts are separate, so imports copy there even when the
preference is *Hardlink when possible*. Symbolic-link imports are not supported.
Linux syncs file/directory changes. Windows flushes staged file contents and
uses the same recovery checks without a directory-fsync guarantee.

Recovery recognizes a published file only when its journal/hash/file identity
match. File registration, available movie status, committed import phase and
completed job commit in one transaction. The redundant app staging link is
then removed. Committed leftover staging links are reconciled at startup.
Torrent sources are never renamed, rewritten or deleted and remain for seeding.

## Recovery and current limits

Activity distinguishes queued, sending, downloading, importing, needs attention,
failed, completed and tracking-stopped states. Fix connectivity, storage, FFprobe,
path mapping or client errors, then Retry. A changed source, mismatched partial
copy or destination collision requires manual review. Conflicting files are
never deleted to force success.

Stop tracking is available for blocked/failed jobs after their lease is released.
It changes local state only; the torrent, source and staged/published files stay
in place. Resume tracking reuses the original hash/journal. Client URL/root
changes and disconnection are refused while active jobs exist; stop tracking
those jobs first. Password rotation on the same endpoint is supported.

The remaining features are implemented and documented elsewhere: existing-library
scans in EXISTING_LIBRARY.md, replacement imports in MOVIE_VERSIONS.md, and
subtitles in SUBTITLES.md. Playback history is covered by the Emby integration
(EMBY.md), and movie removal from the library is described in UI.md. TV shares
this pipeline (TV_DOWNLOADS.md). Scheduled searches, ranked profile decisions, opt-in
missing-item downloads and resolution upgrade suggestions are described in
AUTOMATION.md.

## Checks and references

Automated checks exercise HTTP contracts, encryption, malformed feeds/metainfo,
redirect containment, deduplication, lost-response restart, lease fencing,
traversal/symlinks, partial-copy and publication restart, collisions, source
changes, file choice and real FFprobe. Linux also copies across an actual
filesystem boundary into a disposable /dev/shm root.

The opt-in command `REELSCAREY_LIVE_QBT=1 go test -race ./internal/transfer -run
TestRealQBittorrentDownloadAndImport -count=1 -v -timeout=5m` starts its own
authenticated qBittorrent-nox profile and Torznab protocol fixture. It downloads
a generated private video from a localhost web seed, reopens application state
during download, imports through FFprobe and verifies source retention. This
uses no owner's services/library and is not a public-indexer acceptance test.
See VALIDATION.md for executed results.

Contracts were checked against the [Torznab specification](https://torznab.github.io/spec-1.3-draft/torznab/Specification-v1.3.html)
and [qBittorrent Web API documentation](https://github.com/qbittorrent/qBittorrent/wiki/WebUI-API-%28qBittorrent-5.0%29).
Compatibility also covers current upstream [session-cookie/204 handling](https://github.com/qbittorrent/qBittorrent/blob/master/src/webui/webapplication.cpp)
and structured/async torrent-add responses. An authenticated session cookie is
still required, and payload hashes are rechecked immediately before submission.
No implementation code was copied from the media applications inspiring this project.
