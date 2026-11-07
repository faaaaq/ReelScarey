# TV downloads and safe imports

Torznab setup can be deferred. TV catalog/monitoring remain usable without an
indexer; release search explains the missing connection and does not show fake
results. When ready, use the existing Torznab and qBittorrent settings and shared
download mount described in MOVIE_WORKFLOW.md. No second service is required.

Load a show's season episodes first. Find episode releases searches one missing
episode, even if monitoring is paused. Find season releases targets the selected,
missing episodes of a monitored season/show. Search uses canonical show title
plus Sxx or SxxEyy, generic Torznab text search and TV category 5000. The indexer's
advertised limits, timeouts, same-origin metainfo fetch and no-redirect behavior
are shared with movies. The browser submits only an opaque release ID when queuing.
Review release titles yourself; automatic matching/ranking is not implemented.

The queued target snapshot contains episode IDs, provider identities, season/
episode numbers and titles. Each missing episode is reserved transactionally.
Separate downloads can run for nonoverlapping episodes of one show; overlaps and
already imported episodes are rejected. The same torrent hash cannot be attached
to a different target set. An identical retry returns its existing job. This avoids
adopting or editing unrelated qBittorrent torrents. Loss of an add response uses
the existing hash/tag reconciliation and durable worker leases.

Completed TV downloads stop for review before any media write when the mapping
is not unambiguous: more than one completed video, more than one reserved
episode, a replaced registered version, or a filename without exact episode
evidence all keep the explicit review. Activity lists complete, selected,
supported videos, excluding archives/samples/trailers. The path snapshot must
remain in the job's dedicated download directory. Filename patterns S01E01,
S01E01E02, S01E01-E03 and 1x01-02 provide suggestions only; wrong seasons,
ambiguous tokens, nonconsecutive groups and unknown episodes produce no
suggestion. Date/absolute-number filenames can be assigned manually.

Assign every target episode to a listed file or explicitly skip it when a
review is required. Unambiguous downloads map themselves: the server records
the same mapping the review would have confirmed, journals an "imported
automatically" event, and runs the identical ffprobe verification and journaled
import. A shared file can cover up to 20 consecutive target episodes in one
season. Confirm that you reviewed the assignments, then select Confirm files &
import. At least one episode must be imported. The server validates the complete partition, rejects duplicate/
foreign targets and unknown file indexes, stores its own path/size snapshot, and
locks the mapping before import. Skipped episodes release their reservations.
Mapping cannot change once confirmed; stop tracking/review rather than altering
an existing import plan. No arbitrary source/destination path is accepted.

Settings offers standard or with-title TV names. New TV jobs snapshot the TV
folder, naming choice and canonical series identity; later settings changes do
not rename files or change in-flight jobs. Paths are:

`<TV folder>/<show> (<year>) [tmdb-<id>]/Season 01/<show> - S01E01[-E02][ - episode titles].ext`

Year is omitted if unknown. Unsafe filename characters are replaced and TV name
components are bounded in UTF-8 bytes. Specials use Season 00. A multi-episode
file is stored once, with multiple episode associations. Each video has a separate
plan/staging name identified by parent job and client file index. The shared
importer anchors filesystem access, rejects traversal/symlinks, requires FFprobe,
journals size/mtime/hash, prefers Linux hardlinks, resumes verified copies and
publishes exclusively without overwrite. Media filesystems must support the
existing staging-to-final hardlink publication. Sources remain unchanged.

File registration, episode associations and that file's committed journal phase
are transactional. The parent job completes only after all mapped files commit.
A later collision/error leaves earlier imports registered and visible. Retry
resumes remaining plans without reimporting completed files; confirmed mappings
and plans can recover with qBittorrent temporarily unavailable. Startup cleans only
redundant matching staging links. Stop tracking leaves torrents, sources and all
published/staged files in place. It releases active reservations; resuming may be
refused if another job has since claimed or imported a remaining episode.

Availability is registration data, not a continuous filesystem scan. Series
status available means some episode files exist; season counts/episode badges
show actual registered coverage. Deleting files outside ReelScarey is not detected
automatically. Provider identity changes at existing episode/season numbers stop
metadata refresh for review rather than silently changing associations.

Limits: at most 100 missing targets per job, 500 eligible videos per torrent and
20 consecutive episodes per shared file. Season catalogs still support up to
2,000 episodes, subject to provider response bounds. Multi-season packs, arbitrary
template expressions, replacement imports, existing-file scans,
archive extraction and playback are outside this milestone. Opt-in automation
can queue individual missing episodes and suggest resolution upgrades, while
retaining mandatory mapping review; see AUTOMATION.md. Split large or
multi-season releases into separate, explicitly chosen downloads.

Migration 005 preserves prior movie jobs, encrypted payloads, file IDs and import
phases while adding a default -1 file index for existing movie operations. Back
up the stopped config directory before upgrading; in-place downgrades are not
supported. Tests use disposable roots and generated media. The opt-in Linux real
client test downloads a generated season pack through its own authenticated
qBittorrent profile and localhost Torznab/web-seed fixtures, then reopens state
during download and after mapping confirmation. It never uses the owner's services.
See VALIDATION.md for executed results.
