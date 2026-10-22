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
internal/httpapi/    REST handlers, middleware, frontend serving
web/src/             application, pages, reusable UI, API client
web/dist/            generated frontend (ignored, embedded at build time)
docs/                API and operational documentation
.github/workflows/   build, tests, container smoke check
```

## Data model

Implemented tables: `schema_migrations`, singleton `settings`, and `media_items`.
The migration ledger stores version, filename, checksum, and applied time.
Settings changes are atomic and carry a revision for optimistic concurrency.
Media items share `kind` (movie/series), TMDb identity, title, year, monitored
flag, and status. `(kind, tmdb_id)` is unique; a movie and series may have the
same TMDb ID. The foundation exposes read-only library collections; no metadata
ingestion or media mutations exist yet.

Planned entities (design, **not implemented schema**):

| Entity | Relationships and invariants |
| --- | --- |
| seasons | Belongs to a series media item; unique series + season number |
| episodes | Belongs to season; unique season + episode number; optional air date |
| media_files | Belongs to media item; episode_files join supports multi-episode files; unique canonical path, size and probe data |
| indexers | Provider type, endpoint, categories and protected credentials |
| download_clients | Provider type, endpoint, protected credentials and path mapping |
| downloads | Media item, optional episode targets, indexer release identity, client download ID, state and import journal |
| quality_profiles | Ordered qualities, languages, cutoff and scoring rules |
| subtitles | Media file, language, provider identity, path and hearing-impaired flag |
| jobs | Type, payload, unique deduplication key, due time, attempts, lease owner/expiry and last safe error |
| import_operations | Download, source fingerprint, destination, staged progress and completion |

Media file paths are never derived from untrusted release names without
normalization and root containment checks. Downloads remain separate from files:
an imported file can survive removal of download-client history.

## Module boundaries and planned interfaces

These are design contracts, not claimed integrations. Concrete Go interfaces
will be added alongside their first tested consumer to avoid speculative code.
Every operation receives a context and returns typed results/errors.

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
and import. No worker or scheduler is running in the foundation.

## Configuration and security

Process environment controls bind address, config location, and media/download
roots. SQLite stores editable library preferences. Environment seeds paths only
at process startup; UI preferences survive restarts. API DTOs intentionally
contain no credentials. Before provider credentials are introduced, implement
write-only secret fields and authenticated administration; settle encryption/key
backup policy with the owner. Restricted config permissions are not a substitute
for encryption or access control.

This milestone has no authentication and is intended for loopback/local testing.
The default bind and Compose-published port are loopback-only. Mutation endpoints
require JSON, reject cross-origin browser requests, bound request size, reject
unknown fields, and reject stale revisions. Deploying to a shared network or the
internet requires authentication and TLS. Logs omit bodies and query strings.
The service sets security headers and graceful shutdown flushes database work.

## Deployment

The multi-stage Docker build compiles UI, tests Go, and creates a non-root Debian
runtime with CA certificates and FFprobe available for the future import module.
`/config`, `/media`, and `/downloads` are mounted persistently. Initial Compose
media/download mounts are read-only because this milestone performs no imports.
Persistent host folders must be accessible to UID/GID 10001 (or an explicit
Compose user override). One process must own a given config directory; SQLite
should be on local storage, not a network filesystem.
