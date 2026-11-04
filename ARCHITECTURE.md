# Architecture

## Foundation decision

One Go process owns a REST API, an embedded React application, and SQLite.
The application is deployed as one container, without Redis or another database.
Go's standard HTTP library supplies routing, cancellation, and timeouts. SQLite
uses modernc.org/sqlite so builds do not require CGO; the tradeoff is a larger
binary than a native SQLite binding. A single database connection serializes
access for this initial low-throughput milestone. WAL and a busy timeout allow
external backup/read tools without immediately failing on transient contention.

React + TypeScript + Vite implement the UI. Tailwind supplies layout utilities;
Radix Themes supplies accessible form and action primitives. React Router owns
navigation and deep links. Frontend build output is embedded in the Go binary,
so the production API and UI always share an origin and version.

## Directory structure

```
cmd/reelscarey/       process lifecycle and healthcheck command
internal/config/     environment validation
internal/database/   SQLite connection and transactional SQL migrations
internal/settings/   validated, persisted library preferences
internal/library/    unified media item read model
internal/auth/       administrator, Argon2id passwords and persistent sessions
internal/securestore/ encrypted provider credentials and local key lifecycle
internal/metadata/   real TMDb adapter and normalized movie service
internal/transfer/   Torznab/qBittorrent, durable downloads and safe imports
internal/quality/    conservative identity, profile persistence and release ranking
internal/automation/ persistent scheduled searches, audit history and policy fences
internal/subtitle/   provider, track preferences, registered-media jobs and sidecars
internal/youtube/    reviewed public source inspection, acquisition and imports
internal/requests/  requester-owned requests, list imports and webhook delivery
internal/emby/      encrypted Emby connection and read-only playback snapshots
internal/instance/   OS config ownership and maintenance locks
internal/maintenance/ verified offline config snapshots and new-folder restores
internal/backups/    live ZIP snapshots, persisted schedules, retention, restore journal
internal/operations/ explicit installation checks, setup and recovery review
internal/httpapi/    REST handlers, middleware, frontend serving
web/src/             application, pages, reusable UI, API client
web/dist/            generated frontend (ignored, embedded at build time)
docs/                API and operational documentation
.github/workflows/   build, tests, container smoke check
```

## Data model

Migration 031 adds the paused-by-default singleton `backup_schedule`. Backup ZIPs
and strict sidecar metadata live in a separate persistent volume. The composition
root owns the controlled restart: it drains HTTP, joins every worker and closes
SQLite before applying a durable, rollback-capable configuration file journal.
See [backup design and recovery](docs/BACKUPS.md).

Implemented tables: `schema_migrations`, singleton `settings`, `media_items`,
singleton `admin_account`, `sessions`, `provider_secrets`, `integration_configs`,
`release_candidates`, `downloads`, `import_operations`, `media_files`,
`series_details`, `seasons`, `episodes`, `download_tv`, `download_targets`,
`episode_downloads`, `download_tv_files`, `download_mappings`, `episode_files`,
`quality_profiles`, `item_automation`, `search_schedule`, `automation_runs` and
`automation_results`, `subtitle_preferences`, `subtitle_provider_state`,
`subtitle_jobs`, `subtitle_candidates`, `subtitle_inspections`, `subtitles` and
`subtitle_imports`, `indexers` and `media_enrichment`.

Migration 009 adds `library_scans`, `library_scan_dirs` and `library_scan_files`.
Existing videos register in `media_files` with explicit existing-file provenance
and no fabricated download. A transactional parent rebuild retains all IDs and
episode/subtitle references; the runner checks foreign keys before commit.
Manual discovery and reviewed verification use persistent leases in transfer,
with a separate worker in the same Go process and the shared FFprobe/hash helpers.
Registration updates availability atomically without publishing/moving files.
See docs/EXISTING_LIBRARY.md for bounds, review and identity checks.

Migration 010 retains encrypted integrations/indexers while adding Newznab and
SABnzbd. Old jobs default to torrent; jobs retain protocol, client ID and immutable
completed-output snapshots. Newznab shares caps/search but validates NZBs
separately. SABnzbd is a distinct adapter; only submission/reconciliation branch,
with shared review/reservations/import and the same process/SQLite leases.
NZB descriptor checksums use an `nzb:` namespace. Uncertain submissions reconcile
without automatic resubmission. Completed post-processing and exact dedicated
mapped output are mandatory. No client deletion/script execution API exists.
See docs/USENET.md.

Migration 038 adds named download clients and per-protocol defaults while
preserving legacy encrypted connections and connection-history triggers. New
jobs snapshot the selected client and remote/local folder mapping; transactions
fence defaults, profile revisions and credentials after release fetches. NZBGet
uses positional authenticated JSON-RPC and the same Usenet reconciliation,
reservations and importer. See docs/DOWNLOAD_CLIENTS.md.

Migration 039 adds the global subtitle output preference, defaulting existing
installations to SRT. Original ASS/SSA/VTT selections retain their extension in
the immutable destination journal. The same content/format checks and importer
serve manual and automatic choices; pending bytes recover independently of later
format changes. OpenSubtitles conversion is omitted for styled original files.

Wanted and Calendar are transactionally consistent read models owned by library,
combining movie/loaded-episode metadata, registration existence and active target
reservations. HTTP owns strict filter validation and a five-second read budget;
the UI uses bookmarkable filters and explicit existing workflow links. No provider,
worker or filesystem operation is started by these reads. Calendar dates remain
civil dates; UTC supplies the comparison day. Invalid saved dates stay undated
and cannot authorize automation. This adds no tables/migrations/dependencies;
schema remains 10. See docs/PLANNING.md.
Library browsing extends the existing paginated read with strict literal-title,
parent-status/monitoring filters and allowlisted stable ordering. Counts and rows
share a transaction; the UI keeps filters/view/page in the URL. Poster/table view
changes are local presentation only. The overview reuses planning reads without
starting work. No migration or application dependency is needed.
See docs/LIBRARY_BROWSING.md.
Reviewed bulk monitoring remains a bounded library transaction over explicit IDs.
The complete parent monitoring/series/assignment snapshot is checked before any
write. Existing series and automation revisions fence old editors and automatic
queue decisions; explicit profiles and TV child selections are preserved. No
worker, provider, filesystem operation or new migration is involved. The UI
selection is page-scoped and requires review before apply. See docs/BULK_MONITORING.md.
Quality exposes transaction-aware profile reads. Catalog profile context and
effective-profile filters share the catalog transaction; automation owns reviewed
profile assignment. Apply checks the complete assignment/current-rule/target-rule/
schedule snapshot before changing profile_id/revision. Changed assignment revisions
fence editors and pending queues; monitoring and files are untouched. No migration
or dependency is required. See docs/BULK_PROFILES.md.
The migration ledger stores version, filename, checksum, and applied time.
Additive migration 011 introduces explicit Anime classification and canonical
episode absolute aliases without changing movie/series identity. Migration 012
adds organization settings/assignments, immutable queued naming/alias snapshots
and plan/output journals. Organization owns review, safe publication and batch
reference switching; transfer consumes its narrow destination planner. Shared
filesystem exclusion and durable pending-plan checks coordinate imports,
existing discovery and subtitle workers through restart. Registered paths remain
relative to the deployment-selected main root. See docs/ANIME_ORGANIZATION.md.
Migration 013 adds per-show opt-in Anime search settings and immutable download
search snapshots. Quality matches one exact episode against explicit identity
labels; automation delegates bounded queries to transfer and checks policy,
category and numbering revisions again inside queue reservations. Existing shows
remain disabled; canonical episode IDs and reviewed imports are retained. See
docs/ANIME_SEARCH.md.
Migration 014 adds scoped AniList identity snapshots, explicit typed release-order
entries and expiring server-side reviews. TMDb group proposals join exact loaded
canonical episode IDs; AniList counts/titles never infer an episode graph. Apply
compares order/category/metadata revisions and preserves existing absolute aliases.
Transfer snapshots reviewed manual target sets and provider facts, permits packs
across seasons through separate files, and rechecks the order inside reservations.
Each shared file retains the same-season consecutive mapping constraint; ordinary
automatic episode rules remain separate. See docs/ANIME_PROVIDER_ORDERS.md.
Migration 015 adds explicit mapped-search consent, optional specials and bounded
packs. Automation searches reviewed typed label cohorts and rechecks the exact
order, policy and eligibility in the queue transaction. Completed downloads
still require file mapping review. See docs/ANIME_SEARCH.md.
Migrations 016–019 add distinct YouTube collections and reviewed source jobs,
leased acquisition, additive provenance and journaled subtitle sidecars. Saved
videos link explicitly to a catalog target or a custom collection; they never
fabricate metadata-provider IDs. YouTube reservations fence the shared import
paths, and publication remains reviewed, verified and no-overwrite. See
docs/YOUTUBE.md for setup, limits and recovery.
Settings changes are atomic and carry a revision for optimistic concurrency.
Media items share `kind` (movie/series), TMDb identity, title, year, monitored
flag, and status. `(kind, tmdb_id)` is unique; a movie and series may have the
same TMDb ID. Migration 002 adds overview, artwork path, release date and runtime.
An authenticated add accepts only a TMDb ID and monitoring preference, retrieves
canonical details from TMDb, and inserts transactionally. Duplicate adds return
the existing item without changing metadata or monitoring, even during a provider
outage. Collections have stable title/id ordering, totals, and 24-item pagination.
Movie adds do not change files. A separately selected release creates a durable
download job. Verified import registers a media file and marks the movie available.
Migration 003 adds this without altering prior migrations or account/provider state.

Migration 004 adds TV details, season summaries and episodes. TV uses the same
catalog item and encrypted TMDb connection. Adding a show fetches canonical series
metadata once and transactionally inserts its summaries; season episodes load on
explicit request. Show-level revision checks serialize all metadata refreshes and
monitoring changes. Parent monitoring switches gate effective episode monitoring
without changing child selections. Refresh upserts metadata by season/episode
number while retaining selections and rows absent from later provider responses.
Migration 005 generalizes the shared download/import pipeline for TV. The existing
downloads.movie_id column remains the parent catalog ID for compatibility; kind
distinguishes movies from series. TV jobs snapshot canonical episode targets and
naming preferences, reserve episode IDs, and require a confirmed mapping from
completed client file indexes to a complete partition of imported/skipped targets.
One file can map to consecutive episodes. Each file has a journal keyed by parent
download and file index; existing movie journals retain default index -1. Existing
movie file IDs, encrypted payloads, leases and journal phases are migrated intact.
Each file registration commits with its episode associations and journal phase;
the parent completes after all files commit. A later failure retains previous
imports and resumes the remaining files. Availability derives from episode_files.

Planned entities (design, **not implemented schema**):

| Entity | Relationships and invariants |
| --- | --- |
| jobs | Future arbitrary job types; searches and downloads already have durable leases/retries |

Media file paths are never derived from untrusted release names without
normalization and root containment checks. Downloads remain separate from files:
an imported file can survive removal of download-client history.

## Module boundaries and planned interfaces

Migration 008 adds managed `indexers`, cached `media_enrichment` and historical
download source names. Transfer selects the saved adapter for every encrypted
release; revision/generation checks invalidate stale selections. Native IPTorrents,
XSpeeds, NYAA and private RSS share bounded multi-source search with Torznab and
automation. Metadata enrichment stays behind the metadata provider boundary.
Local details read verified import records, counting shared TV files once, and
remain available independently of providers. See docs/INDEXERS_DETAILS.md.

TMDb MovieProvider implements Validate/Search/Movie; SeriesProvider implements
SearchSeries/Series/Season using the same bounded HTTP client, rate-limit cooldown
and encrypted credential. The library owns catalog adds, reads and TV monitoring.
Transfer's Indexer and Client interfaces implement
capability tests, search/metainfo retrieval and authenticated add/info/files.
The broader contracts below include future scope. Interfaces accompany tested
consumers. Every operation receives a context and returns typed results/errors.

| Module | Responsibility | Primary service contract |
| --- | --- | --- |
| Metadata | TMDb mapping, artwork, provider rate limits | Search(kind, query, page), Get(kind, providerID) |
| Library | Catalog, monitoring, seasons/episodes, availability | Add(metadata, profile), List(filter), SetMonitored(id, value) |
| Indexers | Torznab/Newznab protocol, credentials, capabilities | Test(), Capabilities(), Search(query) |
| Releases | Parse, match and rank normalized releases | Match(release, target), Score(match, profile) |
| Downloads | qBittorrent/SABnzbd adaptation, lifecycle reconciliation | Test(), Add(release, category), List(), Get(clientID) |
| Import | FFprobe, safe naming, hardlink/copy, crash journal | Plan(download), Validate(plan), Execute(plan) |
| Subtitles | Detection, provider search and scoring, safe storage | Search(file, languages), Fetch(candidate) |
| Automation | Durable claims, retries, scheduling, deduplication | Enqueue(type, payload, key), Claim(), Complete(), Retry() |
| Settings | Preferences, provider configuration and secret references | Get(), Update(expectedRevision, preferences) |

HTTP handlers orchestrate application services; provider adapters do not import
HTTP transport code or write library rows directly. Metadata DTOs and normalized
release/download types belong to their consuming modules, not a giant shared
utility package. Future music/books can extend media kinds through a migration
without adding empty feature packages now.

## Persistent automation

Jobs live in SQLite and use atomic claims with expiring leases. Restarted workers
reclaim expired work; retries use capped exponential backoff and bounded attempts.
Leases must be renewed during long work. Deduplication keys prevent concurrently
scheduled duplicates; handlers reconcile external client state before retrying
side effects. This is at-least-once execution with idempotency, not exactly-once.
The movie slice introduces the first actual worker for download reconciliation
and import. A single worker now reconciles downloads using random owners,
60-second leases renewed every 15 seconds and fenced state changes. Temporary
failures back off; ambiguous submissions reconcile by expected hash/tag.
Migration 006 adds a singleton interval schedule, editable resolution/language
profiles, item assignments and bounded run/result history. Searches have a separate
leased task lifecycle in the same process/database and delegate all download work
to transfer. An atomic claim captures settings revision, issues a 60-second lease
renewed every 15 seconds, and records an auditable run. Cursor rotation bounds each
run to 1–25 known released targets. UTC calendar dates gate monitoring; missing
dates and unloaded TV metadata are not inferred. Failed requests back off for up
to five attempts; interrupted runs are reclaimed. History retains 100 runs.

Search mode only records ranked decisions. Download mode selects conservative
exact title/year or individual-episode matches; ambiguous identities, packs,
unknown quality/size/seed count and disallowed profile attributes are rejected.
Transfer executes an automation-supplied policy guard inside the download target
reservation transaction after metainfo fetch, fencing expired leases, paused/
changed schedules, monitoring changes and profile/assignment revisions. Existing
hash/target reservations and client-tag reconciliation prevent duplicate effects.
TV downloads retain the mandatory reviewed mapping before any import.

Resolution upgrades compare stored FFprobe dimensions to the current profile
cutoff. Below-cutoff files can generate suggestions; no replacement download or
file mutation is performed. Labels establish selection evidence only, never
verified audio language. Generic arbitrary jobs, replacement imports and advanced
source/codec scoring remain future scope. See docs/AUTOMATION.md for exact limits.

TV shares the same client adapter, submission deduplication, leases, filesystem
importer and startup staging cleanup. It adds episode reservations, file snapshots
and reviewed mappings, rather than creating a separate queue or importer. Imports
resume from confirmed snapshots without requiring an available client API. Stopped
tracking releases reservations; retries reacquire them and reject conflicts with
newer jobs/files. TV metadata refresh rejects changed provider identities at known
numbers instead of silently redirecting existing associations.

Import plans journal the anchored source, deterministic destination, size,
mtime, SHA-256 and FFprobe data before writes. Linux prefers anchored hardlinks;
other hosts and cross-device operations stage verified copies. Partial copies
resume after prefix validation. Exclusive hardlink publication cannot overwrite
an existing file, so the media filesystem must support this operation. Sources
stay untouched. File registration, available catalog state and job completion
commit together. See docs/MOVIE_WORKFLOW.md for guarantees/platform limitations.

## Subtitles

Migration 007 adds subtitle preferences, per-file leased work, expiring selections,
inspection fingerprints, detected/downloaded tracks and per-track import journals.
The encrypted integration table is extended without changing existing ciphertext.
One subtitle worker discovers registered movie/episode files in batches of 50;
it never recursively scans arbitrary library contents. Global language/variant
preferences start paused with English full, signs/songs and forced tracks.

The first provider is OpenSubtitles.com. Requests use fixed official HTTPS API
hosts, serialized one-second spacing, bounded bodies, safe errors and no API
redirects. Temporary subtitle links and redirects use an explicit HTTPS CDN
allowlist without API/account headers. Account/API credentials are encrypted by
securestore; login tokens remain in memory, signed URLs are not persisted.
Quota/rate cooldowns and rejected-account states persist across restarts.

FFprobe identifies embedded subtitle language/title/disposition. Anchored reads
detect existing sidecars before network requests. A first inspection verifies the
registered video SHA-256; unchanged size/mtime and registered SHA reuse that cached
inspection. Automatic selection requires provider catalog identity and a file-hash
or exact original-download release match; shared episode files require a hash.
Uncertain timing is manually reviewed through a 30-minute opaque selection bound
to the preferences revision, provider generation and registered video SHA.

New downloads are bounded UTF-8 SRT. Verified bytes are journaled before staging,
published by exclusive same-folder hardlink and registered transactionally.
The video is never written. Existing destinations, invalid sidecars and unknown
stages remain untouched. Partial stages allocate a fresh journaled stage rather
than truncating existing data; published journals recover without provider calls.
Only proven committed stage/destination links are cleaned. See docs/SUBTITLES.md
for exact format, matching, filesystem and recovery limits.

## Configuration and security

Migrations 029–030 add `acquisition_replacements` and private list provider modes.
Manual acquisitions bind the existing registered-file SHA and exact episode set
inside their reservation transaction. Transfer and YouTube publish verified
candidates into retained versions, then stop for a separate organization review.
The existing organization replacement journal publishes independent copies and
switches provenance/availability atomically, preserving canonical file IDs and
episode associations. Existing automatic upgrade searches remain suggestions.
Private list tokens are encrypted with the per-list securestore context and use
fixed TMDb/Trakt origins. Rebuilding the list table restores its event triggers.

Catalog removal is another immutable organization plan. A bounded foreign-key
dependency closure captures row IDs and content hashes without exposing payloads.
Apply compares the entire closure; commit deletes dependent rows in one deferred-
constraint transaction. File removal uses journaled no-overwrite moves into
retained trash, preserving client sources, extras and versions. Failed cleanup
holds the media gate durably. No arbitrary SQL or filesystem path is accepted
from the review client, and no permanent purge endpoint exists.

Process environment controls bind address, browser origin, config location, and
media/download roots. SQLite stores preferences, account state and encrypted
provider credentials. All application API data requires authentication; only
health/readiness, session status and setup/login are public. Static frontend
files are public and contain no private application state.

The first administrator requires a random 256-bit code stored in an owner-only
`setup-token` file. A singleton account row atomically closes setup. Passwords
use salted Argon2id (19 MiB, two iterations, one lane); password hashing is
limited to two concurrent operations. Login/setup are limited per remote IP and
globally; forwarded IP headers are ignored. This milestone supports one admin.
The owner's local `reset-password` CLI reads stdin and atomically revokes all
sessions. There is no email-based recovery or external identity provider.

Random 256-bit sessions use HttpOnly, SameSite=Strict, Path=/ cookies and a fixed
24-hour expiry. SQLite stores SHA-256 token hashes only, retains at most ten
sessions and survives restarts. Logout revokes its row. CSRF values are domain-
separated SHA-256 derivatives of the session token, exposed through session
status and compared in constant time on every authenticated mutation. Session
tokens never appear in JSON or localStorage. JSON bodies are bounded and strict;
settings additionally require a matching revision.

TMDb's Read Access Token is write-only in Settings. It is validated against TMDb
before replacing the saved value, then encrypted using AES-256-GCM with unique
random nonces and provider/version-associated data. `secrets.key` is generated
locally with owner-only creation permissions. Backups must include this key;
missing keys with existing ciphertext or failed decryption stop startup. This
protects database-only exposure. Access to the entire config backup also reveals
the key; restrict that directory and backups using the host's access controls
(Windows uses inherited ACLs). Keys and setup codes never enter normal logs.

Only loopback Hosts are accepted by default, limiting DNS rebinding. Set
REELSCAREY_PUBLIC_URL to an explicit browser origin for another host or TLS
reverse proxy; forward the original Host. Cookie Secure is enabled for that HTTPS
origin or direct TLS, without trusting arbitrary forwarded headers. Mutations
reject unexpected Origin/Fetch-Metadata and require CSRF. Default bind and
Compose publication remain loopback-only. Broader deployment requires TLS and
the remaining production security review. Logs omit bodies, cookies, headers,
query strings and upstream response bodies.

TMDb requests use the fixed official HTTPS origin, Bearer authorization, bounded
responses, timeouts, no redirects and a shared Retry-After cooldown on 429.
There is no user-configurable upstream URL. Posters accept only normalized TMDb
paths and the official image origin. CSP permits that origin for images only.

## Deployment

The multi-stage Docker build compiles UI, tests Go, and creates a Debian
runtime with CA certificates and FFprobe for verified movie imports.
`/config`, `/media`, and `/downloads` are mounted persistently. Initial Compose
download mounts are read-only to preserve sources; media is writable for imports.
Persistent host folders must be accessible to UID/GID 10001 (or an explicit
Compose user override). One process must own a given config directory; SQLite
should be on local storage, not a network filesystem.

## Version 0.22 follow-through

All seven follow-through features are implemented: scheduled metadata refresh
and richer calendar facts, reviewed refresh of linked AniList identities,
per-show subtitle preferences, reviewed subtitle timing/replacement/rollback,
YouTube subscriptions and bulk acquisition, source/codec scoring with Usenet age
filters, and reviewed bulk metadata matching. See [feature behavior and bounds](docs/SEVEN_FEATURES.md).
Provider credentials, private-service acceptance and the license still require
owner decisions or access where listed in the remaining-work record.
## Version 0.23 storage and reliability

The Storage page browses media/download folders and up to eight explicitly
mounted read-only roots. A guided offline CLI reviews and copies the main media
tree, creates a verified configuration backup, retains originals, refuses
replacement and requires activation of the new mount before workers resume.
See [storage behavior and commands](docs/STORAGE.md). Standard TV matching now
also accepts explicit 1x02, spaced S01 E02 and Season 1 Episode 2 labels while
rejecting ambiguous or multi-episode automatic selections. No database migration
or runtime dependency is added. [Workload checks](docs/WORKLOAD.md) define the
repeatable constrained container budget and its limits.

## Version 0.26 integrations and subtitle alignment

Transmission 4 joins the named torrent clients with stable hash tracking,
atomic ownership labels and the existing reviewed TV/import recovery path. Kitsu
joins AniList for explicit Anime identity links and fact-refresh reviews. SubDL
provides manually reviewed original-format subtitles. Reference-text alignment
fits a conservative offset and uniform drift, with existing edit review/rollback.

Migration 040 rebuilds the named-client allowlist while retaining IDs/defaults;
041 records subtitle provider provenance in tracks and pending import journals.
All run in the same Go/SQLite application container; no client daemon or model is
bundled. See the feature documents and [remaining work](docs/REMAINING.md).

The simple Compose installation initializes only private app folders as root, then
drops to PUID/PGID through Debian's gosu before executing the Go process. Explicit
Docker users skip initialization and remain compatible with dropped capabilities
and a read-only root filesystem. The simple layout keeps state, backup archives
and acquisition staging in separate sibling directories under one config mount;
existing flat-config deployments retain their paths. TZ uses Debian tzdata.
See [installation](docs/INSTALL.md). No database migration is introduced.

## Automatic existing-library matching (0.27)

Migration 042 adds durable per-scan matching jobs, bounded title caches and
per-file evidence. Transfer's existing-library worker resolves the complete scan
before queueing any matches, rejects overlapping target claims, and uses a separate
transactional automatic authorization guard. Current lease, scan completion,
file identity, title/provider/category and canonical episode snapshots must still
agree when registration is queued. Manual consent checks remain in place.
Guarded catalog/season writes prevent cancelled provider fetches from modifying
state. Existing verification supplies file observations, FFprobe facts and in-place availability;
no download rows or video mutations are introduced. Backups include this state;
restore pauses jobs and clears leases. One process/container remains sufficient.

Migration 043 adds `media_files.verification_json` for bounded existing-file
observations and a scan-resolution index. Transfer keeps one discovery/inspection
worker and an independent leased metadata worker. Large existing files initially
have no full SHA; internal/filecheck captures bounded change evidence and promotes
to a full digest only for explicit file operations. Organization prepares these
checksums before review transactions; its existing copy/publication/cleanup
journals still require full digests. Subtitle inspection validates pending
observations without a full-library read. Seekable inherited descriptors keep
FFprobe inspection independent of movie size for ordinary container metadata.

Migration 044 removes movie-file cardinality restrictions while retaining file
IDs, observations and child references. Subtitle edits have a cascading foreign
key; their explicit row IDs and payloads are preserved across the parent rebuild.
An indexed generated resolution projects the same video-dimension bands as
quality.VerifiedQuality. Automation owns opt-in per-movie HD/4K policy, while
transfer owns per-version reservations and checks actual resolution before import.
Library planning projects each enabled target independently. Additional versions
and per-file replacements require explicit manual intent; organization snapshots
include stable version suffixes for multi-file movies. See [movie versions](docs/MOVIE_VERSIONS.md).
