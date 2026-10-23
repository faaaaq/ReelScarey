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
internal/httpapi/    REST handlers, middleware, frontend serving
web/src/             application, pages, reusable UI, API client
web/dist/            generated frontend (ignored, embedded at build time)
docs/                API and operational documentation
.github/workflows/   build, tests, container smoke check
```

## Data model

Implemented tables: `schema_migrations`, singleton `settings`, `media_items`,
singleton `admin_account`, `sessions`, `provider_secrets`, `integration_configs`,
`release_candidates`, `downloads`, `import_operations`, `media_files`,
`series_details`, `seasons`, `episodes`, `download_tv`, `download_targets`,
`episode_downloads`, `download_tv_files`, `download_mappings` and `episode_files`.
The migration ledger stores version, filename, checksum, and applied time.
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
| indexers/download_clients | Future multi-provider CRUD; one encrypted configuration per integration exists now |
| quality_profiles | Ordered qualities, languages, cutoff and scoring rules |
| subtitles | Media file, language, provider identity, path and hearing-impaired flag |
| jobs | Future generic scheduler; downloads already persist their own leases/retries |

Media file paths are never derived from untrusted release names without
normalization and root containment checks. Downloads remain separate from files:
an imported file can survive removal of download-client history.

## Module boundaries and planned interfaces

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

## Persistent automation design (next phases)

Jobs live in SQLite and use atomic claims with expiring leases. Restarted workers
reclaim expired work; retries use capped exponential backoff and bounded attempts.
Leases must be renewed during long work. Deduplication keys prevent concurrently
scheduled duplicates; handlers reconcile external client state before retrying
side effects. This is at-least-once execution with idempotency, not exactly-once.
The movie slice introduces the first actual worker for download reconciliation
and import. A single worker now reconciles downloads using random owners,
60-second leases renewed every 15 seconds and fenced state changes. Temporary
failures back off; ambiguous submissions reconcile by expected hash/tag.
Scheduled searches, generic scheduling and upgrade policies remain unimplemented.

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

## Configuration and security

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

The multi-stage Docker build compiles UI, tests Go, and creates a non-root Debian
runtime with CA certificates and FFprobe for verified movie imports.
`/config`, `/media`, and `/downloads` are mounted persistently. Initial Compose
download mounts are read-only to preserve sources; media is writable for imports.
Persistent host folders must be accessible to UID/GID 10001 (or an explicit
Compose user override). One process must own a given config directory; SQLite
should be on local storage, not a network filesystem.
