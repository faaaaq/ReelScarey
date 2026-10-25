# Validation record — 2026-10-24

## Anime and organization (0.16 development)

- Separate Anime film/show classification and coherent browsing preserve
  canonical IDs, duplicate-add behavior, monitoring, file and episode references.
  Explicit unique aliases, no-op edits, foreign/duplicate/out-of-range entries,
  metadata identity/revision changes and automatic episode exclusions are tested.
- Independent migration review verifies released schema-10 data/credentials,
  paths, sidecar journals and profile/monitoring choices survive additive11/12.
  API review covers auth/CSRF/origin, strict MIME/JSON/bounds, revisions, literal
  file filters, no-media-write preview and durable queue/history behavior.
- Organization tests cover stale settings/assignments/aliases/files/companions,
  unsafe/overlapping paths, custom templates, distinct media-server ID tags,
  collision refusal, hardlink and staged-copy publication, styled sidecars,
  partial-copy/published/cleanup recovery, cancellation, foreign-output identity
  and same-folder renames. Durable reservations and the shared filesystem gate
  exclude restarted subtitle/discovery/import workers until reconciliation.
  Queued download naming/aliases remain immutable through edits and reopen.
- Generated owned FFmpeg media passes real existing-library discovery, explicit
  shared two-episode Anime registration, FFprobe/hash verification, custom
  absolute25–26 reorganization across reopen, ASS content/reference preservation
  and independent seeding-hardlink identity/mtime preservation. No owner file,
  service or private tracker account is used.
- Clean locked npm install/audit (zero vulnerabilities), formatting/type checks,
  all95 interface tests and production build pass. The build reports the standard
  >500 KiB JavaScript chunk advisory; it succeeds. Full backend/native checks,
  browser/package/Linux acceptance and preview upgrade are being completed.
  Graphite/blue is retained. Private IPTorrents/XSpeeds account testing remains
  deferred. Main-root migration, whole-folder extras, AniDB/alternate-order
  automation and queued batch rollback/cancellation are explicitly outside scope.

## Reviewed profile assignment (0.15 development)

- Changed backend/API suites pass for coherent read-only reviews, assignment vs
  inheritance, effective/default-only filters, profile/default/assignment changes,
  missing targets, atomic concurrent writes, cancellation and reopen persistence.
  Single-item/bulk-monitoring editors and automatic queues reject stale state,
  including assignments changed away/back during metainfo retrieval.
- Monitoring, TV parent/season choices and profile definitions are retained.
  All 78 UI tests pass after a clean locked install (zero audit vulnerabilities),
  formatting, type checks and production build. The eight new interactions cover
  exact review/cancel, target rules and revisions, pinning a current default,
  return to inheritance/no-op completion, conflict re-review, saving locks,
  bookmarked profile filters/selection scope and obsolete preview replies.
  Full Go tests/vet/build and native recovery checks pass. No migration or
  application dependency was added. All three cross-build package checksums
  verify; the extracted Windows package passes the disposable recovery drill.
- A separate disposable browser catalog verifies effective/default-only profile
  filters, explicit vs inherited labels, exact selected movies/shows, mixed
  changed/unchanged review counts, target rules, cancel without a write, profile
  assignment and return to inheritance. Paused titles remain paused; TV specials
  and individual episode choices are retained. The review and its actions fit a
  390-pixel phone viewport; the override was reset. Screenshots remain outside
  Git, and the test process/catalog were removed. No owner media, private
  accounts or connected services were used as fixtures.
- The stopped owner preview's schema-10 config was backed up and verified at
  `before-profiles-20261024-174848`; the 0.14 executable is retained. Version
  0.15 reports readiness 200, setup_required=false and healthy database,
  encryption, storage/publication and FFprobe checks. Movies is open after
  sign-in. No owner profile or monitoring assignments were changed as test data.
  IPTorrents/XSpeeds account testing remains deferred; graphite/blue is retained.

The [Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40139874438)
passed for application commit `d7d9316`: all 78 UI tests, Go race/static checks,
real disposable qBittorrent/SABnzbd movie/TV downloads and verified imports,
generated subtitle and existing-library regressions, native/extracted-package
recovery, checksummed cross-builds and authenticated container restart.
Govulncheck reports zero vulnerabilities affecting the application and one
advisory in a required module outside imported/called code; npm audit reports
zero. No advisory is suppressed. The constrained container snapshot is
8.586 MiB/512 MiB and seven PIDs, not a sustained workload measurement.
Private artifact `12579155390` contains the 0.15 packages/checksums with 14-day
retention. ARM runtime/NAS deployment, sustained load and owner-service/private
tracker acceptance remain unverified; local Docker/CGO/race tools are unavailable,
so those checks ran on Linux CI.

## Reviewed bulk monitoring and neutral palette (0.14 development)

- Feature/API/security tests pass for explicit bounded IDs, read-only previews,
  missing/repeated/invalid targets, complete-snapshot conflicts, atomic concurrent
  writes, unchanged rows, preserved profiles/child choices/tracked downloads,
  restart persistence and cancellation. Old TV and assignment editors are fenced;
  automatic queue tests reject preferences paused and resumed during metainfo fetch.
- All 70 UI tests pass, including eight new behaviors: explicit selection/review,
  cancel without a write, exact expected-state apply, refreshed results, view/page
  selection scope, show-level resume, fresh review after conflict, no-op completion,
  saving locks and preview errors. The compact layout/plain copy remain while
  olive surfaces and accents are replaced with neutral graphite and blue.
- Clean locked npm install/audit (zero vulnerabilities), format/type/70 UI tests,
  production build, full Go tests/vet/build and native startup/restart/recovery
  checks pass. All three cross-build package checksums verify; the extracted
  Windows package passes the disposable recovery drill. No application dependency
  or migration was added.
- A separate disposable browser catalog verifies exact movie selections, mixed
  changed/unchanged review counts, cancel without a write, successful apply and
  refreshed monitoring labels. Show-level pause/resume retains a selected first
  episode, unselected second episode and unselected specials. Navigation and the
  review fit a 390-pixel phone viewport; the override was reset. Known public
  posters and sample metadata were used only in that catalog. No owner media,
  services or private sessions were used as fixtures. Screenshots remain outside
  Git; the test process and catalog were removed.
- The stopped owner preview's schema-10 config was backed up and verified at
  `before-bulk-monitoring-20261024-170450`; the 0.13 executable is retained.
  Version 0.14 reports readiness 200, setup_required=false and healthy database,
  encryption, storage/publication and FFprobe checks. Movies is open after
  sign-in. No owner monitoring selections were changed as test data.
  IPTorrents/XSpeeds account testing remains deferred.

The [Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40136864657)
passed for application commit `8279f4b`: all 70 UI tests, Go race/static checks,
real disposable qBittorrent and SABnzbd movie/TV downloads and verified imports,
generated subtitle and existing-library regressions, native/extracted-package
recovery, checksummed cross-builds and authenticated container restart.
Govulncheck reports zero vulnerabilities affecting the application and one
advisory in a required module outside imported/called code; npm audit reports
zero. No advisory is suppressed. The constrained container snapshot is
8.539 MiB/512 MiB and nine PIDs, not a sustained workload measurement.
Private artifact `12577636976` contains the 0.14 packages/checksums with 14-day
retention. NAS/arm64 runtime, owner service/shared-storage acceptance and broader
security/accessibility review remain separate work.

## Library browsing and visual overhaul (0.13 development)

- Saved catalog reads pass literal wildcard/backslash search, status/parent
  monitoring filters, stable sorting, unknown-year placement, pagination,
  coherent collection/filtered counts, reopen and no-write checks. API checks
  reject malformed/repeated/unknown filters and require authentication.
- All 62 UI tests pass after a clean locked install (zero audit vulnerabilities),
  format/type checks and production build. Seven new behavior checks cover filter
  bookmarks/back navigation, page resets, layout changes without another fetch,
  precise workflow links, empty-page/filter recovery, partial TV status and
  independent overview errors. Existing authentication tests retain their behavior
  with the new plain labels. No migration or dependency was added.
- An initial restricted clean install failed with npm's "Exit handler never
  called" and left an incomplete node_modules; repeating the locked install with
  registry access succeeded. Protocol-fixture tests also require local socket
  access and pass with it. Full Go tests/vet/build and native startup, restart,
  backup and recovery checks pass. All three final package checksums verify;
  the extracted Windows package passes the same disposable recovery drill.
- Browser checks use a separate disposable catalog, known public poster URLs,
  sample titles and no connected owner services. Poster/table layouts, paused
  filtering, year ordering/unknown years, title queries, clear-filter recovery,
  back navigation, the two-page boundary and layout preservation pass. Overview,
  details, Settings and Wanted were inspected. At 390 pixels wide, phone
  navigation closes after a route change, two-column posters fit and the table
  stays in its own scrollable region. Screenshots remain outside Git. The test
  process, catalog and settings were removed. No actual download/import was
  claimed by the browser fixtures; existing real-client regressions run in CI.
- The stopped owner preview's schema-10 config was backed up and verified at
  `before-redesign-20261024-161237`; the 0.12 executable is retained. Version 0.13
  reports readiness 200, setup_required=false and healthy database, encryption,
  storage/publication and FFprobe checks. Movies is open after sign-in. No owner
  media was used as a fixture or scanned. IPTorrents/XSpeeds account testing stays
  deferred.

The [final Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40133125302)
passed for application commit `03787f9`: all 62 UI tests, Go race/static checks,
real disposable qBittorrent and SABnzbd movie/TV downloads and verified imports,
generated subtitle and existing-library regressions, native/extracted-package
recovery, checksummed cross-builds and authenticated container restart.
Govulncheck reports zero vulnerabilities affecting the application and one
advisory in a required module outside imported/called code; npm audit reports
zero. No advisory is suppressed. The constrained container snapshot is
6.703 MiB/512 MiB and seven PIDs, not a sustained workload measurement.
Private artifact `12576500637` contains the 0.13 packages/checksums with 14-day
retention. Local final archives are in `dist/private-0.13.0-final`; the extracted
Windows build also passes. NAS/arm64 runtime, owner service/shared-storage
acceptance and broader security/accessibility review remain separate work.

## Wanted and release calendar (0.12 development)

- Local library/API/automation tests pass for registration/job precedence,
  effective monitoring, shared TV files, coverage, literal search, date validity,
  UTC boundaries, pagination, reopen persistence and read-only behavior.
- All 55 UI tests pass, including eight planning interactions: exact workflow
  links, paused monitoring explanations, retained filter bookmarks, month/year
  bounds, unknown dates, invalid filter recovery and out-of-range pages.
- Invalid saved movie/episode dates no longer authorize scheduled provider work;
  focused automation regressions pass. No dependency or migration was added.
- Clean locked npm install/audit (zero vulnerabilities), format/type/55 UI tests,
  production UI build, full backend tests/vet/build and the native disposable
  startup/restart/backup/recovery drill pass. All three private cross-build
  checksums verify; the extracted Windows package passes the same recovery drill.
- Browser acceptance uses a separate disposable catalog and generated H264
  movie/shared TV file registered through the normal reviewed import workflow.
  Wanted excludes registered items, shows the exact missing episode, explains
  paused monitoring and links a controlled blocked job to its focused Activity
  view. Back navigation retains filters. Calendar shows both episodes sharing
  one file, correct saved civil dates, month navigation, unloaded-season coverage
  and an undated-items link. No provider call or download was initiated by these
  views. Desktop and narrow-screen layouts were inspected; screenshots remain
  outside Git. The disposable catalog and generated media were removed.
- The stopped owner preview's schema-10 config was backed up and verified at
  `before-planning-20261024-023115`; the 0.11 executable is retained. Version 0.12
  reports readiness 200, setup_required=false and healthy database, encryption,
  storage and FFprobe checks. The local Wanted page is open after sign-in.
  No owner media or services were used as fixtures or scanned. IPTorrents/XSpeeds
  account tests stay deferred.

The [Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40073977195)
passed for application commit `44375a4`: all 55 UI tests, full Go race/static
checks, real disposable qBittorrent and SABnzbd movie/TV downloads and verified
imports, generated subtitle and existing-library regressions, native/extracted
package recovery, checksummed cross-builds and authenticated container restart.
Govulncheck reports zero vulnerabilities affecting the application and one
advisory in a required module outside imported/called code; npm audit reports
zero. No advisory is suppressed. The constrained container snapshot is
6.629 MiB/512 MiB and seven PIDs, not a sustained workload measurement.
Private artifact `12552896307` contains the 0.12 packages/checksums with 14-day
retention. NAS/arm64 runtime, owner service/shared-storage acceptance and broader
security/accessibility review remain separate release work.

## Usenet search, downloads and imports (0.11 development)

- Local changed backend/API/upgrade/policy checks pass. All 47 UI tests pass,
  including Newznab setup, SABnzbd secret preservation and protocol-specific
  download controls. UI build passes with no added application dependency.
- Newznab/SABnzbd protocol fixtures cover source credentials, NZB descriptor
  validation, stale selections, duplicate prevention, lost upload replies,
  completed post-processing, failed/changed/duplicate jobs, path containment,
  symlinks, explicit TV review and source retention. Uncertain submissions retain
  their attempt fence through manual retry/reopen rather than resubmitting.
- Real portable SABnzbd 5.1.3 was downloaded from the official release and its
  published SHA-256 verified. Its isolated local profile fetched generated yEnc
  video articles from a local NNTP fixture, assembled actual movie/TV videos,
  survived application database reopen while articles were held, and passed
  FFprobe/import verification. TV mapping required review and completed sources
  retained identical bytes. No owner provider, indexer or media was used.
- Real acceptance first caught SABnzbd 5's UUID identities and its single-file
  history storage path. Both were corrected while retaining strict dedicated-job
  containment. Earlier sandboxed tests lacked socket/file-publication access;
  the same suites passed with access to their disposable services/directories.
- Clean locked npm install/audit (zero vulnerabilities), format/type/47 UI tests
  and production build pass. Full Go tests/vet/build and native process/recovery
  smoke pass. All three cross-build package checksums verify, and the extracted
  Windows package passes the full restart/backup/new-folder recovery drill.
- Browser acceptance uses a separate disposable app/config, fictional metadata,
  generated H264 video and controlled Newznab/SABnzbd protocol services. It
  verifies setup, saved-key editing, NZB-only download availability, Activity
  completion and original release/path/probe/history in the library. The browser
  screenshot is outside Git; temporary application/settings/media were removed.
  Real SABnzbd/NNTP acceptance is recorded separately above.
- The stopped owner preview's schema-9 config was backed up and verified before
  migration 010; the 0.10 executable is retained. Version 0.11 reports readiness
  200, setup_required=false and healthy database/encryption/storage/FFprobe checks.
  The usual local settings page is open at SABnzbd after sign-in. Owner media and
  existing services were not used as fixtures or scanned.
- The first Linux run (40070146621) passed the frontend/race/static/vulnerability,
  real qBittorrent, generated subtitle and existing-library checks, then failed
  the real SABnzbd test: both jobs remained Downloading until their timeout.
  Diagnostics in run 40070818945 identified missing par2/unrar runtime tools;
  SABnzbd explicitly disabled downloading despite keeping its API available.
  CI now installs the real tools and the opt-in test checks their presence before
  starting. No integration behavior was bypassed. The final Linux run passes.
  Owner Newznab/SABnzbd/shared-mount acceptance is
  separate; IPTorrents and XSpeeds account tests remain explicitly deferred.

The [final Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40071407180)
passed for commit `6e25cb6`: all 47 UI tests, Go race/static checks, real disposable
qBittorrent and SABnzbd movie/TV workflows, generated subtitle and existing-library
regressions, native/extracted-package recovery, checksummed cross-builds and the
container restart test. Real SABnzbd 5.1.3 completed both generated-media cases
in that Linux run. Govulncheck reports zero vulnerabilities affecting the app and
one advisory in a required module outside imported/called code; npm audit reports
zero. No advisory is suppressed. The constrained container snapshot reports
8.508 MiB/512 MiB and seven PIDs; this is a smoke snapshot, not a workload budget.
Private artifact `12551980524` contains the 0.11 packages/checksums with the
workflow's 14-day retention.

## Existing-library registration (0.10 development)

- Local backend/API tests and all 44 UI tests pass, including explicit scan
  requests, canonical title additions paused for monitoring, movie/TV match
  review, rejection handling and no client-supplied filesystem paths.
- Disposable domain tests cover restart/lease recovery, duplicate prevention,
  retained file identity/bytes/mtime, source replacement/change, active-download
  conflicts, consecutive TV mappings, shared-file totals, skipping/cancellation
  and symlink rejection. No owner library is scanned or used as test input.
- A real generated H264 video passes discovery, reviewed registration, double
  hashing and FFprobe (320×180, verified duration), preserving original bytes,
  physical identity and modification time without creating a download job.
- Migration tests preserve existing file IDs and all episode/subtitle references;
  invalid foreign-key migrations roll back. Existing filenames do not become
  invented release evidence for automatic subtitle selection.
- Browser acceptance uses its own config, fictional catalog titles and generated
  movie/TV videos. It verifies explicit discovery, gated title review, consecutive
  episode choices, successful in-place registration, true paths/probe data and no
  invented download history. A shared TV file counts once and both episodes show
  available at the same original path. Screenshots remain outside Git; the QA
  application, catalog and generated media were removed after verification.
- Clean npm installation, format/type/test/build, backend/static/build and native
  process/recovery checks pass. Empty-folder discovery requires an explicit API
  request and its scan history survives native process restart. All three private
  cross-build checksums verify; the extracted Windows package passes the full
  recovery drill. No dependency was added.
- Owner preview reports 0.10.0-dev, readiness 200, setup_required=false and healthy
  database/key/storage/FFprobe checks. A stopped schema-8 backup was created and
  verified before migration 009, with the previous executable retained. No scan
  of the owner library was initiated. IPTorrents/XSpeeds account testing remains
  explicitly deferred.

The [final Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40057876547)
passed for application commit `0592715`: all 44 UI tests, Go race/static checks,
real disposable qBittorrent movie/TV downloads and reviewed imports, generated
subtitle media, generated existing-library registration, native/extracted-package
recovery, checksummed cross-builds and the container restart test. Govulncheck
reports zero vulnerabilities affecting the application and one advisory in a
required module outside imported/called code; npm audit reports zero. No advisory
is suppressed. The constrained container snapshot reports 10.36 MiB/512 MiB and
seven PIDs; this is a smoke-test snapshot, not a workload performance claim.
Private artifact `12546317672` contains the 0.10 packages/checksums and has the
workflow's 14-day retention.

## Direct indexers and title details (0.9 development)

- Local Go/API/static checks pass. The 39 UI interaction tests cover public
  source creation, private-secret preservation, offline disabling, detail links,
  cast/crew/storage/history and retained cached content after refresh failure.
- Independently authored IPTorrents/XSpeeds protocol fixtures verify HTTP
  method/query/form, cookie/agent, freeleech, size/seed normalization, unknown
  layouts/challenges, same-origin links, redirect refusal and cancellation.
  RSS tests cover NYAA namespace fields, duplicate/foreign links and malformed
  feeds. Direct metainfo uses the shared bounded torrent identity validator.
- Managed-source tests cover encrypted storage, revisions/generations, source
  fairness, partial/all failure, removed/disabled selection refusal, queued-job
  independence and original source history. Scheduler tests cover managed
  connections without the legacy single Torznab configuration.
- Movie import details survive reopen. TV shared files retain grouped episodes
  and count once in totals. Backup recovery preserves encrypted indexer sessions;
  an indexer-only config with a missing key refuses startup.
- The public NYAA RSS endpoint returned HTTP 200 and 75 release entries with
  size/seeder fields through PowerShell on this machine. The opt-in Go adapter's direct socket was denied by Windows
  (`connectex` access permissions), so live app-to-NYAA acceptance is pending.
  This is recorded as a failed live check, not a passing integration. No public
  torrent was retrieved. Private IPTorrents/XSpeeds account acceptance also
  remains pending owner sessions entered privately in the app.
- Clean npm install/audit reports zero vulnerabilities. Private packages for
  Linux amd64/arm64 and Windows amd64 were built and all three checksums verify.
  The extracted Windows package passes the full disposable restart/recovery drill.
- Browser acceptance uses a separate port/config and fictional movie/TV metadata.
  It verifies title navigation, credits/links, a real generated-video import's
  technical facts/source/history, focused Activity, TV seasons, RSS save/test,
  disable/re-enable and blank-secret editing. Narrow layout was corrected from
  the observed page. Screenshots are retained outside Git; generated QA media
  and its temporary application/configuration were removed after acceptance.
- The owner preview reports 0.9.0-dev, readiness 200 and setup_required=false.
  A stopped schema-7 backup was verified before migration 008; the previous
  executable is retained. Local installation/credential checks pass. Owner
  provider settings and catalog were not used as test fixtures.

The [final Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40048388113)
passed for application commit `0b0cb00`: all 39 UI tests, Go race/static checks,
real disposable qBittorrent movie/TV downloads and reviewed imports, generated
subtitle media, native/extracted-package recovery, checksummed cross-builds and
the container restart test. Govulncheck reports zero vulnerabilities affecting
the application and one advisory in a required module outside imported/called
code; npm audit reports zero. No advisory is suppressed. The constrained
container snapshot reports 8.41 MiB/512 MiB and seven PIDs; this is a smoke-test
snapshot, not a workload performance claim. Private artifact `12542446904`
contains the 0.9 packages/checksums and has the workflow's 14-day retention.

## Production hardening (0.8 development)

- Local backend/API tests and static checks pass; all 35 UI interaction tests
  pass, including the first-administrator setup checklist, deferred Torznab,
  explicit storage checks, recovery review and failed-check retry.
- The native disposable-process drill passes startup/restart, duplicate-config
  process rejection, live-backup refusal, verified offline snapshot/new-folder
  restore, retained account/preferences, revoked sessions, held workers and
  explicit recovery release without enabling schedules. Source backup checksums
  still verify after restore. No owner services or media are test fixtures.
- Unit checks cover encrypted-provider recovery, missing-key refusal, changed
  checksums, manifest path rejection, migration drift, overlapping storage roots,
  probe cleanup/source preservation, concurrency and invalid recovery markers.
- Clean locked npm installation and audit report zero known vulnerabilities.
  Local govulncheck v1.9.0 could not reach vuln.go.dev because this Windows
  environment denied the socket; Linux CI runs the required scan.
- Packaging cross-builds Linux amd64/arm64 and Windows amd64, includes only the
  executable, whitelisted documentation and dependency manifests, and produces
  SHA256SUMS. Linux CI verifies/extracts the amd64 package and runs the same full
  native recovery drill. Its container smoke is capped at 512 MiB, one CPU and
  256 PIDs; this is a constrained startup test, not a NAS workload benchmark.
- Owner reports OpenSubtitles connected. Actual owner-media subtitle downloading
  remains untested until registered media exists. Torznab and the shared download
  mount remain deferred. The adapter omits the provider's default machine-
  translation filter to avoid a canonical redirect and retains local filtering.

The first hardening Linux run (40027066795) passed UI/race/static checks, then
correctly failed the new vulnerability gate: setup-go selected Go 1.27.0 from
the old module minimum and reported 12 reachable standard-library advisories,
all fixed by 1.27.3. The minimum is now 1.27.3, matching the existing local and
container builds. The check remains enforced; no advisories are suppressed.
Disposable browser verification passed checks/recovery resume and optional
Torznab. It also caught settings-anchor scrolling and short-viewport sidebar
overflow, both corrected before the preview upgrade. The preview reports
0.8.0-dev and readiness 200 after a stopped, verified config backup; the old
executable and original settings remain available. No owner secrets were shown.

The [final Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40028358946)
passed at code commit a954a1c: all 35 UI tests, race/static checks, the patched
Go vulnerability scan and npm audit, actual disposable movie/TV qBittorrent
imports, generated subtitle/embedded-track acceptance, native recovery drills,
all three cross-builds/checksums and the extracted Linux amd64 recovery drill.
The authenticated container restart/persistence/health/FFprobe check passed
under 512 MiB, one CPU and 256 PIDs. Its final idle snapshot was 8.484 MiB and
7 PIDs; that is neither a peak nor a sustained workload budget. govulncheck
reported zero reachable vulnerabilities, plus one advisory in a required module
whose vulnerable code is not called; no exclusions were added.

The private artifact `reelscarey-private-0.8.0` (12533833547, 18,165,985 bytes)
contains the three archives and SHA256SUMS and is retained for 14 days. Locally,
all final archive hashes were verified and the extracted Windows amd64 package
passed the same full recovery drill. Final native build metadata identifies
Go 1.27.3 and a954a1c. The owner preview was backed up and upgraded to that build;
readiness is 200, with existing account/settings retained. The disposable browser
installation and fixture helper were removed; its recovery-success screenshot
is retained at `data/preview/hardening-qa.png` (ignored, not uploaded).

NAS/arm64 runtime validation, sustained budgets, independent security review,
release provenance and license choice remain gates. Owner Torznab/shared-mount
and actual owner-media subtitle download acceptance remain deferred.

## Subtitle providers, language preferences and automatic downloads

English full dialogue, signs/songs and forced tracks are independent defaults;
automatic work starts paused. Torznab owner setup remains deferred. The first
real adapter is OpenSubtitles.com; the owner has since reported its connection
saved. Owner-media download acceptance remains separate.

- Real-adapter protocol tests pass for login/VIP hosts, canonical sorted movie/TV
  requests, identity/language/type normalization, signed-download credential
  isolation, hostile locations/redirects, bounded responses, quota/rate cooldowns
  and bounded authentication retries. No external account is used by these tests.
- Workflow tests pass for three distinct tracks, embedded/sidecar detection,
  invalid/unlabeled sidecar preservation, explicit review, shared episode files,
  stale choices/revisions, lease reclamation, connection/pause/monitoring fences,
  encrypted credentials, quota preservation and no duplicate downloads after reopen.
- Partial/staged/linked/published journal recovery tests reopen the database and
  complete from saved bytes without another provider call. Collision checks retain
  existing destinations and partial files; cleanup removes only proven committed
  staging links. Generated videos remain byte-for-byte unchanged.
- Migration checks preserve prior encrypted integration bytes/timestamps and
  enabled automation settings, while adding paused subtitle defaults. Broken
  migration rollback/checksum/newer-version checks pass with schema version 7.
- Clean locked frontend install reported zero known vulnerabilities. Formatting,
  TypeScript, all 31 interface tests and production build pass. Initial new UI
  assertions used an outdated label and did not await the independently loaded
  file list; both assertions were corrected before the passing full run.
- Full Windows Go tests, vet, executable build and disposable native restart smoke
  pass. The native check retains all English track variants, an added language,
  enabled/hash-only preferences and authenticated sessions across restart with
  OpenSubtitles/Torznab unconfigured. Local Windows race/container checks require
  Linux CI; no local Docker/C compiler is available.
- Opt-in FFmpeg/FFprobe acceptance passed locally for a generated video with
  embedded English full/signs/forced streams (zero provider calls), and a second
  generated video receiving three distinct SRT sidecars. Video bytes are preserved.
- Browser QA caught a new imported file returning an error before its first
  subtitle job existed. File/list handling now accepts an absent job; a backend
  regression test confirms visibility while automatic work is paused.

- Disposable browser acceptance connected the fixture provider through the real
  form, searched a generated video, required explicit review before a forced-track
  download, then enabled automatic work to complete the full/signs tracks. The UI
  reported three available tracks; all three SRT files exist and the video SHA-256
  is unchanged. The temporary server and both QA roots were removed afterward.
- The owner's stopped configuration was fully backed up before schema 7. The
  preview upgraded without reading/changing owner credentials or adding media.
  Its detached process later stopped without an application error; readiness
  correctly failed. It was restarted in a persistent terminal session and readiness
  returned 200. The owner can complete OpenSubtitles setup privately; a successful
  live owner-account connection has not yet been confirmed.

- The [complete Linux run](https://github.com/faaaaq/ReelScarey/actions/runs/39992048520)
  at feature commit 1c5ec65 passed all 31 interface tests, full Go race/vet checks,
  real disposable qBittorrent movie/TV imports, generated FFmpeg/FFprobe subtitle
  acceptance, native restart smoke and Docker build/container restart smoke.
  GitHub's test-tool installation was unusually slow but completed successfully.
- Packaging review found stale 0.3 version arguments in Docker/Compose. Both now
  identify the subtitle milestone as 0.7. Its first follow-up run (39995833837)
  passed UI/race/static checks but timed out while downloading Ubuntu test tools
  from the hosted runner's Azure mirror. The prior successful run downloaded
  74.4 MB at only 63.9 kB/s and narrowly finished under the 20-minute limit.
  CI now selects Ubuntu's primary HTTPS archive in the runner's mirror list,
  retaining package signatures and the existing timeout.
- The [final successful Linux run](https://github.com/faaaaq/ReelScarey/actions/runs/40014778675)
  at code/packaging commit f669038 passed all 31 interface tests, full Go race/vet,
  real qBittorrent movie/TV imports, generated subtitle-media acceptance, native
  restart smoke and Docker build/container restart smoke. The corrected container
  reports 0.7.0-dev. No owner provider credentials or media were used by CI.

See SUBTITLES.md for exact format/matching/cache/recovery limits.

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

- ARM runtime/NAS deployment coverage, sustained resource measurements,
  broad accessibility audits and independent security review remain future work.
- Owner-service download/import acceptance requires the indexer and shared mount
  described above. TV ingest and the OpenSubtitles adapter are now implemented;
  their generated-media/real-client evidence is recorded in the newer sections.
- Sessions use a fixed 24-hour lifetime. Rate-limit counters are memory-resident
  and reset on restart; durable sessions and library data are SQLite-backed.
- The project license still needs the owner's choice before public distribution.

## Reproduce

Follow README.md to build the UI before Go, then run `go test ./...`,
`go vet ./...` and `node scripts/smoke.mjs <binary-path>`. The smoke test creates
and removes only its own temporary config directory. `scripts/container-smoke.mjs`
is intended exclusively for the disposable `reelscarey-ci` container in CI.
See AUTHENTICATION.md for first setup, password recovery and key backup.
