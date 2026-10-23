# Validation record — 2026-10-23

## Scheduled searches and quality profiles

Owner Torznab setup/shared-mount acceptance remains explicitly deferred. This
milestone adds opt-in interval searches, editable resolution/language profiles,
per-item assignments, ranked/rejected release history, automatic missing-item
queues and non-destructive resolution upgrade suggestions. All new schedules
start paused/search-only. Media/download tests use disposable data exclusively.

- Backend/API checks cover paused and one-off intent, persisted schedules and
  profiles, cursor rotation, release dates, parent/episode monitoring, automatic
  queue deduplication across reopen, five-attempt backoff, lease reclaim and
  pause/profile/assignment/monitoring changes during metainfo retrieval.
- Movie and TV upgrade checks preserve existing registrations and refuse
  replacement queues; at-cutoff files make no indexer request. Automatic TV
  downloads use existing reservations and still stop for mapping review before
  import. Interrupted one-off requests retain intent for restart.
- Profile checks cover exact title/year/episode matching, pack/range rejection,
  explicit languages, unknown/conflicting labels, seed/size limits, term rules,
  stable ranking, cropped-video dimensions and stale/duplicate profile edits.
- All 26 frontend interaction tests pass, including deferred Torznab, opt-in
  download mode, strict saved schedule bodies/CSRF, profile conflict recovery,
  rejection explanations and movie monitoring/profile assignment.
- Clean locked frontend install reported zero known dependency vulnerabilities;
  formatting, TypeScript and production build pass. Full Go tests, go vet and
  Windows build pass outside the sandbox needed by localhost/link tests.
- Native smoke passes with profile/schedule/session/settings persistence after
  restart and waiting-indexer behavior. An initial smoke invocation used the old
  default executable and correctly failed the automation capability assertion;
  rerunning against the newly built executable passed.

- The existing local preview was stopped, backed up and upgraded to 0.6. The
  administrator account still requires sign-in; no owner credential was read or
  changed. The new schedule defaults to paused, and owner Torznab remains deferred.
- A separate disposable browser installation saved a 60-minute/five-target
  schedule, created a Japanese-language HD profile with a required term, assigned
  it to a generated catalog item, changed movie monitoring and reloaded those
  preferences. Its real scheduler recorded waiting_indexer with zero searches or
  downloads, then accepted a pause. The test server/data were removed afterward.
- Browser verification exposed an omitted frontend direct-route entry; the fix
  is covered by HTTP routing, native smoke and container smoke checks. The local
  native direct-link/restart check passes with the fix.

- The [successful Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/39983093550)
  at code commit 89ff38b passed all 26 UI tests, full Go race/static checks,
  disposable authenticated qBittorrent movie/TV downloads with generated media,
  native direct-link/restart smoke, Docker build and authenticated container
  restart checks. Both native/container checks retain profiles and enabled
  search-only schedules across restart while waiting for the unconfigured indexer.
  Real TV import verification still registers two files for three episodes and
  preserves sources. No owner services or media were used by these checks.

See AUTOMATION.md for precise selection and safety limits. Earlier CI attempts
were superseded/cancelled while the direct-link and stricter pack matching fixes
were finalized; the linked run validates the final code head.

## Television downloads and imports

Torznab owner setup remains explicitly deferred. This milestone extends the
existing provider/client/leased worker and anchored importer for episode and
season releases, reservations, confirmed file mappings, multi-episode files,
per-file crash journals, naming preferences and registered episode availability.

- Backend checks pass for reviewed mappings, disjoint/overlapping reservations,
  cancellation/resume conflicts, explicit skips, lost submission responses,
  multi-episode registration, copy/hardlink preference, source retention,
  collision refusal and partial-season restart while the client API is offline.
- The schema upgrade check preserves completed movie registrations, staged movie
  journals, file IDs and opaque encrypted payload bytes with foreign keys enabled.
- TV search/mapping API checks cover authentication/CSRF, TV category 5000,
  canonical episode scope, strict bodies and refusal of client-supplied paths.
- Clean locked frontend install, formatting, TypeScript, build and all 22
  interface tests pass. Mapping tests require review, group episode IDs, preserve
  failed choices and verify episode-scoped release requests. No known dependency
  vulnerabilities were reported during that install.
- Full Go tests, go vet, Windows executable build and isolated native
  startup/authentication/restart/password-recovery smoke pass.
- The Linux opt-in real-client check now includes a generated multi-file TV pack
  with three episodes across two videos, plus the existing movie regression.
  Both passed in the [successful Linux run](https://github.com/faaaaq/ReelScarey/actions/runs/39916273665)
  at code commit ff345d8. It reopens state during download and after mapping
  confirmation. The run also passed all 22 frontend tests, full Go race/static
  checks, native smoke and Docker build/authenticated container restart smoke.
- The stopped preview configuration was backed up before migration to 0.5.
  The existing administrator session, saved integrations and library survive;
  Torznab remains unconfigured. No owner download or library file was used.
- A separate browser session against disposable generated media required review
  before import, then registered three episodes across two files. The season
  showed all three available, including the shared episode file. Both source
  videos remain present and SHA-256 hashes of both imports match their sources.
  The disposable server and its temporary media were removed after verification.

Media tests use disposable roots and owned generated data. The owner's existing
torrents/library are not used for acceptance. See TV_DOWNLOADS.md for behavior,
filesystem guarantees and bounded pack/mapping limits.

## Television catalog milestone

The owner deferred Torznab setup and authorized continuing to television. This
milestone implements TMDb TV search/details, canonical idempotent show adds,
season summaries, on-demand episode loading/refresh and revision-checked
show/season/episode monitoring. TV release/download/import work remains pending.

- The TV provider contract checks search encoding, language/adult/page parameters,
  series/season identity, specials, unknown dates, nullable runtimes and invalid
  or duplicate episodes. Existing bounded HTTP, TLS, redirect and rate-limit
  behavior is shared with the movie adapter.
- Library/API checks cover distinct movie/TV identities, canonical adds, duplicate
  retry during a provider outage, selections across refresh/pause/reopen, stale
  revision rejection, missing-child rollback, authenticated/CSRF-protected routes,
  offline episode reads and direct TV browser links.
- Migration 004 upgrades from 003 with existing movies/series and encrypted
  integration data, preserving the catalog and opaque credential bytes. Earlier
  migrations remain unchanged. A stopped-config backup precedes preview upgrade.
- Frontend clean installation, formatting, TypeScript, production build and all
  19 interaction tests pass, including TV search/add and episode monitoring.
  A restricted npm cache failed locally; reinstalling from the locked registry
  outside that cache succeeded with zero reported vulnerabilities at install time.
- The full Go test suite, go vet, Windows executable build and isolated native
  startup/authentication/restart/password-recovery smoke pass. An initial Go
  package scan overlapped a dependency reinstall and failed while node_modules
  was being replaced; the required sequential run after the frontend build passed.
- The stopped preview config was backed up before migration. Version 0.4 starts
  successfully with the existing administrator session and saved TMDb connection.
  Live browser search/details returned FROM (2022), TMDb 124364, with its real
  artwork and synopsis. This was a read-only check; no show was added to the
  owner's catalog and no owner media/downloads were used for testing.
- The [successful Linux run](https://github.com/faaaaq/ReelScarey/actions/runs/39892317129)
  at code commit e701dfa passed all 19 frontend tests, the full Go race/static
  checks, real disposable qBittorrent download/import regression, native process
  smoke, Docker build and authenticated container restart/persistence smoke.

See [television operations](TELEVISION.md) for monitoring semantics and limits.

## Movie download/import milestone

Implemented Torznab capability tests/search, authenticated qBittorrent submission,
encrypted settings and torrent payloads, durable leased reconciliation, file
selection and FFprobe/SHA-256 verified imports. All media tests use temporary
roots and generated data; no existing user library or torrents are modified.

- Local Go suite and API/security checks pass, including lost-response recovery,
  lease fencing, traversal, collision refusal, staged-copy restart and already-
  published restart. Windows uses staged copies; Linux anchored links and actual
  cross-filesystem behavior are exercised in CI.
- Real local FFmpeg/FFprobe generated a video, verified it, and rejected garbage.
- Frontend formatting, TypeScript, production build and 17 behavior tests pass.
- Windows executable build and isolated native startup/auth/restart smoke pass.
- The [Linux workflow run](https://github.com/faaaaq/ReelScarey/actions/runs/39889570064)
  at code commit 7e8a909 passed the frontend suite, Go race/static checks, native
  authenticated restart smoke, Docker build/container restart smoke, and the
  opt-in real authenticated qBittorrent-nox download/import test. The private
  generated video came from a localhost web
  seed; the Torznab service was a protocol fixture, not a public indexer. The
  application database was reopened during the download, and the completed
  FFprobe-verified import retained the original seeding file. The container check
  verified embedded frontend, protected APIs, health/FFprobe and persistent
  authentication/settings across restart. The workflow completed successfully.
- The first workflow attempt passed the frontend/race/static checks but exposed
  a test-client startup flag unsupported by Ubuntu's qBittorrent 4.6.3. Removing
  that flag and accepting the notice in the disposable profile fixed the real
  client test. Client startup now detects early process exit immediately.
- The owner's hosted qBittorrent connection was verified and saved through
  Settings after supporting its newer HTTP 204 login and session-cookie format.
  Credentials were entered privately and remain encrypted. No owner torrents
  were added or changed. No Torznab service is configured yet; an indexer and a
  shared download mount remain necessary for owner-service acceptance.

See [workflow operations](MOVIE_WORKFLOW.md) for filesystem guarantees and limits.

## Previous movie-catalog milestone

Implemented: single-admin authentication, encrypted TMDb configuration, real
TMDb movie search/details, canonical idempotent movie adds, and a paginated
movie library. This completes the requested catalog milestone; the full
Torznab/download/import vertical slice remains on the roadmap.

## Local checks passed

| Check | Evidence |
| --- | --- |
| Go tests | `go test ./...`: auth, config, database, HTTP, library, metadata, securestore and settings |
| Static Go checks | `go vet ./...` |
| Authentication | Protected API data, one-time setup closure, invalid credentials, Argon2id hashes, hashed session storage, cookie flags, session expiry/restart/logout, rate/concurrency limits, password recovery revocation |
| Credentials | AES-GCM encryption, reopen with matching key, reject missing key/tampered ciphertext, write-only status responses, failed replacement preserves current token |
| Provider contract | Bearer header, encoded query/page/adult flag, details normalization, official poster URLs, error/body limits, rate-limit cooldown, timeouts and safe errors |
| Movie workflow | Authenticated provider configure → search → canonical details → add → library read; duplicate retry survives provider failure without changing monitoring |
| Library | Concurrent duplicate adds create one row; 24-item pagination/totals; metadata, monitoring and artwork references survive reopen |
| SQLite | Initial creation and existing migration history, WAL/foreign keys/timeouts, rollback, checksum drift/newer schema rejection, Unicode config paths |
| Frontend | TypeScript check, formatting check, production Vite build |
| UI behavior | 12 tests: setup/login, truncated setup-code feedback before an API attempt, session expiry/logout, search/details/add with monitoring choice, failed add, write-only TMDb save, settings/conflicts/offline recovery/navigation |
| Dependency repair | Fresh npm install from repaired lock; Linux x64 dry-run resolution succeeds; npm reports zero known vulnerabilities at install time |
| Native executable | Windows amd64 build and real process launch |
| Native process smoke | Embedded frontend/assets, health/readiness, protected API, setup-code CLI, setup/login, session + settings across restart, healthcheck CLI, stdin password reset/revocation and logout |
| Browser | Owner created the administrator and entered the TMDb token privately in Settings; connection verified and saved. Live search and details returned Mutiny (2026), TMDb 1288445, 96 minutes. Adding it saved one monitored library entry with its poster and synopsis, visibly marked Not downloaded. The session and saved movie remained after replacing and restarting the preview executable, and Test saved connection verified the encrypted token after restart. |

Local tools: Windows amd64, portable Go 1.27.3, Node 24.12.0, npm 11.6.2.
Tests use temporary config roots only. Provider response fixtures exist only
inside tests; production always uses the real official TMDb HTTPS adapter.
No existing user media was scanned or changed.

## CI and environment limits

The foundation push's [first CI run](https://github.com/faaaaq/ReelScarey/actions/runs/39851298503)
failed before tests: npm found missing cross-platform @emnapi dependency entries.
This milestone regenerates the lock without an existing node_modules directory
and verifies both a clean Windows install and Linux resolution.

The milestone's first Docker build found an existing /media directory in the
Debian runtime image. Changing directory creation to mkdir -p fixed the build.
The [successful Linux run](https://github.com/faaaaq/ReelScarey/actions/runs/39881583988)
at commit 45d3983 passed the full frontend suite, Go race detector/static checks,
native authenticated smoke test, Docker build and authenticated container
restart/persistence/health/FFprobe smoke test. The subsequent setup-code UX fix
adds a twelfth frontend test, which passes locally. Docker and a C toolchain are
not available locally, so container and race checks run in GitHub Actions.

## Remaining acceptance work

- Linux/ARM deployment coverage, resource measurements, backup/restore drills,
  broad accessibility audits and full security review remain future work.
- Owner-service download/import acceptance requires the indexer and shared mount
  described above. TV ingest and subtitle adapters remain future work.
- Sessions use a fixed 24-hour lifetime. Rate-limit counters are memory-resident
  and reset on restart; durable sessions and library data are SQLite-backed.
- The project license still needs the owner's choice before public distribution.

## Reproduce

Follow README.md to build the UI before Go, then run `go test ./...`,
`go vet ./...` and `node scripts/smoke.mjs <binary-path>`. The smoke test creates
and removes only its own temporary config directory. `scripts/container-smoke.mjs`
is intended exclusively for the disposable `reelscarey-ci` container in CI.
See AUTHENTICATION.md for first setup, password recovery and key backup.
