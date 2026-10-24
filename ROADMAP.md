# ReelScarey roadmap

Status is evidence-based: checked items are implemented, not planned adapters.

## 1 — Foundation
- [x] Go server, structured logging, graceful shutdown and validated environment
- [x] SQLite initialization, transactional checksummed migrations and persistence
- [x] Versioned REST API, health/readiness, settings and library reads
- [x] React/TypeScript/Vite/Tailwind/Radix UI with responsive navigation
- [x] Dashboard from live API data and editable persisted library settings
- [x] Multi-stage Dockerfile, Compose mounts and healthcheck configuration
- [x] Architecture, API, contributor and operational documentation
- [x] Automated tests and native launch/API/frontend verification
- [x] Docker build and runtime smoke test executed successfully

## 2 — Movie workflow
- [x] Authenticate administration: code-protected setup, login/logout, persistent
  sessions, CSRF/Host checks, password hashing, throttling and local recovery.
- [x] Implement encrypted, write-only TMDb credentials with real test/replace/
  disconnect operations; document key backup and fail-closed recovery.
- [x] Implement real TMDb movie search/details with timeouts, safe errors,
  rate-limit cooldown and contract tests.
- [x] Add a movie idempotently from canonical provider metadata; persist artwork
  references and monitoring state, show paginated library collections.
- [x] Test authentication, search/add API behavior, duplicate/restart cases,
  provider failures and frontend interaction with isolated fixtures.
- [x] Verify authenticated search/details/add against live TMDb with the owner's
  token entered directly in Settings: Mutiny (2026), TMDb 1288445.
- [x] Configure/test Torznab; normalize real API release responses.
- [x] Configure/test qBittorrent; deduplicate releases and reconcile lost responses.
- [x] Verify the owner's saved qBittorrent connection in Settings after a restart.
- [x] Introduce persistent jobs, leases and restart-safe download reconciliation.
- [x] Detect completion, map paths, probe files and plan safe imports.
- [x] Preserve seeding sources; prefer Linux hardlinks and safely stage copies.
- [x] Journal imports, update library availability and display actionable failures.
- [ ] Complete owner-service acceptance after a Torznab service and shared download
  mount are connected. Disposable real-client CI validation is recorded separately.
  Owner explicitly deferred Torznab setup on 2026-10-22 and authorized continuing
  the project; this acceptance item remains deferred rather than blocking TV work.

Acceptance: a real movie moves from TMDb search through a real indexer and
download client to a verified library file. Restart during download/import must
recover; collision and cross-filesystem tests must pass. Disposable services and
temporary libraries only. Never use unauthorized content as test material.

## 3 — Television
- [x] Real TMDb TV search/details using the existing encrypted provider connection.
- [x] Canonical, idempotent show adds; paginated TV library and saved season summaries.
- [x] Load/refresh season episodes on demand and retain metadata for offline browsing.
- [x] Persist show/season/episode monitoring with revision conflicts and parent pauses
  that preserve individual selections. Specials start unselected.
- [x] Episode and season release searches, reviewed multi-episode mapping and availability.
- [x] Reuse download/import services; configurable, validated episode naming.
- [x] Reserve episode targets, retain per-file journals and resume partial season imports.
- [ ] Owner-service acceptance remains deferred with Torznab and the shared mount setup.

## 4 — Automation
- [x] Persistent opt-in scheduled monitoring/search and one-off requests.
- [x] Editable quality/language profiles and per-movie/show assignments.
- [x] Conservative identity filtering, ranked decisions and rejection explanations.
- [x] Resolution cutoff/upgrade suggestions that preserve existing files.
- [x] Automatic missing-item downloads through shared reservations and safe imports.
- [x] Renewed leases, bounded provider retries, cursor rotation and restart recovery.
- [x] Failed download visibility/manual retry and duplicate prevention through transfer.
- [ ] Owner-service acceptance remains deferred with Torznab/shared mount setup.
- [ ] Versioned replacement imports, audio-language verification and richer source/
  codec scoring remain future extensions; they are not represented as working.

## 5 — Subtitles
- [x] Provider interface and real OpenSubtitles.com REST integration; encrypted,
  write-only credentials, connection tests, quota/cooldown handling.
- [x] Revision-checked language, full/signs/forced and hearing-impaired preferences;
  default English full, signs/songs and forced tracks, automatic work paused.
- [x] Embedded and existing SRT/VTT/ASS/SSA detection for registered movie/TV files.
- [x] Hash/exact-release automatic matching, catalog identity checks, explicit
  review for uncertain timing, expiring opaque selections.
- [x] Leased automatic searches/downloads, bounded retries, persistent allowance
  state and journaled SRT publication with collision refusal and restart recovery.
- [x] Owner reports OpenSubtitles connected through private settings.
- [ ] Owner-library subtitle download acceptance awaits imported media; it is
  separate from the completed connection and generated-media tests.
- [ ] Additional providers, arbitrary-library scanning, ASS-style downloads,
  subtitle replacement/resynchronization and per-show language overrides remain future scope.

## 6 — Production hardening
- [x] Guided setup checklist with optional/deferred Torznab and real settings links.
- [x] Explicit authenticated installation checks, private folder/publication probes,
  database/key integrity and correctly labeled runtime resource snapshots.
- [x] OS configuration/maintenance locks, verified offline backup/check/new-folder
  restore, revoked sessions and paused workers requiring authenticated review.
- [x] Threat model, upgrade/rollback and backup/recovery documentation.
- [x] Private Linux amd64/arm64 and Windows amd64 packaging with checksums;
  automated native recovery drill and dependency vulnerability checks.
- [ ] Independent security review and signed public-release provenance.
- [ ] Sustained performance/resource budgets on representative NAS hardware;
  arm64 runtime and shared-storage filesystem acceptance.
- [ ] Owner chooses open-source license before public distribution.

## 7 — Direct indexers and title details
- [x] Managed encrypted multi-indexer CRUD, explicit tests, offline disabling,
  source-bound selections and visible partial failures.
- [x] Independent direct IPTorrents/XSpeeds session adapters, public NYAA RSS,
  private RSS and multiple Torznab endpoints; Prowlarr optional.
- [x] Movie/TV detail navigation, cached cast/creators/directors/writers,
  IMDb/TMDb/trailer links and offline local status.
- [x] Verified file paths/sizes/technical details, original release/source history
  and individual Activity links; shared TV files counted once.
- [ ] Owner private tracker session acceptance and shared-storage import acceptance.
- [ ] Absolute anime numbering, automatic challenge solving and arbitrary library
  scanning are outside this milestone.

See [setup and exact limits](docs/INDEXERS_DETAILS.md). Current validation is
recorded in [validation](docs/VALIDATION.md).

## 8 — Existing-library discovery and registration
- [x] Explicit bounded discovery of configured movie/TV folders with durable
  restart recovery, review history, skip/stop controls and no arbitrary paths.
- [x] Saved-catalog/TMDb match review, new titles paused, explicit consecutive
  TV episode selection and no automatic filename-based registration.
- [x] Stable identity/size/mtime checks, double SHA-256 and FFprobe verification,
  lease-fenced atomic in-place registration and download/episode conflict refusal.
- [x] Genuine existing-file provenance, preserved migration references and shared
  availability/quality/subtitle consumers without fabricated download history.
- [ ] Owner-library acceptance is separate from generated/disposable tests.
- [ ] Copy/move/rename, arbitrary-root browsing, automatic bulk matching and
  replacement/reassignment remain future scope.

IPTorrents and XSpeeds account acceptance remains explicitly deferred by the owner.
See [existing-library workflow](docs/EXISTING_LIBRARY.md).

## 9 — Usenet search, downloads and safe imports
- [x] Managed Newznab caps/search/NZBs, encrypted keys, bounded responses,
  source provenance, same-origin download URLs and redirect refusal.
- [x] Encrypted SABnzbd settings/tests and protocol-aware client routing,
  without applying torrent seed counts to Usenet.
- [x] Durable client identity/progress, lost-reply reconciliation, conservative
  uncertain-submission handling and completed post-processing gates.
- [x] Bounded dedicated-folder discovery, stable file choices, shared movie/TV
  reservations, mandatory TV review and journaled source-preserving imports.
- [x] Generated-video real SABnzbd/NNTP/FFprobe movie and TV tests on Windows.
- [x] Final Linux race/container/package and real SABnzbd acceptance.
- [ ] Owner Newznab/SABnzbd/shared-mount acceptance requires private setup.
- [ ] Retention scoring, multiple SABnzbd clients and NZBGet remain future scope.

See [Usenet setup and limits](docs/USENET.md). IPTorrents and XSpeeds account
testing remains deferred.

## 10 — Wanted and release calendar
- [x] Authenticated read-only movie/episode planning from registered files,
  tracked reservations, monitoring hierarchy and validated cached dates.
- [x] Wanted filters, literal title search, counts, stable pagination and real
  links to existing release searches, title details and Activity.
- [x] Monthly calendar agenda, retained filter bookmarks, civil-date display and
  explicit undated/unloaded metadata coverage without forecasting episodes.
- [x] Invalid catalog dates cannot authorize automated provider searches.
- [x] Complete browser/native/package/Linux acceptance for the application head.
- [ ] Calendar uses cached metadata; broadcasts, filesystem-health checks,
  automatic metadata refresh and replacement imports remain future scope.

See [planning behavior and limits](docs/PLANNING.md). IPTorrents and XSpeeds
account testing remains deferred.

## 11 — Library browsing and visual overhaul
- [x] Literal title search, saved status/parent monitoring filters, stable title,
  year and recently-added ordering with bounded pagination and coherent counts.
- [x] Bookmarkable poster/table views, filter preservation, page recovery and
  explicit partial-show availability labels.
- [x] Shared catalog layout, compact navigation, functional overview and removal
  of application slogans, decorative labels, welcome art and promotional copy.
- [x] Local backend/API checks and 62 UI interaction tests.
- [x] Complete browser/native/package/Linux acceptance and preview upgrade.
- [ ] Bulk editing, media removal, disk health scanning and versioned replacement
  imports remain future scope. No private-account testing is included.

See [browsing behavior and interface](docs/LIBRARY_BROWSING.md). IPTorrents and
XSpeeds account testing remains explicitly deferred.

## 12 — Reviewed bulk monitoring
- [x] Explicit page selection in poster/table views, current-state review and
  atomic parent movie/show pause/resume with stale-edit rejection.
- [x] Preserve child selections, existing downloads, profiles and schedules;
  invalidate older TV/assignment editors and automatic queue decisions.
- [x] Keep the catalog layout and plain copy; replace olive with graphite/blue.
- [x] Local feature/API/security checks and 70 UI tests.
- [x] Complete clean/native/package/browser/Linux acceptance and preview upgrade.
- [ ] Bulk episode edits, removal, file moving and replacement imports
  remain future scope. Private tracker account testing stays deferred.

See [bulk monitoring and limits](docs/BULK_MONITORING.md).

## 13 — Reviewed profile assignment (current)
- [x] Library effective-profile labels and filters, including inherited defaults.
- [x] Explicit page selection, target-rule review and atomic profile assignment
  or return to schedule-default inheritance for movies/shows.
- [x] Fence stale assignments/profile rules/defaults and automatic queue decisions;
  preserve monitoring, child choices, schedules, existing downloads and files.
- [x] Complete local/browser/native/package/Linux acceptance and preview upgrade.
- [ ] Replacement imports, episode bulk edits, removal and file moving remain
  future work. IPTorrents/XSpeeds account testing stays deferred.

See [profile assignment and exact limits](docs/BULK_PROFILES.md). The graphite/blue
layout and plain interface copy are retained.

## Validation record

2026-10-24 reviewed profile assignment: the
[Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40139874438)
passes all 78 UI tests, backend/race/static/vulnerability checks, real disposable
download/import regressions, package recovery and container restart. Browser
checks verify effective/default-only filters, exact movie/show reviews, target
rules, cancel/apply, inheritance and retained TV episode/specials choices on
desktop and phone. Graphite/blue is retained. The owner preview is backed up and
running 0.15; private tracker account testing remains deferred. See
[validation and environment limits](docs/VALIDATION.md).

2026-10-24 reviewed bulk monitoring: the
[Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40136864657)
passes all 70 UI tests, backend/race/static/vulnerability checks, real disposable
download/import regressions, package recovery and container restart. Browser
checks verify exact selections, cancel/apply, changed/unchanged counts and retained
TV episode/specials choices on desktop and phone. The layout stays intact with
graphite/blue replacing olive. The owner preview is backed up and running 0.14;
no owner media or preferences were used as test data. IPTorrents/XSpeeds account
testing remains deferred. Full evidence and limits are in
[validation](docs/VALIDATION.md).

2026-10-24 library browsing and visual overhaul: the
[final Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40133125302)
passes all 62 UI tests, backend/race/static/vulnerability checks, real disposable
download/import regressions, checksummed packages/recovery and container restart.
Desktop/phone browser checks verify poster/table views, saved filters, ordering,
pagination, navigation and the functional overview. Slogans, decorative labels,
welcome art and promotional copy are removed. The owner preview is backed up and
running 0.13. No owner media or private services were used as fixtures; IPTorrents
and XSpeeds account testing remains deferred. Full evidence and limits are in
[validation](docs/VALIDATION.md).

2026-10-24 Wanted and calendar: the
[Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40073977195)
passes all 55 UI tests, backend/race/static/vulnerability checks, real disposable
download/import regressions, package recovery and container restart. Disposable
browser checks verify monitoring, registered/shared files, job links, saved
filters, undated coverage, month navigation and responsive layouts. The owner
preview is backed up and running 0.12; no owner media or private services were
used as fixtures. IPTorrents/XSpeeds account tests remain deferred. Dates are
cached catalog metadata, not broadcast times or a promise of release availability.
Full evidence and limits are in [validation](docs/VALIDATION.md).

2026-10-24 Usenet: the
[final Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40071407180)
passes all 47 UI tests, backend/race/static/vulnerability checks, real generated
SABnzbd/NNTP movie and reviewed TV imports, torrent/subtitle/existing-library
regressions, package recovery and container restart. Windows real-client and
disposable browser checks also pass. The preview is backed up and running 0.11;
no owner media or private accounts were used as fixtures. The initial Linux
failure was missing SABnzbd runtime tools, now installed and checked. Owner
Usenet/shared-mount acceptance and IPTorrents/XSpeeds account testing remain
separate/deferred. Full evidence is in [validation](docs/VALIDATION.md).

2026-10-23 existing-library registration: the
[Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40057876547)
passes all 44 UI tests, backend/race/static/vulnerability gates, generated-media
registration and real disposable-client regressions, native/package recovery and
container restart checks. Browser acceptance verifies reviewed movie and shared
TV-file registration with generated videos, preserving their original paths and
recording no invented download history. The owner preview is backed up and
upgraded to 0.10; no owner library scan was initiated. IPTorrents/XSpeeds account
testing remains deferred. Full evidence and limits are in
[validation](docs/VALIDATION.md).

2026-10-23 direct indexers and title details: the
[Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40048388113)
passes all 39 UI tests, backend/race/static/vulnerability gates, generated-media
and real disposable-client regressions, native/package recovery and container
restart checks. Browser acceptance verifies source management and title/file
history with disposable data. The owner preview is backed up and upgraded to
0.9. Public NYAA RSS format/search was read successfully, but the Go adapter's
direct socket is blocked by this Windows environment; private tracker account
checks await owner sessions. Full evidence and limits are in
[validation](docs/VALIDATION.md).

2026-10-23 production hardening: setup guidance, explicit local diagnostics,
config ownership locks, verified offline backups and new-folder restores with
session revocation/worker review are implemented. The
[final Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40028358946)
passed all 35 UI tests, race/static/vulnerability checks, real disposable client
and generated-media regressions, native/extracted-package recovery drills,
checksummed cross-builds and the memory-limited container restart test. The
extracted Windows package also passes locally. The owner preview was backed up
and upgraded; Torznab stays deferred. NAS/arm64 runtime, independent security
review, release provenance and license choice remain release gates. Detailed
evidence and limits are in [validation](docs/VALIDATION.md).

2026-10-22: Foundation native checks passed. Its first GitHub run failed because
the npm lock omitted cross-platform dependencies. The lock was regenerated in a
clean directory; a clean Windows install and Linux resolution check now pass.
The movie catalog backend suite and 12 frontend tests pass. Native process smoke
checks include setup, login, persistent sessions/settings, logout and password
recovery/revocation. Linux race/static checks and authenticated Docker restart
checks passed in GitHub Actions. Live TMDb search/details/add saved Mutiny (2026)
with artwork and monitoring state. Full details are in
[validation evidence](docs/VALIDATION.md).

2026-10-22 movie workflow: local backend/API/static checks and 17 UI tests pass.
Generated-video FFprobe validation, partial-copy and publication restart,
destination collisions and source retention pass in temporary roots. CI adds a
disposable real qBittorrent/web-seed test and actual cross-filesystem copy. The
[workflow run](https://github.com/faaaaq/ReelScarey/actions/runs/39889570064)
passed the real client download, application-state reopen, verified import and
source-retention check, plus race/static/native checks, the frontend suite and
Docker build/container restart checks. The workflow completed successfully.
The owner's hosted qBittorrent connection also verifies after an app restart;
owner-service acceptance still requires an indexer and shared download mount.
Owner supplied a qBittorrent URL but does not currently use Torznab; existing
user torrents/library files are not used for testing.

2026-10-22 television catalog: canonical TV search/add, on-demand season episode
metadata, parent monitoring pauses, metadata refresh and revision conflicts are
implemented. All 19 frontend tests, backend/API tests, migration upgrade checks,
static checks and native restart smoke pass locally. Live read-only TMDb browser
search/details returned FROM (2022), TMDb 124364, using the saved connection.
No owner show was added as test data. TV download/import mapping remains next.
The [Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/39892317129)
also passed the complete frontend/race/static checks, disposable qBittorrent
regression, native smoke and Docker build/container restart checks.

2026-10-23 television downloads: reviewed episode/file mappings, episode target
reservations, shared multi-episode files and per-file safe import recovery pass
backend/API tests and all 22 UI tests. The
[Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/39916273665)
passed race/static/native/container checks and real disposable qBittorrent movie
and TV pack downloads. The TV check reopens state around download and mapping,
imports two generated videos for three episodes, and preserves seeding sources.
Disposable browser verification also confirmed review gating, both verified
imports and three available episodes. The owner preview was backed up and
upgraded; owner Torznab/shared-mount acceptance remains deferred.

2026-10-23 automation: durable interval/one-off searches, editable quality/language
profiles, item assignments, conservative ranked decisions and opt-in automatic
missing-item queues are implemented. Restart/reclaim, cursor fairness, provider
backoff, deduplication and changed-policy fences pass backend/API checks; all 26
frontend interaction tests pass. Native smoke preserves schedule/profile/session
settings across restart. Disposable browser checks confirm save/reload, waiting
for a deferred indexer and pausing without touching owner services. Movie/TV
resolution upgrades are suggestions only; existing files remain protected and
TV imports require reviewed mappings. The
[successful Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/39983093550)
passed all UI/race/static checks, real disposable movie/TV qBittorrent downloads,
native smoke and Docker build/authenticated restart persistence. Full evidence
is in [validation](docs/VALIDATION.md); owner Torznab/shared-mount acceptance is deferred.
