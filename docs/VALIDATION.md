# Validation record - 2026-11-08

## Fast local-tag music matching (2026-11-08)

Implementation checks on 2026-11-08 (Windows development host):

- Complete embedded tags now create/reuse a local catalog and match without
  MusicBrainz, AcoustID or fingerprint calls. A 1,200-file generated well-tagged
  fixture matched in about 1.55 seconds with zero provider calls.
- Mixed fixtures fall through to AcoustID and then MusicBrainz only when tags are
  insufficient. Genuine tagged MBIDs are reused; synthetic `local-*` IDs are never
  written to MusicBrainz tag fields. Multi-disc, missing disc, duplicate position,
  case/punctuation and idempotent rerun cases are covered.
- Finalize/organize works from local tag rows and still uses the existing
  no-overwrite, retained-backup and journal safety.
- Full Go suite passed with the known environment-only 64 GiB sparse-fixture test
  skipped; `go vet` and `go build` passed.
- Frontend `format:check`, `check`, `test` (52 files, 299 tests) and `build`
  passed. The unrelated Requests test flaked under load once, then passed in
  isolation and in the complete rerun.
- Not exercised: owner media, live MusicBrainz/AcoustID accounts, Docker/Linux
  acceptance or a real 11k-file scan. Streaming/probing an initial 11k library is
  disk-bound; only the matching phase is covered by the 1,200-file fixture.

## MusicBrainz cache, persistent cooldown and optional mirror (2026-11-08)

Implementation checks on 2026-11-08 (Windows development host):

- Migration 074 adds `musicbrainz_cache` (`key`, raw `payload`, `expires_at`,
  `negative`) with `musicbrainz_cache_expiry`, plus
  `musicbrainz_provider_state` seeded at `id=1`. It rebuilds
  `integration_configs` only to add the encrypted `musicbrainz` name and
  recreates its activity triggers; existing rows, ciphertexts and foreign keys
  are preserved. Schema-version assertions now expect 74.
- MusicBrainz responses are cached by base URL + endpoint + sorted query. The
  TTLs are 30 days for artist/release-group/release/track metadata, 7 days for
  searches and 1 day for explicit 404/empty metadata. Invalid, truncated,
  oversized and non-404 error bodies are never stored. Expired rows are removed
  in bounded batches of at most 500. Cache read/write/cleanup failures log a
  warning and continue against the provider.
- A `429`/`503` parses `Retry-After` as seconds or an HTTP date and persists the
  retry window and message in `musicbrainz_provider_state`. The next request
  returns `music_rate_limited` with `Retry-After` without sleeping. The matching
  workflow stops in `review`/`ready` with the matched rows intact and an explicit
  cooldown warning; it does not retry in a loop.
- The optional base URL defaults to `https://musicbrainz.org/ws/2`, is encrypted
  through `securestore`, and is applied at startup or when saved. Public pacing
  is one request/second; local/private mirrors use 50 ms. HTTPS is accepted for
  any host; plain HTTP only for localhost, loopback, RFC1918 and IPv6 ULA.
  Credentials, query, fragment and path traversal are rejected.
- `GET/PUT/DELETE /api/v1/library/music/musicbrainz` returns
  `base_url/default_base_url/using_default/cooldown_until/message/cache_entries`
  and never a secret. The Music Import page shows the provider, cache count and
  cooldown, saves/resets the URL, and labels a paused workflow as a provider
  cooldown rather than a failed scan.
- `internal/music/musicbrainz_test.go` covers cache hit/miss, TTL classes,
  expiry and bounded cleanup, negative 404 and empty metadata, ignored
  invalid/error responses, fail-open cache failures, persisted cooldown across a
  database reopen, Retry-After seconds/HTTP dates/bounds, public versus local
  pacing, URL validation and status fields. `internal/music/workflow_test.go`
  covers a rate-limited match workflow with preserved matched rows and a single
  provider call. `internal/httpapi/music_test.go` covers auth, CSRF, status
  fields, save/reset and method rejection. `internal/database/music_upgrade_test.go`
  covers the new tables, seeded state, expiry index and `musicbrainz`
  integration name.
- `go test ./internal/music ./internal/httpapi ./internal/database
  ./internal/maintenance ./internal/securestore -count=1`, `go test ./...
  -count=1`, `go vet ./...`, `go build ./cmd/reelscarey`, and frontend
  `format:check`, `check`, `test` (52 files, 299 tests) and `build` passed. The
  only broad-suite failure is the known environment limit
  `TestLargeExistingRegistrationDoesNotReadWholeVideo` in `internal/transfer`,
  which cannot create its 4 GiB temporary fixture on this host ("There is not
  enough space on the disk").
- Not exercised here: a real MusicBrainz request, a real public or local mirror,
  an actual rate-limit window against MusicBrainz, a Docker/Linux acceptance run
  or owner media. Tests use local fake MusicBrainz responses, generated database
  rows and temporary paths only. No MusicBrainz mirror is bundled with the
  image.

## Music resumable import workflow (2026-11-08)

Implementation checks on 2026-11-08 (Windows development host):

- Migration 073 adds the resumable workflow columns to `music_scans`
  (`full_scan`, `phase`, `progress_total`, `progress_done`, `current_path`,
  `message`, `skipped_count`, `request`, `requested_at`, `work_owner`,
  `work_until`) plus path/work indexes. Existing state, counts and error fields
  are unchanged.
- A scan does a name-only pre-count with the walk's extension/depth/symlink/file
  limits, then writes `progress_done`, `file_count`, `skipped_count` and a
  message after each bounded batch. The walk still enforces 20,000 files, 24
  levels, no symlinks, a 30-minute budget and the cancellation check.
- Normal scans skip unchanged registered paths and carry unchanged
  matched-but-unregistered paths forward with their artist/album/track IDs and
  current tag parser version, without re-probing. Previously unresolved paths
  are reconsidered against the current catalog. Changed size or saved
  nanosecond mtime re-reads the file. A full scan re-examines every eligible
  file but reuses an unchanged digest/tag observation; registering never
  duplicates a `music_files` row for the same relative path.
- `POST /scans/{id}/match-all {reviewed:true}` and
  `POST /scans/{id}/finalize {reviewed:true}` return 202 with a queued request.
  `RunWorkflow` claims one record with a database owner/lease, renews it per
  batch, matches tags then fingerprints remaining files, or registers and then
  applies one edit plan at a time. `RecoverScans` re-queues interrupted requests
  with an explicit message and clears stale leases; interrupted walks still fail.
- `internal/music/workflow_test.go` covers pre-count/live progress, the skip and
  carry-forward policy, unresolved reconsideration, full rescan without duplicate
  library rows, restart re-queue and one-at-a-time claiming, request
  validation/consent, tag-then-fingerprint matching, unavailable-AcoustID
  warnings, finalize registration + sequential edit organization and partial
  failure staying in review. `internal/httpapi/music_test.go` covers the new
  routes' authentication, CSRF, consent validation, progress fields and method
  rejection. All fixtures use generated files and fake providers; no owner media
  or real AcoustID/MusicBrainz account was used.
- `go test ./internal/music ./internal/httpapi ./internal/database
  ./internal/maintenance -count=1`, `go test ./...`, `go vet ./...` and
  `go build ./cmd/reelscarey` (after `web` `npm run build`) passed. The only
  broad-suite failure is the known environment limit
  `TestLargeExistingRegistrationDoesNotReadWholeVideo` in `internal/transfer`,
  which cannot create its 4 GiB temporary fixture on this host ("There is not
  enough space on the disk").
- Not exercised here: a real AcoustID key/fpcalc run, live MusicBrainz rate
  limiting, a Docker/Linux acceptance run or the Music Import UI controls (a
  separate change owns `web/src`). Match All/Finalize provider behavior uses
  fake MusicBrainz/AcoustID responses.

## Music catalog discovery (2026-11-08)

Implementation checks on 2026-11-08 (Windows development host):

- Reviewed discovery resolves selected unresolved music files through strong
  evidence only: a release-group MusicBrainz ID, or one exact normalized album
  artist and album title with year only used for disambiguation. It creates the
  missing artist/release group, loads tracks and re-matches the files.
- Provider work is bounded to 500 files, 10 album groups and a time budget.
  Ambiguous/missing evidence, provider failures and deferred groups remain
  unresolved with explicit messages; scan counts are refreshed.
- Tests cover release-group and exact tag paths, ambiguous/missing evidence,
  duplicate artist reuse, provider failure retention, deferral, count refresh,
  re-match, HTTP auth/CSRF/validation and the Music Import UI action.
- Combined `go test ./... -count=1` passed for every package except
  `TestLargeExistingRegistrationDoesNotReadWholeVideo` in `internal/transfer`,
  which could not create its 4 GiB temporary fixture on this host. Targeted
  music, HTTP, automatic-matching and existing-library tests pass.
- `go vet ./...`, `go build ./cmd/reelscarey`, frontend `format:check`, `check`,
  `test` (51 files, 280 tests) and `build` passed.
- Not exercised here: a real MusicBrainz account/library, live TMDb/library
  acceptance, owner media or a Docker/Linux run. Provider behavior uses local
  fake MusicBrainz/TMDb responses.

## Cross-season existing-library matching (2026-11-08)

Implementation checks on 2026-11-08 (Windows development host):

- Automatic existing-library matching now tries the filename's canonical
  season/episode first, then one unique exact normalized episode title across
  the show's bounded eligible seasons. The generated Dan Da Dan-shaped fixture
  maps a file labeled `S02E11` to TMDb `S01E11`, registers the canonical episode,
  and reports the cross-season mapping after verification.
- New fixtures keep duplicate cross-season titles and numbered files without a
  usable title in review. Existing season/episode matching still passes.
- A saved show that predates a newly added TMDb season refreshes and loads the
  new season before matching. A simulated metadata-refresh revision race retries
  once with the current show revision instead of pausing or reporting a missing
  season.
- `go test ./... -count=1` passed for every package except
  `TestLargeExistingRegistrationDoesNotReadWholeVideo` in `internal/transfer`,
  which could not create its 4 GiB temporary fixture on this host ("There is not
  enough space on the disk"); that check is environment-limited and unrelated to
  matching. Targeted automatic/existing/bulk transfer tests pass.
- `go vet ./...` and `go build ./cmd/reelscarey` passed after building
  `web/dist`; no frontend source changed.
- Not exercised here: a live TMDb account, owner media, an existing library or a
  Docker/Linux run. All provider behavior used generated fixtures.

## Music import discovery (2026-11-08)

- The Music toolbar and empty state now link directly to **Music Library
  Import**. The main **Library Import** Discover panel lists Music with the
  existing video folders, and the Settings library index includes Music Import.
- Music scanning remains the reviewed MusicBrainz tag workflow; the new controls
  route to it rather than pretending audio is TMDb video discovery.
- Installation docs now show an optional `/data/music` bind mount and explain
  that the scan folder is relative to the media root and case-sensitive.
- Web `format:check`, `check`, `test` (51 files, 280 tests) and `build` passed.
  No Go behavior changed for this UI discovery fix.

## Automatic and library metadata refresh (2026-11-08)

Implementation checks on 2026-11-08 (Windows development host):

- Migration 070 adds a durable per-title metadata refresh queue. Adding a
  movie/show, registering an existing file and committing a download import
  queue the title automatically; it runs independently of the paused-by-default
  schedule. Library pages queue every title in the current collection with a
  full-season pass.
- `go test ./...`, `go vet ./...` and `go build ./cmd/reelscarey` passed,
  including new queue scope, automatic-add, full-season, API CSRF and migration
  tests.
- Web `format:check`, `check`, `test` (51 files, 278 tests) and `build` passed.
- Not exercised here: a live TMDb account, a real library-wide provider pass or
  a Docker container. Generated provider fixtures cover queueing, library scoping
  and all-eligible-season loading.

## Import method preference (2026-11-08)

Implementation checks on 2026-11-08 (Windows development host):

- Migration 069 adds the persisted `import_mode` setting (`hardlink` default,
  `copy` alternative). First-run setup saves the chosen method in the same
  transaction as the administrator; existing installations can change it in
  Settings.
- `go test ./...`, `go vet ./...` and `go build ./cmd/reelscarey` passed,
  including new setup persistence, settings validation/preservation and
  copy-only importer tests.
- Web `format:check`, `check`, `test` (51 files, 277 tests) and `build` passed.
- Not exercised here: a live Docker hardlink-enabled parent mount or a real
  download client. Copy-only behavior and legacy-default preservation are
  covered by generated local fixtures only.

## Release feed sync (0.40 development)

Implementation checks on 2026-11-08 (Windows development host):

- `go test ./...` passed, including new feed-matching, dedupe, registered-file
  refusal, profile-rejection, TV reservation, adapter-request, migration and HTTP
  contract tests.
- `go vet ./...` and `go build ./cmd/reelscarey` passed.
- Web `format:check`, `check`, `test` and `build` passed. One full web run
  reported one test file failed while all 268 reported tests passed and no
  failure text was emitted; the immediate rerun passed 48 files and 269 tests.
  That first run is recorded as an unresolved harness/flake signal, not a pass.
- No live indexer, download client or upgrade-path migration was exercised here.
  The migration and default-enabled behavior are covered by local fixtures only.

## Version 0.23 — storage and release-label follow-through

- The guided main-root migration and folder browser are implemented without a
  database migration or new runtime dependency. Standard TV matching accepts
  explicit 1x02, spaced S01 E02 and Season 1 Episode 2 labels, preserving exact
  identity, single-episode and pack/range rejection. Manual suggestions remain
  subject to the existing canonical mapping review.
- All 165 frontend tests, formatting/type/build gates and the full Linux backend
  test/vet/race suite pass. New tests exercise mounted-scope browsing, pagination,
  links, traversal, protected paths, bounded listings, cancellation and unchanged
  files; API requester denial/read-only methods; migration ownership, reviewed
  digests, verified backup, source changes, foreign destinations, interruption,
  mount activation, idempotence and ordinary recovery resume.
- The read-only Docker storage rehearsal uses separate disposable config/media/
  backup/target volumes and generated FFmpeg video. It verifies external-folder
  browsing, authenticated settings/catalog retention, offline copy and retry,
  old-mount activation rejection, pending-startup blocking, actual mount change,
  completed activation, explicit recovery resume and container restart. All
  original media/subtitle bytes and copied video hashes match; integrity and
  foreign-key checks pass. The live installation is never used as a fixture.
- The five-minute constrained workload completes 22,686 authenticated reads and
  60 revision-checked settings writes against 1,000 generated catalog titles with
  eight concurrent readers and no failures. Read p95/p99 are 17/27 ms. Across 72
  resource samples, working memory peaks at 41.54 MiB and finishes at 13.39 MiB;
  peak process count is 14. Limits are one CPU, 384 MiB and 128 processes. Restart,
  preserved catalog and SQLite integrity/relationships pass. This defined shared-
  host test is separate from NAS/ARM and representative production sizing.
- Integration testing caught and corrected an import-journal table reference and
  a top-level publication helper assumption. The migration recovery marker uses
  the existing reviewed-resume format; idempotent activation does not reintroduce
  a hold after review. The complete Docker rehearsal passes after these fixes.

Behavior, commands and bounds: [Storage](STORAGE.md), [workload checks](WORKLOAD.md).

### Live version 0.23 upgrade

- The previous image is retained as `reelscarey:before-remaining-milestones-20261029`.
  An early backup and fresh final stopped backup were created and independently
  verified. Final snapshot:
  `data/backups/before-remaining-milestones-20261029/configuration-final`.
  The private deployment settings are retained beside it.
- The live app now runs `0.23.0-storage`, schema 37, from the complete tested
  image. All 80 existing tables including the migration ledger match the final
  backup exactly; key and setup-token bytes match. No database migration was
  needed. Integrity and foreign-key checks pass, and first-run setup remains
  required. The running installation contains no acceptance fixtures.
- Controlled restart, all nine installation checks, Windows readiness, seven
  direct routes and all 39 referenced embedded assets pass. The persistent WSL
  host process remains active. The same media/config/download/backup volumes,
  read-only protections, UID 10001 and restart policy are retained. Deployment
  version settings now agree with the running image.
- The final image repeats the complete storage and backup/restore rehearsals and
  the previously completed seven-feature container regression. Final focused race
  checks for storage, maintenance/recovery, quality, HTTP and commands also pass.
- Delivery is tracked in [pull request #3](https://github.com/faaaaq/ReelScarey/pull/3)
  against primary `codex/foundation`. Hosted run `40906762719` did not start:
  GitHub reports failed recent account payments or a spending limit requiring an
  increase. Hosted CI is not recorded as passed. Repository rules, statuses and
  billing settings were not changed; local/container checks passed independently.

## Version 0.22 — seven completed follow-through features

- All 163 frontend tests, formatting, type checks and the production build pass.
  Full Go tests, vet and race tests pass in Linux containers. The final Docker
  build independently repeats the frontend/backend/static gates and embeds the
  tested UI. No runtime service or dependency was added.
- New tests cover metadata pause/revision fences and canonical-ID preservation;
  immutable AniList refresh/apply; effective per-show tracks; SRT/VTT/ASS/SSA
  timing and reviewed replacement/rollback, interrupted publication and foreign
  output preservation; subscription baselines, deduplication and pause fences;
  atomic YouTube bulk queues; source/codec ranking and Usenet age evidence; and
  complete-batch matching with stale catalog/source/reservation rejection.
  Requester denial, CSRF and direct web routes are covered at the HTTP boundary.
- The actual read-only, network-isolated feature container uses generated FFmpeg
  video and a clearly isolated inspection-only YouTube fixture. It verifies
  paused metadata scheduling/provider waiting, non-Anime refresh rejection,
  per-show preference save/reset, completed subtitle timing/rollback/upload,
  persisted scoring, source baselines, reviewed bulk queues and reviewed bulk
  registration. Video hashes, retained original subtitles, SQLite integrity and
  foreign keys pass independent checks. A fixture category assumption was
  corrected before the successful complete repeat. Real AniList refresh facts
  use provider contract tests; this fixture does not prove public YouTube downloads.
- The backup container rehearsal passes manual backup, ZIP download/upload,
  stale-digest refusal, controlled restart, session revocation, catalog/settings
  restoration, retained safety backup and recovery review. It now backs up
  enabled metadata and YouTube schedules and verifies both restore paused.
- Generated-media integration regressions pass for subtitle inspection,
  organization, existing-library registration, reviewed YouTube imports and
  FFprobe audio-language tags. Real isolated authenticated qBittorrent movie,
  TV and Anime packs pass; both automatic Anime schedulers preserve review gates
  and generated seeding sources. Real SABnzbd was not rerun for this change.

### Live container upgrade

- Old image retained as `reelscarey:before-seven-features-20261029`. A fresh
  stopped schema-31 backup was created and independently checked with the new
  image at `data/backups/before-seven-features-20261029/configuration-final`.
  Private deployment settings are saved beside it; original backups are retained.
- The running single-container app is now `0.22.0-seven-features`, schema 37.
  All 72 pre-existing non-ledger tables match the final backup using their original
  columns; previous migration rows and key/setup bytes match. Integrity and
  foreign-key checks pass. New schedules are paused and first-run setup remains
  required. No fixture account or media was created in the live installation.
- Controlled restart, Windows localhost readiness and all six relevant direct
  web routes pass. All nine installation checks are ready. The container retains
  the existing volumes, read-only root/download mount, non-root UID 10001,
  dropped capabilities and persistent WSL host launcher. Observed idle memory
  after restart was approximately 10 MiB; this is not a sustained-load benchmark.

Feature scope and bounds: [SEVEN_FEATURES.md](SEVEN_FEATURES.md).

### GitHub delivery

The implementation and deployment evidence are in pull request
[#2](https://github.com/faaaaq/ReelScarey/pull/2), targeting the repository's
primary `codex/foundation` branch. Hosted application-check run `40846711182`
did not start: GitHub reports failed recent account payments or a spending limit
requiring an increase. This is not a passing hosted check. The local/container
results above are independent evidence. No repository rules, external statuses
or billing settings were changed.

## Configuration and library database backup manager

- Schema 31 adds the persistent paused backup schedule. Live WAL snapshots,
  encrypted provider preservation, ZIP upload/download, strict archive/path/size
  checks, stale review fencing, retention protections and both session types are
  covered by isolated backend/API tests. Due scheduling honors recovery hold.
- Interrupted restore tests simulate each boundary while old configuration files
  move aside and new files publish; every case recovers the latest previous
  settings and matching key. Restore closes HTTP, workers and SQLite, excludes
  maintenance commands, saves a verified safety archive, checks the new state and
  reopens with worker review required. Temporary copies are cleaned conservatively.
- All 155 frontend tests pass, including schedule/CSRF, manual job controls,
  upload verification failures, explicit restore consent and deletion review.
  Backend tests/static checks and race checks pass in Linux containers.
  The complete Docker build repeats frontend/backend/static verification.
- The actual read-only, network-isolated container rehearsal verifies manual
  backup, ZIP download/upload, stale digest rejection, controlled restart,
  original session revocation, restored catalog/settings, retained safety backup,
  paused schedules, critical installation checks, explicit recovery resume and
  deletion. Its first run caught a missing `/backups` deep route; that is fixed,
  covered by a regression check and passes the repeated complete rehearsal.
  A test-only setup-code encoding mistake was also corrected. The repeatable
  `scripts/backup-container-smoke.sh` uses fresh temporary volumes and is in CI.
  No owner account, provider connection or media was used as a test fixture.

### Running installation and GitHub integration

- The previous image is retained as `reelscarey:before-backup-manager-20261029`.
  The stopped schema-30 configuration was backed up and independently verified
  before replacing the container. Final private snapshot:
  `data/backups/before-backup-manager-20261029/configuration-final`.
- The running single-container app is `0.21.0-backups`, schema 31, with a separate
  persistent `/backups` volume. All 71 pre-existing tables match the final backup;
  encryption-key and setup-token bytes match. Full SQLite integrity, foreign-key
  checks, Windows readiness, the `/backups` deep link and all nine doctor checks
  pass. Backup storage is writable by the non-root app and scheduling is paused.
  The existing first-run setup state is preserved; no fixture account was created.
- GitHub's default branch is `codex/foundation`. Its existing Docker publishing
  workflow was merged into the feature history and preserved. The complete app
  and milestone history are tracked in pull request #1. GitHub Actions run
  `40836943861` could not start: the account reports failed billing payments or a
  spending limit. This is not recorded as a passing hosted check. The separate
  Entire status also reports no reviewer approval; no findings block the change.
  Repository rules and external status checks were not modified.


## Running-container upgrade and repository cleanup

- The original running image and its stopped schema-28 configuration were
  retained before deployment. The offline backup contains the database, matching
  encryption key and setup token; separately checksummed archives retain staging,
  media/download folders and private deployment settings. The first rehearsal
  correctly refused supplementary archives beside the configuration manifest;
  the clean configuration snapshots were separated and reverified. Final snapshot:
  `data/backups/before-single-container-upgrade-c1ae459/configuration-final`.
- A private restore with networking disabled migrated the real snapshot to schema
  30, passed integrity/foreign-key checks, preserved key/setup bytes and fetched
  all 35 web assets before and after restart. Its temporary container/volume were
  removed. This did not contact owner providers or download services.
- The running Compose service now uses `0.20.0-c1ae459-optimized`, schema 30,
  the same persistent volumes and read-only download mount. Sixteen table contents
  match the final pre-upgrade backup; the encryption key and setup token match.
  Controlled restart, readiness, Docker health and all nine local doctor checks
  pass. The installation had no administrator/catalog/integrations before upgrade
  and still requires first-run setup; no fixture account was created in it.
- Windows reachability exposed the WSL host exiting after its command finished.
  The hidden, idempotent `scripts/start-docker-wsl.ps1` launcher now keeps Docker's
  WSL host alive. Repeated Windows readiness checks pass beyond the previous idle
  shutdown interval. The launcher is a Docker-host convenience, not a native app
  build or another application container. It must be run again after Windows
  reboot; no Windows automatic-start task was installed.
- All 150 frontend tests, type checks and production build pass, including failed
  lazy-chunk recovery. Main JavaScript falls from 618.58 KB to 276.31 KB (gzip:
  172.62 KB to 87.42 KB); pages load on demand. Shared event rendering was moved
  out of Timeline to avoid pulling that page into the entry bundle.
- The final Docker build passes frontend and full Go test/vet gates. Runtime
  FFmpeg/FFprobe and pinned yt-dlp pass execution checks; Node, Go, pip and
  setuptools are excluded. The yt-dlp environment falls from 51 MiB to 27 MiB;
  Docker's inspected image-size metric falls from 240,653,394 to 230,147,880 bytes.
  Required graphics/media dependencies were retained after package simulation
  showed removing them would also remove FFmpeg.
- Disposable instances of the optimized image pass authenticated setup/restart,
  session/preference persistence, generated metadata update/version rollback,
  move undo and both catalog-removal choices. Runtime data and sources remain
  preserved. The deployed instance also serves all 35 assets after restart.
- Removed the obsolete standalone packager and 1,193,932,593 bytes of generated
  bin/dist output, plus eleven root-level test outputs. Operational/development
  tests, prior data subfolders and all verified backups remain. Only the active
  repository was cleaned; sibling projects and system-wide Docker caches were
  not pruned. Docker backend copies only required source and embedded web assets.

## Remaining implementation milestones — schema 30

- Final formatting, TypeScript, all 149 UI tests and production build pass;
  reviews cover removal defaults and explicit acquired-replacement approval.
  The existing bundle-size advisory remains. No new dependency was introduced.
- Full Linux Go tests, vet and the complete race suite pass. Subsequent final
  focused race regressions pass organization, transfer, YouTube, HTTP and requests.
  The final Docker image builds with the complete frontend/Go/vet gates.
  Schema assertions and backup/restore checks now cover 30 migrations; rebuilding
  request lists preserves event triggers. The first regression run found the
  missing triggers and old schema-count assertions, which were corrected.
- Generated movie and shared-TV upgrades use real FFprobe with race detection.
  Tests cover protected normal imports, acquisition without replacing the current
  video, required separate review, exact shared-episode mappings, rejection of
  partial mapping, changed container extension, retained client sources and
  rollback of original download provenance. Existing independent-copy, changed
  source, symlink, occupied-destination and interrupted-swap recovery tests pass.
- Generated YouTube replacement passes real FFprobe with selected VTT publication,
  separate replacement approval, retained original/acquisition bytes and restored
  provenance after rollback. Acquisition transport is isolated fixture data, not
  live YouTube network acceptance. Selected VTT copies are additive and remain
  retained across rollback; subtitle timing correction is not implied.
- Real disposable qBittorrent movie, TV and multi-season Anime pack regressions
  pass on Linux using owned generated videos and private localhost profiles.
  They preserve seeding data and reviewed mappings across reopen. Replacement
  acquisition itself has generated-media/protocol-fixture evidence; no owner
  download client, account or library was used.
- Private TMDb/Trakt fixtures verify fixed hosts, user bearer tokens, encrypted
  per-list storage, request-account isolation and actual periodic request imports.
  Existing changed-list revision fences and public-provider regression tests pass.
  Expired tokens require owner renewal; OAuth enrollment/refresh is not automated.
  No live private list or provider account acceptance is claimed.
- Disposable read-only-root containers pass authenticated setup, frontend,
  health, persistent settings/sessions and actual restart checks. Generated media
  moves, verified retagging, version rollback, move undo and catalog removal via
  both keep-files and retained-trash choices pass through the application API;
  client sources remain unchanged. Test containers/volumes were removed and the
  running owner installation was not modified.
- Direct continuous/private IMDb synchronization remains open: the official
  multi-step export does not provide a stable list-export URL. Existing MDBList
  synchronization and CSV snapshots remain available. Optional future scope and
  owner-service/hardware/security acceptance are still tracked separately.

## Verification, additional title containers and organization undo

- Baseline frontend formatting, types, 146 tests and production build pass.
  The final interface passes formatting, types, all 147 tests and production
  build, including the explicit undo-review gate. The existing bundle-size
  advisory remains. No frontend dependency was added.
- The complete Linux Go suite, vet and Docker image build pass. The full Linux
  race suite passed before the move-undo addition; organization and HTTP API race
  regressions passed after it. Generated-media tests run with race detection in
  the runtime image, separately from build tests that lack FFmpeg.
- MP4, M4V, Matroska, MOV, AVI/PCM and WebM/Vorbis generated fixtures pass
  verified title updates and version rollback, preserving original seeding bytes.
  Matroska chapters, subtitles and attachment tests pass. M4V uses the compatible
  muxer and existing brand metadata; the previous MP4 muxer failed this fixture.
  AVI/AAC and WebM/Opus timing-changing rewrites are rejected and leave their
  originals unchanged. TS/M2TS title tags remain unsupported.
- Reviewed selected-file and whole-folder undo tests pass with collision refusal,
  subtitle reference restoration, retained seeding sources, unchanged future
  collection preferences and recreated-service recovery. A real separate tmpfs
  mount verifies cross-filesystem folder undo with extras and empty directories,
  alongside existing partial-copy/publication/recovery regressions, with races
  enabled. These use generated files and disposable database roots only.
- A fresh read-only-root application container passes authenticated setup,
  frontend, health and preference/session restart persistence. A second disposable
  container passes a generated movie move, verified title update, version rollback
  and reviewed path undo across an actual container restart through the HTTP API.
  Both containers and their temporary volumes were removed. The running owner
  application, its mounts and private services were not changed.
- Restricted Windows checks cannot complete localhost and hardlink-dependent
  backend tests. The supported Linux container checks above are the acceptance
  evidence. Download/YouTube replacement, private/direct IMDb list sync and
  reviewed catalog removal remain implementation work in MILESTONES.md; live
  owner-service acceptance remains separate. This is not project completion.

## Automatic cross-media discovery and history expansion

The Linux adapter successfully queried public Wikidata from both Star Wars movie
TMDb ID 11 and TV ID 105971; each returned movie and TV associations. Isolated
tests cover query identity, caching, ambiguous provider responses, exact local
library matching, provider failures, and chronological history pagination.
An upgrade fixture verifies historical creation/state backfill without inventing
intermediate transitions. History tests check that encrypted credentials and
private list identifiers are not copied to event descriptions. Owner-library
acceptance and the completeness of community-maintained Wikidata remain open.
The full Linux image build passed Go tests and vet, the complete frontend suite
passed 136 tests, and targeted Linux race checks passed. A disposable Docker
container passed the authenticated setup/restart smoke test, including event
visibility and persistence; Compose configuration validation also passed.

Per-series release rule fixtures cover codec/group labels, stored revisions,
stale edits and a rule change fencing an in-flight automatic selection. Existing
automatic and mapped-Anime suites passed on Linux; owner indexer acceptance is
still needed.

Emby monitor fixtures cover paused-by-default operation, observed sessions,
upstream failures, invalid responses, uncertain gaps and stale setting writes.
Cross-media relationship fixtures cover explicit, mutual, idempotent links.
Timeline fixtures cover recorded events and cursor pagination. These do not
replace testing against a real Emby server or a representative media library.

## Unmatched review, Emby snapshots, title folders and movie collections

Linux container tests cover unmatched scan counts, encrypted Emby settings and
bounded playback fixtures, title-folder naming and preservation of existing
files, TMDb movie collection responses and the recent-download tray. The full
frontend suite passed 130 tests. A real Emby server, owner media and real TMDb collection acceptance have
not been exercised. Emby results are on-demand latest-play snapshots, not a
complete history. Windows-hosted tests requiring Linux filesystem operations or
local test sockets failed under the restricted development environment; Linux
container tests are the deployment-relevant checks.

The request and companion-bot changes have Linux container fixture coverage for
IMDb via MDBList, signed Discord interactions, account linkage, request approval
and release selection. The full frontend suite passed (126 tests). No live
MDBList or Discord account was used; owner-service acceptance remains open.

## Request accounts, import lists and webhook notifications

- Isolated tests cover requester session persistence and revocation, denial of
  administrator APIs, account ownership, approval and duplicate retries. A
  disposable Docker container confirmed requester login, denial of administrator
  settings and account persistence across restart.
- Provider fixtures cover public TMDb and Trakt list imports, encrypted Trakt
  settings, per-user ownership and rejection of an in-flight result after a list
  is changed. IMDb CSV and exact TMDb ID resolution have fixture coverage;
  unmatched batches are rejected without partial imports. The public IMDb export
  URL returned HTTP 202 without usable list data from this environment; CSV
  upload is one-time, not a live subscription. Private lists remain unsupported.
- Fake Discord transport tests cover encrypted webhook storage, movie and fully
  monitored TV availability, no notification before fulfillment, uncertain
  delivery and explicit retry. No real webhook or user account was contacted.
  At that earlier point the companion bot was not implemented; only its unconnected signature verifier
  and user-bound selection-token checks have tests.
- Full Go tests and vet, formatting, TypeScript and all 125 interface tests pass;
  the Docker image built with Linux application tests. The request/login changes
  are not yet deployed to an owner installation.

## Reviewed title moves between collections

- Whole-title review captures up to 100 registered files and rejects a changed
  file set. Tests cover a reviewed move with subtitle references and retained
  seeding bytes, stale reviews, API selection and interface confirmation.
- Full Go tests and vet, frontend formatting and type checks, all 123 interface
  tests and the production build passed. A Docker image built with its Linux
  test suite, and a disposable read-only-root container passed health and setup
  state persistence across restart. Its container and temporary volumes were
  removed. At that point whole-folder moves and unrelated extras were not yet
  implemented; the following section records later same-filesystem support.

## Same-filesystem whole-folder moves

- Linux tests review and move a disposable title folder with video, registered
  subtitles, artwork, nested extras and an empty directory. They verify saved
  paths, preserved seeding links, collision and changed-inventory refusal, and
  recovery after an interrupted rename. A changed moved folder keeps media work
  fenced until recovery; a safe failure before rename releases the fence.
- The full Docker build passed its frontend suite, Go tests and vet. Focused
  Linux organization/API tests and organization race tests passed; a disposable
  read-only-root container passed health and restart-persistence checks.
  At that point cross-filesystem whole-folder moves remained unsupported.

## Cross-filesystem whole-folder moves

- A disposable Linux container placed the destination collection on a separate
  tmpfs, forcing a real cross-device move. The test verified video, artwork,
  nested and empty directories, file/directory permissions, seeding links,
  source cleanup, recovery after publication and recovery after partial cleanup.
- Linux organization race tests passed with that cross-device fixture enabled.
  The Docker build ran the full frontend and backend tests and static checks.
  Unknown stages are retained for manual review; a reviewed folder inventory is
  limited to 2,000 entries and 30 seconds.

## YouTube sourcing and subtitle sidecars (0.20 development)

- Clean frontend install, format/type checks and all 122 UI tests pass. Full Go
  tests, vet and native build pass. Linux full race checks and targeted race
  checks for the final YouTube/transfer/API changes pass; the opt-in generated
  video and VTT fixture uses a subprocess and real FFprobe.
- Generated tests cover custom, movie and episode targets, exact review evidence,
  subtitle registration, missing language reporting, reservation conflicts,
  refusal to replace foreign files, changed sources, symlink/overlapping roots,
  verified staging cleanup, recovery after a subtitle conflict and reopen, and
  reorganization of YouTube video and subtitle provenance together.
- Public source metadata and creator-provided English VTT were retrieved for
  XTRA XG #84 (`boKAdg0BS-c`) and XG DAYS #30 (`limOfz2srCk`) without account
  credentials. In a disposable Linux container the XG DAYS video and English
  VTT completed the actual reviewed acquisition and import. A subsequent test
  confirmed acquisition writes to the dedicated staging volume while the
  ordinary `/downloads` mount is read-only. Test containers and media were removed.
- The rebuilt container passes startup/authentication and restart persistence
  checks with a read-only root and download mount. Its pinned yt-dlp package is
  SHA-256 verified. Windows native app startup works, but its yt-dlp child received
  `WinError 10013` connecting to YouTube on this host; public inspection through
  that app returned 502. PowerShell HTTPS and Linux container access succeeded.
  No firewall or operating-system security settings were changed. Windows native
  YouTube network acceptance remains unresolved; the existing preview remains 0.19.
- The 0.20 Linux amd64/arm64 and Windows amd64 packages were built with the
  YouTube guide and their checksums verified. The extracted Windows package passes
  disposable startup, backup/restore and recovery smoke checks; arm64 was
  cross-built but not run. Packages are retained in `dist/private-0.20.0-final3`.
- Hosted workflow 40479132232 for the initial YouTube implementation passed
  all application, generated-media, package and read-only-container checks. Its
  final artifact upload still failed on GitHub storage quota after the approved
  0.8 artifact deletion; GitHub reports usage recalculation takes 6–12 hours.
  The final hosted run 40534519027 at commit `fe2c9dd` also passed all
  application, race, generated video/subtitle, package and container checks;
  its artifact upload failed on the same storage quota.

## Mapped Anime automation (0.19 development)

- Clean frontend installation, format and TypeScript checks, all 119 interface
  tests and the production build pass. An initial test failed on stale explanatory
  copy; its assertion was updated and the suite rerun. The build retains the
  large JavaScript chunk advisory.
- Full Go tests, vet and native build pass after the frontend build, with
  disposable fixture access. The first full run exposed an outdated backup schema
  assertion; it was updated for migration 015 before rerunning the suite. A
  restricted run also failed environment-dependent localhost and hard-link checks.
- Disposable Windows native smoke passes startup/restart, embedded UI, auth,
  backup/restore and recovery checks. The first attempt could not connect to its
  own localhost listener in the restricted environment; the retry with local
  fixture access passed. The owner preview and media were not used.
- Ubuntu WSL was provisioned with Go 1.27.3, GCC, qBittorrent-nox, FFmpeg,
  Docker/Compose and the isolated SABnzbd test runtime. Full `go test -race ./...`
  passes. Both real Anime scheduler tests pass with race detection: the mapped
  pack spans canonical special and two regular seasons, survives a policy/order
  edit and reopen, requires file review, and retains seeding bytes after import.
- Real disposable qBittorrent movie, TV and reviewed Anime pack regressions,
  generated subtitle and existing-library checks, Anime organization, and real
  SABnzbd movie and reviewed-TV imports pass with race detection. All media and
  service profiles were generated for the tests; no owner accounts or media used.
- Linux native and extracted-package startup, backup, restore and recovery smoke
  pass. Linux amd64/arm64 and Windows amd64 packages build and checksums verify;
  arm64 was cross-built only. Docker compose configuration, image build and
  authenticated restart/persistence smoke pass. The point-in-time container
  snapshot was 5.145 MiB, 0.10% CPU and 8 PIDs (512 MiB limit).
- The existing Windows preview was upgraded to 0.19.0-dev after a verified offline
  schema-14 backup at `data/preview/backups/before-mapped-anime-20261026-221813`.
  The 0.18 executable was retained as `bin/reelscarey-before-mapped-anime.exe`.
  Readiness, session state and local doctor checks passed after migration; no
  owner media or private account was used as a test fixture.
- Public AniList identity/title-search acceptance passes without credentials.
  npm audit reports zero vulnerabilities. govulncheck v1.9.0 reports none in
  called code or imported packages, with one advisory elsewhere in a required
  module. Verified private packages are retained in `dist/private-0.19.0`.
- Hosted workflow 40443035881 passed every application, real-client, package and
  container step. Its final artifact upload failed because the account's GitHub
  artifact storage quota is full. With owner approval, the oldest hosted 0.8
  artifact was removed after verifying local copies. The retry passed all
  application steps but the upload again hit quota. A further retry after more
  than seven hours likewise passed all checks and failed only at artifact upload;
  GitHub reports 6–12 hours for usage recalculation. Verified 0.19 packages
  remain available locally.

## Anime provider identity and orders (0.18 development)

- Independent provider/library/API/migration checks pass for GraphQL variables,
  exact Anime type/ID, bounded requests/responses, malformed/duplicate identities,
  redirects, cooldown/cancellation, series group membership and foreign episodes.
  Order preview/apply separates reads from reviewed writes, rejects stale/replayed
  reviews and ambiguous typed labels across provider links, and preserves canonical
  IDs, monitoring, existing absolute aliases, queued jobs, files and ASS references
  through additive schema 14. Films accept identity links without TV mapping controls.
- Transfer tests pass for explicit specials and mixed-season target selections,
  immutable order snapshots, mapping edits during metainfo retrieval, manual-only
  queue authority, duplicate prevention, typed filename hints, cross-season
  single-file refusal and reopened three-file reviewed imports/source retention.
- The fixed public AniList query succeeds through Windows PowerShell HTTP with
  no credentials; the Go adapter's opt-in native live check is blocked by Windows
  socket access permissions. This is recorded separately from contract fixtures.
  The real Go adapter subsequently passed public identity and title-search
  acceptance on Linux; no owner catalog or credentials were transmitted. A
  normal-permission Windows retry after updating the preview returned the same
  OS socket-permission error from the temporary Go test executable. The installed
  `bin/reelscarey.exe`, started with an empty disposable configuration and account,
  then successfully searched live AniList through its normal authenticated API
  and found Cowboy Bebop (AniList ID 1). Thus native installed-app connectivity
  passes; the temporary test-executable restriction remains distinct. The helper
  process/configuration were removed. No network/security control was changed
  or bypassed and no owner catalog/account was used.
- Clean npm installation reports zero vulnerabilities; formatting, types, all
  114 UI tests, production build, full Go tests/vet and native build pass. A final
  display-only fix separates adjacent Activity provider links; all 114 UI tests,
  formatting/types/build and native recovery pass again. The standard >500 KiB
  JavaScript chunk advisory remains. A mistyped focused test filename ran no
  tests and was replaced by the full suite, not counted as a pass.
- Disposable browser checks cover two AniList identity links, exact-ID TMDb
  subgroup proposals, a typed SP1 special, review/cancel/apply, preserved absolute
  aliases, explicit S00/S01/S02 pack targets, separate release confirmation and
  saved order evidence in Activity after process restart. Desktop and 390-pixel
  phone layouts retain reachable controls. The final display fix was visually
  rechecked; no browser console errors were observed. Fixture providers/client
  prove UI behavior only; real provider/client evidence is recorded separately.
  The temporary process/catalog were removed and the viewport reset. Screenshots
  remain outside Git, including `data/preview/anime-order-activity-qa.png`.
- All three initial private cross-build package checksums verify; the extracted
  Windows package passes native recovery. Restricted Go module-stat-cache writes
  emitted access-denied warnings during packaging but all builds completed and
  verified. No dependency or permission policy was changed to suppress them.
- Before updating the owner preview, stopped version 0.17/schema 13 was backed
  up and verified at `before-anime-orders-20261026-012943`. Its executable remains
  at `bin/reelscarey-before-anime-orders.exe`. Version 0.18 starts successfully,
  reports readiness 200/setup_required=false and healthy integrity, encrypted
  credentials, storage/publication and FFprobe checks. The Anime page is open for
  sign-in. No owner identities, mappings, monitoring or media were changed.
- The [initial Linux run](https://github.com/faaaaq/ReelScarey/actions/runs/40278619357)
  at `cdc0973` passed UI/race/static/dependency checks, real AniList identity/search
  (2.16 s), real qBittorrent special/multi-season pack acceptance (3.57 s), existing
  qBittorrent/SABnzbd/automatic-Anime regressions, generated subtitles/discovery/
  organization, native and extracted-Linux recovery and verified cross-builds.
  Container authenticated restart smoke logged PASS (6.742 MiB/512 MiB, seven
  PIDs; not a sustained-load measure). The run was superseded/canceled during
  container cleanup when the final link-layout commit started another run; it
  is not reported as an overall successful workflow.
- The [final Linux run](https://github.com/faaaaq/ReelScarey/actions/runs/40279969256)
  at application commit `2c6e20e` passes every application gate: all 114 UI tests,
  full Go race/static checks, dependency scans, real public AniList identity/search
  (2.23 s), the generated real-qBittorrent special/multi-season pack (2.91 s),
  movie/TV/automatic-Anime/SABnzbd regressions, generated subtitle/discovery/
  organization checks, native and extracted-Linux recovery, verified cross-build
  archives and authenticated container restart. Container snapshot: 8.625 MiB /
  512 MiB, seven PIDs; not a sustained-load measurement. Govulncheck reports zero
  affected/imported vulnerabilities and one advisory in a required module outside
  imported/called code. Nothing is suppressed. Local Docker/CGO/race tools are
  unavailable, so those checks ran on Linux CI.
- The final workflow's overall result is **failure solely at artifact retention**:
  GitHub's account storage quota prevents new artifact uploads. No older remote
  artifacts were deleted and the failure was not hidden. Local verified packages
  remain in `dist/private-0.18.0`; the final documentation package set is in
  `dist/private-0.18.0-final`. All three final checksums verify and its extracted
  Windows executable passes native recovery. No owner service setup is required
  to install these private packages. ARM runtime/NAS acceptance remains separate.
- Parallel implementation and independent final review found no blockers.
  No owner media or private accounts are fixtures. IPTorrents/XSpeeds testing
  remains deferred and the graphite/blue style is retained. Manual specials and
  packs do not add automatic provider-order matching; see REMAINING.md.

## Automatic Anime searching (0.17 development)

- Opt-in policy/API/parser/scheduler/transfer tests pass for explicit titles,
  season and absolute labels, numeric boundaries, packs/ranges/special refusal,
  missing numbers, strict JSON/authentication, no-op/stale reviews and default-off
  migration. Schema-12 data and old downloads survive additive migration 013.
- Independent localhost Torznab review checks chosen-title padded queries,
  same-budget timeouts, source deduplication and immutable job evidence. Seven
  policy/identity/profile/monitoring changes away/back during metainfo retrieval
  cannot queue work. Opting out remains possible after unsuitable title refresh.
- Clean npm install reports zero vulnerabilities; formatting, types, all 102
  interface tests and production build pass. Full Go tests, vet and native build
  pass, as does the disposable native startup/recovery drill. The standard >500
  KiB JavaScript chunk advisory remains. Initial restricted npm installation was
  denied directory writes; normal filesystem access completed the clean install.
- Desktop and 390-pixel phone browser checks verify default-off policy,
  read-only review/cancel, saved alternate query/absolute and season modes,
  numbering coverage, persistence on reopen and reachable save/cancel controls.
  The viewport was reset and isolated catalog/process removed. Screenshots remain
  outside Git. Generated FFmpeg subtitle, existing-library and shared Anime/ASS
  organization acceptance passes locally. All three private package checksums
  verify and the extracted Windows package passes native recovery.
- The stopped schema-12/version-0.16 owner configuration was backed up and
  verified at `before-anime-search-20261025-190535`; the old executable is retained
  as `bin/reelscarey-before-anime-search.exe`. Version 0.17 migrates to schema 13,
  reports readiness 200/setup_required=false and healthy local integrity,
  encrypted-credential, storage, publication and FFprobe checks. Anime is open
  for sign-in. No owner show was opted in and no owner media was modified.
- The [Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40247030198)
  at application commit `71d3d6f` passes every application gate: all 102 UI tests,
  full Go race/static checks, dependency scans, real disposable qBittorrent and
  SABnzbd movie/TV workflows, generated subtitles/existing-library/organization,
  native and extracted-Linux recovery, all cross-build checksums and authenticated
  container restart. The new automatic Anime real-client acceptance completes in
  4.48 seconds with source retention, reviewed mapping and immutable audit evidence.
  Container snapshot: 6.727 MiB/512 MiB and eight PIDs; this is not sustained-load
  evidence. Govulncheck finds zero affected/imported vulnerabilities and one
  advisory in a required module outside imported/called code. Nothing is suppressed.
- The workflow's overall result is **failure solely at artifact retention**:
  GitHub reports the account's artifact storage quota is full. Packages were
  built and verified, but no new remote artifact was retained. Local verified
  packages/checksums are in `dist/private-0.17.0`, with the final documentation
  package set in `dist/private-0.17.0-final`. No older remote artifacts were deleted
  and the failed upload was not hidden. Local Docker/CGO/race tools are unavailable;
  those checks ran on Linux CI. ARM runtime/NAS/owner-service acceptance remains
  separate. Parallel implementation and independent review were integrated.
- No owner media or private accounts are fixtures. IPTorrents/XSpeeds account
  testing stays deferred and graphite/blue remains unchanged.

## Anime and organization (0.16 development)

- Separate Anime film/show classification and coherent browsing preserve
  canonical IDs, duplicate-add behavior, monitoring, file and episode references.
  Explicit unique aliases, no-op edits, foreign/duplicate/out-of-range entries,
  metadata identity/revision changes and automatic episode exclusions are tested.
- Independent migration review verifies released schema-10 data/credentials,
  paths, sidecar journals and profile/monitoring choices survive additive 11/12.
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
  absolute 25–26 reorganization across reopen, ASS content/reference preservation
  and independent seeding-hardlink identity/mtime preservation. No owner file,
  service or private tracker account is used.
- Clean locked npm install/audit (zero vulnerabilities), formatting/type checks,
  all 95 interface tests and production build pass. Full Go tests/vet/build,
  native recovery and final extracted-Windows package recovery pass. All three
  final package checksums verify. The build reports the standard >500 KiB
  JavaScript chunk advisory; it succeeds.
- Disposable browser checks verify Anime-only film/show separation, correctly
  grouped bulk actions, reviewed absolute numbering, saved custom Anime templates
  and a Family collection with distinct server presets. Cancel leaves original
  paths untouched; apply completes and refreshes exact video/ASS paths. SHA-256
  matches the retained seeding-source fixture. The 390-pixel phone review fits,
  with reachable controls; the viewport override was reset. Collection-aware
  discovery paths and desktop table views are checked. Test catalog/process were
  removed; screenshots stay outside Git. No owner media/preferences were used.
- The stopped owner's schema-10 configuration was backed up and verified at
  `before-anime-organization-20261025-015316`; the 0.15 executable is retained.
  Version 0.16 migrates to schema 12 and reports readiness 200, setup_required=false
  and healthy database/key/storage/publication/FFprobe checks. Organize library is
  open after sign-in. No owner files were renamed, moved or reclassified as tests.
  Graphite/blue is retained. Private IPTorrents/XSpeeds account testing remains
  deferred. Main-root migration, whole-folder extras, AniDB/alternate-order
  automation and queued batch rollback/cancellation are explicitly outside scope.

The [Linux workflow](https://github.com/faaaaq/ReelScarey/actions/runs/40170620500)
passed for application commit `7ec0230`: all 95 interface tests, Go race/static
checks, real disposable qBittorrent/SABnzbd movie/TV downloads and verified
imports, generated subtitles/existing-library tests, the generated shared Anime
organization/ASS/seeding-source acceptance, native/extracted-package recovery,
checksummed cross-builds and authenticated container restart.
Govulncheck reports zero vulnerabilities affecting application code and one
advisory in a required module outside imported/called code; npm audit reports
zero. No advisory is suppressed. The constrained container snapshot is
8.598 MiB/512 MiB and eight PIDs, not a sustained workload measurement.
Private artifact `12590261844` retains the 0.16 packages/checksums for 14 days.
ARM runtime/NAS deployment, sustained load and owner-service/private tracker
acceptance remain unverified. Local Docker/CGO/race tools are unavailable; those
checks ran on Linux CI. The earlier push's run was superseded by the final browser
label fixes. Parallel codebase exploration, implementation and independent test
review were integrated before this final acceptance.

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

## 2026-10-28: reviewed replacements and bulk monitoring

- Linux disposable-file tests cover reviewed replacement and rollback, preserved
  independent seeding/source copies, shared episode mappings, external subtitles,
  symlink rejection, stale source selection, occupied destinations, interruption
  recovery, and replacement activity events. A generated-video test used real
  FFmpeg and the production FFprobe adapter, including restore after reopening.
- Atomic batch season and episode monitoring tests cover explicit selections,
  missing or duplicate IDs, revision conflicts, and preservation of episode
  choices. Frontend tests exercise selection, confirmation and review.
- The Docker build passed frontend formatting, type checking, 140 frontend tests,
  all backend tests and `go vet`. Linux race checks passed for the modified
  library, organization, transfer and HTTP services. A disposable container
  passed startup, authentication, settings persistence and restart checks.
- Live owner media and accounts were not used. Downloaded upgrade imports,
  YouTube-sourced replacements, broader removal and other open items remain in
  [REMAINING.md](REMAINING.md); Emby live acceptance remains deferred.

## 2026-10-28: embedded audio tags

- Real FFmpeg generated a disposable video with Japanese audio metadata;
  FFprobe returned its audio codec and language tag. Fixture tests verify
  reinspection detects content changes, preserves prior facts on failure,
  updates matching YouTube provenance, and does not prevent a reviewed rollback.
- The interface test checks explicit reinspection and the displayed embedded tag.
  Tags are metadata and do not establish the language actually spoken.

## 2026-10-28: selected-file health checks

- The interface checks selected registered files one at a time, reports each
  result and stops without starting the remaining checks when cancelled. The
  selection and cancellation paths have frontend tests; the underlying checksum
  and missing/changed-file checks use disposable filesystem fixtures.

## 2026-10-28: cancellation of unstarted organization jobs

- Tests cover returning a queued job to review without changing media and
  refusing cancellation when output is journaled or a folder may have moved.
  The interface offers cancellation only while the job is queued and planned.

## 2026-10-28: embedded-title inspection

- Real FFmpeg generated disposable MP4 and Matroska videos with an intentionally
  incorrect embedded title and Japanese audio tag; FFprobe returned those tags.
  The details interface displays the title and flags differences from a saved
  movie title. Existing files can be explicitly re-inspected with checksum checks.
- This is inspection only. Safe embedded metadata rewriting during organization
  remains open; no existing media tags were edited by this work.

## 2026-10-28: verified embedded-title updates

- Reviewed MP4/Matroska title updates use independent staging and retain the
  previous video for rollback. A reviewed single-file organization can queue its
  title update after the move; unfinished work is recoverable on restart.
- Generated-video tests verified actual MP4 and Matroska title rewrites, packet
  hashes and timing, multiple streams, subtitles, chapters and an attachment.
  The MP4 workflow preserved an independent seeding source and restored the
  original video. YouTube-origin replacement and rollback retained source
  provenance and subtitle files in isolated fixtures.
- A disposable read-only Docker container passed an authenticated single-file
  move, verified title update, source retention and rollback. The local Compose
  app was rebuilt and started with separate empty project media folders.
- Whole-folder and multi-file automatic title updates, direct downloaded upgrades
  and replacement from newly acquired YouTube videos remain open. No owner media
  or live Emby server was used for these tests.
- A generated Matroska TV episode also received its canonical series, season,
  episode number and episode title after a reviewed move. Subtitle provenance
  was checked across both individual-file and whole-folder moves before rollback.

## 2026-10-29: version 0.24 named download clients and NZBGet

- Implemented up to 32 named qBittorrent/SABnzbd/NZBGet connections, tested paused
  creation, explicit enabled per-protocol defaults, encrypted credential retention,
  revision checks and immutable per-job remote/local folder bindings. Existing
  Settings clients and jobs retain their prior behavior. Schema 038 preserves
  released migration checksums, encrypted legacy rows and connection-history
  triggers. The upgrade test checks these facts without inventing connections.
- Frontend formatting, TypeScript, all **167 tests in 34 files** and production
  build passed. The complete Linux backend race suite, static checks and build
  passed. New API tests cover administrator/requester boundaries, CSRF, paused
  defaults, stale edits, secret omission/retention and reviewed deletion. Domain
  tests cover client/default/credential changes during queue preparation,
  source-folder containment and lost NZBGet append replies without resubmission.
- Real disposable qBittorrent, SABnzbd 5.1.3 and NZBGet 26.3 passed movie and TV
  downloads/imports under the race detector using FFmpeg-generated videos and
  owned local torrent/web-seed or yEnc/NNTP fixtures. Named clients retain their
  binding across changed defaults and application reopen. TV review, FFprobe
  validation and unchanged source bytes are verified. Existing qBittorrent Anime
  special/multi-season pack acceptance also passed. Test runtimes stay outside
  the app image; NZBGet's CI download is pinned by SHA-256.
- The final Docker build passed its frontend/backend gates. Disposable container
  backup download/upload/restore preserved a named connection, its folder and
  default, then successfully tested its restored encrypted credentials. Session
  revocation, retained safety backup, paused schedules and recovery review passed.
  Existing seven-feature and storage-copy/activation/restart rehearsals passed.
- A one-minute constrained container run completed **4,552 reads and 12 settings
  writes with zero failures**, p95 17 ms and p99 26 ms. Peak sampled memory was
  39.19 MiB under a 1 CPU / 384 MiB / 128 process limit. Post-run catalog,
  integrity and restart checks passed. This bounded local run is not NAS, ARM
  or production-scale acceptance.
- Before replacement, retained image `reelscarey:before-download-clients-20261029`
  and two verified schema-37 configuration snapshots under the private ignored
  `data/backups/before-download-clients-20261029` folder. `configuration-final`
  was taken offline immediately before replacement; `deployment.env` retains
  the previous private deployment settings. Both encryption key and setup state
  remain unchanged. Existing media and download sources were not test fixtures.
- Upgraded the existing Compose container/volumes to **0.24.0-clients**, schema38.
  Compared all original columns in all **80 prior tables**, including the complete
  prior migration ledger; all were unchanged. Integrity and foreign keys passed.
  All **nine installation checks** passed before and after controlled restart.
  Docker reports healthy, zero crash restarts, UID10001 and a read-only root.
  Windows loopback routes and all **40 release assets** matched the built files;
  first-run setup remained unchanged and administrative APIs remained protected.
  The hidden WSL host launcher was verified directly on the host and remains
  running; restricted process visibility is not evidence that it exited.
- Owner-service/NAS/proxy acceptance, extra provider protocols and the license
  remain separate work requiring appropriate access or decisions. No owner
  credentials, account or media was used for automated acceptance.

## 2026-10-29: version 0.25 original subtitle formats and explicit TV labels

- Added an opt-in original-format preference for provider SRT, VTT, ASS and SSA.
  Existing installations keep SRT. Candidates display their output format;
  unsupported originals, format mismatches and unsafe timing are rejected. ASS/
  SSA event/style text and VTT styles/comments survive import, reviewed timing
  edits and retained-version rollback. Original SRT also bypasses conversion;
  SRT output explicitly requests conversion when its source is another format.
  Provider quota resets accept numeric epochs, numeric strings and the documented
  human-readable reset paired with a UTC timestamp.
- Subtitle journals retain their original extension/content across preference
  changes and reopen. Tests cover planned/staged/published recovery without
  consuming quota again, preference changes during fetch, malformed event fields,
  negative/invalid/overlong cues, preserved video bytes and byte-exact rollback.
  Backups retain the format preference while restore pauses work for review.
- Replaced the duplicate filename-token parser with bounded shared episode-span
  parsing. Explicit S01EP02, S01 Episode 02 and Season 1 Ep 2 labels work with
  exact title/year/canonical targets. Reviewed filename hints accept consecutive
  spaced/long-form ranges and chains of at most twenty episodes in one season.
  Multiple/conflicting labels, nonconsecutive/mixed ranges, cross-season labels
  and hidden extra episode evidence are rejected; x264/x265 remain codec labels.
  Ordinary automatic searches still select only a single canonical episode.
- Frontend formatting/type checks, **168 tests in 34 files**, and production
  build passed. Complete Linux backend race/static/build checks passed, followed
  by final affected-package race checks after the final parser/provider changes.
  The final Docker build passed all embedded frontend/backend gates.
- Real FFmpeg videos and FFprobe verified original SRT/ASS/SSA/VTT imports,
  subtitle readability, style preservation, source hashes and reopen. The real
  OpenSubtitles adapter's HTTP serialization/parsing uses owned API/CDN protocol
  fixtures; no owner account or quota is used. Real isolated qBittorrent,
  SABnzbd 5.1.3 and NZBGet 26.3 movie/TV regressions passed under the race detector,
  including expanded filenames, mandatory mapping review, application reopen,
  saved named-client mappings and retained source bytes. The web seed serves only
  its declared generated filenames. Existing Anime pack acceptance also passed.
- Disposable Docker backup download/upload/restore preserved the original-format
  preference and named encrypted clients. Existing seven-feature and storage
  copy/activation/restart rehearsals passed. A one-minute constrained container
  run completed **4,531 reads and 12 settings writes with zero failures**, p95
  **18 ms**, p99 **27 ms**, peak sampled **41.89 MiB** and eight processes under a
  1 CPU / 384 MiB / 128 process limit; restart/catalog/integrity checks passed.
  This bounded run does not establish NAS, ARM or long-term production capacity.
- Before replacement, retained image `reelscarey:before-subtitle-formats-20261029`
  and verified schema-38 snapshots `configuration` and `configuration-final` in
  private ignored `data/backups/before-subtitle-formats-20261029`, with the prior
  deployment environment saved alongside them. The final snapshot was offline
  and independently checked with the new application before image replacement.
- Upgraded existing Compose volumes to **0.25.0-formats**, schema39. Compared all
  prior columns in all **82 existing tables**, including the complete prior
  migration ledger; all matched the final backup. Encryption key/setup state were
  unchanged and the new format default is SRT. Integrity and foreign keys passed.
  All **nine installation checks** passed before and after controlled restart.
  Windows loopback/deep links and all **40 release assets** matched the build;
  administrative routes remain protected and setup state remains unchanged.
  The WSL host launcher remains running. Deployment uses the existing single
  non-root container and read-only root, without adding runtime dependencies.
- Owner-service acceptance, additional providers, language recognition, OCR/
  alignment, broader release grammars, NAS/ARM validation and license selection
  remain separate work. No owner media, private tracker or provider account was
  used as a fixture.

## 2026-10-31: version 0.26 providers and reference subtitle alignment

- Implemented Transmission 4 as a named torrent client, Kitsu Anime identity
  search/link/refresh, SubDL manually reviewed subtitle downloads, and local
  reference-text subtitle timing alignment. Existing canonical IDs, explicit
  consent, no-overwrite imports and retained subtitle rollback remain in force.
  Migration 040 preserves named clients/defaults; 041 preserves subtitle provider
  provenance in catalog tracks and pending journals. Existing data defaults to
  OpenSubtitles without re-entering credentials.
- Frontend format/type checks, **171 tests in 34 files**, and production build
  passed. The first complete Linux race run passed every package; a concurrently
  added regression test had a fixture pointer error caught by the subsequent
  vet step. Corrected it, then the final affected subtitle/API race checks passed.
  Regression coverage includes colliding provider IDs, Kitsu search/review/refresh
  scoping, exact SubDL episode identity, cooldown and pending import provenance.
- Real isolated authenticated Transmission movie and reviewed TV imports passed
  using generated web-seeded media. A fresh build of the committed Transmission
  test Dockerfile reproduced both flows under the race detector: stable job
  binding across default changes/reopen, FFprobe validation, retained seeding
  bytes and mandatory TV mapping review. The adapter's session challenge,
  foreign-torrent rejection and incomplete verification states also passed.
- Public Kitsu identity and title search passed without credentials or owner
  media. SubDL adapter checks use owned HTTP responses, not an owner's paid key
  or quota. Generated FFmpeg/FFprobe SRT, VTT, ASS and SSA imports passed with
  manual review and provider provenance retained across reopen. Reference
  alignment tests cover offset/drift, weak or incompatible references, duplicate
  cues, preview without writes, journaled application and exact rollback.
- Dependency scanning found the newly reported HTML parser performance issue
  [GO-2026-6860](https://pkg.go.dev/vuln/GO-2026-6860), reachable through tracker
  HTML parsing. Updated golang.org/x/net from 0.60.0 to fixed 0.62.0. The repeated
  scan found zero affected symbols or imported packages. Its module-only notice
  concerns unused x/crypto/openpgp; the application does not import that package.
  npm audit found zero vulnerabilities. Actionlint passed both workflows.
- Container publication now embeds the version, adds commit-addressed tags,
  SBOM and provenance, and conditionally signs the published digest through the
  pinned official GitHub action. This is workflow preparation, not evidence of
  signed publication. Private-repository eligibility, hosted checks, actual
  publishing and owner license selection remain separate release requirements.
- Final dependency-patched Docker build passed all embedded UI/backend checks.
  The broader Debian trixie/FFmpeg 7 race run exposed an obsolete assumption that
  VP9/Opus WebM remuxing always changes timing. Updated the regression to compare
  independently probed packet timestamps/content and require rejection whenever
  they differ. The complete transfer race suite and vet then passed (350.482 s),
  including real Transmission; every other package passed the complete patched
  race run. The corrected assertion also passed with production's FFmpeg 5.
- Existing real qBittorrent movie/TV/named-client/Anime-pack and NZBGet movie/TV
  regressions passed. SABnzbd's initial connection sometimes consumed its full
  60-second interval, exhausting the old one-minute total fixture deadline.
  Allowed a bounded two minutes for startup plus post-processing/import review;
  all four isolated standard/named movie/TV cases then passed (29.138 s total).
  No production timeout or validation safeguard was relaxed.
- Disposable Docker backup/download/upload/safe-restore, seven-feature workflows
  and storage copy/mount-activation/restart checks passed. A one-minute constrained
  run completed **3,816 reads and 12 settings writes with zero failures**, p95
  **81 ms**, p99 **117 ms**, peak sampled **48.71 MiB** and eight processes under
  **1 CPU / 384 MiB / 128 processes**. Catalog/integrity survived restart. This is
  a bounded fixture workload, not long-term NAS or ARM capacity acceptance.
- Before replacing the installed image, retained
  `reelscarey:before-remaining-integrations-20261031` and a private offline,
  verified schema-39 snapshot at
  `data/backups/before-remaining-integrations-20261031/configuration-final`.
  Prior deployment settings are stored alongside it. The candidate independently
  verified the snapshot before image replacement. Backup data remains ignored by
  Git and excluded from the Docker build context.
- Upgraded the existing Compose app to **0.26.0-integrations**, schema **41**,
  image `sha256:912dbc6283d85b37e3ed91a8a8a03c6bbd201515c9f57ffd6bb4d183dbff757a`.
  Compared every prior column/row in all **82 tables** and the prior migration
  ledger with the offline backup: unchanged. Encryption key/setup state remained
  unchanged; integrity and foreign-key checks passed. All **nine installation
  checks** passed before and after controlled restart. Docker reports healthy,
  zero crash restarts, UID10001 and a read-only root. Windows loopback routes,
  administrative authentication boundaries and all **40 served assets** passed.
  The existing hidden WSL host launcher is active. The test-only packet assertion
  change reproduced the exact acceptance-tested application binary.
- GitHub Actions on PR #6 could not start: its annotations report failed account
  payments or the spending limit. Entire Gates additionally reports missing
  approvals and CI success. These are not represented as passing hosted checks.
  Local results above were executed independently; no billing, branch protection,
  check status or approval setting was changed.

## Existing milestone maintenance — 2026-10-31; rechecked 2026-11-01

- Removed the unshipped OCR, translation and audio-alignment experiment, its
  dependency manifests, model build/test files, downloaded models and test images
  at the owner's request. No database migration or production feature removal was
  needed because the experiment was never deployed. Existing manual timing and
  reference-text subtitle alignment are preserved.
- Audited the active milestone queue and reconciled historical entries for
  already-completed clients, subtitle formats, bulk monitoring, file checks,
  removal and replacements. Optional extensions are not active implementation
  work. Owner-service, independent-review, hardware, supported IMDb-feed and
  public-release requirements remain explicitly open.
- Fixed browser titles on existing management pages, keyboard focus after route
  navigation, and return to the mobile menu button when Escape closes navigation.
  Removed the stale hard-coded sidebar version. This is targeted accessibility
  coverage, not a complete accessibility or independent security review.
- All 175 UI tests passed, including four navigation checks; formatting,
  TypeScript and frontend build passed. The production Docker build passed
  `go test ./...`, `go vet ./...` and the application build. Linux full race tests
  and static checks also passed using the current application sources.
- Disposable Docker backup/upload/restore/restart, existing seven-workflow
  acceptance and storage migration/activation/recovery checks all passed.
- Created and verified the offline schema-41 configuration/database/key backup
  at `data/backups/before-existing-maintenance-20261031/configuration`; the private
  deployment environment is alongside it. Preserved the old image as
  `reelscarey:before-existing-maintenance-20261031`.
- Updated the installed single container to `0.26.1-maintenance`, image
  `sha256:ed4e499a476b0f22357a99d1cdcad91515169442bf306edb7ea71729c6df06af`.
  All 82 pre-upgrade database tables and migration records matched; integrity,
  foreign keys, encryption key and setup token were preserved. Startup and an
  explicit restart passed doctor and health checks. The container remains
  UID 10001, read-only, with zero recorded unexpected restarts.
- Host HTTP readiness and protected routes passed. All 40 served frontend assets
  matched the newly built files. Cancelled processing dependencies were confirmed
  absent from the final runtime image. On resumption, all installation checks and
  served-asset comparisons passed again; the existing hidden WSL host launcher
  was restarted and the container reached healthy status.

## Ubuntu Docker Hub release preparation — 2026-11-01

Version 0.26.1 adds a published-image Compose installation and server setup guide
for Ubuntu x86-64. There are no new application features or schema changes.
The Docker Hub workflow now runs candidate Compose, backup/restore, existing
workflow and storage recovery acceptance before push, publishes a version tag,
and pulls the registry digest to verify provenance labels and backup/restore.
The actual workflow result is required before claiming publication succeeded.

Hosted storage acceptance exposed a harness portability defect: the non-root
runner could not traverse Docker's private volume directories. The harness now
uses Docker-managed volume subdirectory mounts. Running the corrected full test
as the `nobody` host user with Docker group access passed activation, restart,
retained-original hashes and database checks. The Ubuntu Compose layout passed
startup, HTTP readiness, administrator authentication boundary, read-only download
mount, UID 10001, writable persistent storage, key preservation and restart checks
using disposable folders/volumes with the already-verified maintenance image.
Compose config, shell syntax, frontend formatting and actionlint 1.7.12 workflow
validation passed. Release-image build and registry verification run in the
publishing workflow using the existing Docker Hub login; repository visibility
and signing eligibility have not been changed.

## 2026-11-02: simple Docker Compose installation (0.26.2)

Added a single-file published-image template for an existing npm network, PUID,
PGID and TZ. The app runs as the selected non-root account after private-folder
initialization. No schema or application feature is added. Existing explicit-user,
read-only deployments retain their mappings; the simple layout keeps separate
state/backup/staging folders under one config mount and persistent library recovery
storage beneath separately mounted Anime/TV folders.

Executed locally:
- Docker build: frontend formatting/types/tests/build, all Go tests, vet and build passed.
- Private-directory initialization rejected linked/overlapping roots and preserved
  outside files reached through symbolic links and hard links.
- The simple Compose template passed UID/GID 1000 startup, TZ, separate mount
  access, unchanged media ownership, backup/upload/restore, safety snapshots,
  container replacement, persistent key/archives/library state, HTTPS proxy login,
  Secure cookies and foreign Host/Origin rejection.
- Existing server Compose, backup, seven-feature and storage migration/recovery
  acceptance passed, including original generated file hashes after restart.
- Publishing/CI workflows passed actionlint; Compose and shell syntax passed.
- A fresh offline backup verified three configuration files at schema 41. The local
  app upgraded to 0.26.2, passed doctor before/after restart, and retained exact
  rows in all 82 database tables plus the original encryption key. Prior image,
  deployment settings and verified configuration backup remain available locally.

The initial local build exposed legacy-builder incompatibility with COPY --chmod;
a standard chmod layer fixed it. The first proxy test used fetch, which replaced
its explicit Host header; native HTTP now exercises the actual configured and
rejected proxy Hosts. Final acceptance passed. Docker Hub publication must still
be confirmed from the new publishing workflow; these local results do not imply
that a remote build or registry pull has completed.
## 2026-11-02: automatic existing-library matching (0.27.0)

Executed in Docker using generated/disposable media, without owner provider
credentials or owner media:

- Frontend formatting, type checks, all 177 interaction tests and production build.
  Initial concurrent UI runs exposed stale scan-request expectations and three
  startup timeouts; updated expectations and a bounded two-worker run passed.
  The release Dockerfile independently passed the full frontend suite.
- Full backend race suite, static checks and application build; focused final
  automatic-matching race checks passed. A host-mounted build initially failed
  Git ownership stamping; the explicit buildvcs=false development build and the
  production Docker build succeeded.
- Automatic title/year/provider identity parsing; duplicate-version exclusion;
  cached title and season loading; manual-review preservation; explicit old-scan
  start; cancellation during provider fetch; source/catalog changes; ambiguous
  provider identities; bounded retries and paused failures; failed discovery;
  foreign leases; persisted restart; admin/CSRF/requester boundaries; restored
  jobs paused with cleared leases.
- New automatic-library container acceptance exercised authenticated scan/start,
  movie and TV registration, a two-episode shared video, duplicate and uncertain
  exceptions, actual FFprobe and restart. All source hashes, inodes and mtimes
  stayed unchanged; three canonical episodes became available with no fabricated
  download records. This check runs before Docker Hub publication.
- Local configuration backup was created and verified before upgrading schema
  41 to 42. Version 0.27.0 passed readiness and doctor before/after restart. Every
  row in all 82 prior tables, the migration ledger through 41, encryption key and
  setup token were preserved. No matching job was implicitly created.

Authenticated acceptance on the owner's Ubuntu server remains unperformed:
the public readiness endpoint answered, but the desktop browser controller could
not establish its session. No owner-server library records were changed.

## 2026-11-03: fast existing-library registration (0.27.5)

Executed with disposable/generated media only:

- Frontend formatting, type checks, all 181 interaction tests and production build.
- Docker build passed all Go tests, vet, compilation and its independent frontend
  checks. Automatic scan/registration/restart, backup/restore, simple Compose
  PUID/PGID/proxy startup and offline storage migration acceptance passed.
- A generated 64 GiB sparse fixture registered in 18.3 ms with a fixture prober;
  actual FFprobe inspected a 4 GiB sparse MP4 with trailing metadata in 54.1 ms.
  These establish bounded reads and seeking, not real-server throughput promises.
- Folder authority, one provider detail fetch for an explicit ID, title/season
  caching, duplicate exclusions, accurate matched/unmatched filters and continued
  queueing while disk inspection is deliberately blocked passed targeted tests.
- Deferred checksums preserve replacement reservations, are promoted before
  organization reviews, and reject changed/replaced sources. Subtitle inspection
  and cached reuse validate observations before sidecar publication. Schema 42 to
  43 preserves full hashes, technical facts and pending matching evidence.
- The change-timestamp test initially exposed same-clock-tick timestamp coalescing
  on an overlay filesystem. After allowing a timestamp tick in that test, ten
  repeated fingerprint-suite runs passed. Bounded observations are explicitly not
  represented as full-content integrity checks.
- Final focused checks also cover TV shows inside grouping folders and preserved
  changed-file errors. The publishing workflow rechecks the merged source before
  uploading and pulls the resulting registry digest for verification.
- Primary-branch release run 41618794982 published version 0.27.5 and verified
  the pulled Docker Hub digest, startup and backup/restore. Its generated-media
  automatic matching, Compose and storage recovery checks passed before push.
- Hosted CI run 41618794927 passed all 181 frontend tests, the full backend race
  suite, vet and real-client/generated-media acceptance, then stopped on a stale
  smoke-test expectation of 24 library items per page. The expected default is
  corrected to the requested 48. The complete corrected smoke script passed in
  the local release container, including restart, offline backup, restore and
  recovery. This correction changes test expectations only, not app behavior.

No owner library, credentials, or server database was used or modified. Real-server
scan timing and metadata-provider latency remain owner-server acceptance work.

## Version 0.28 movie versions

Executed against disposable/generated media:

- All 185 frontend interaction tests, formatting, type checks and production
  bundle; Docker build runs the same checks plus all Go tests and vet.
- Backend race checks passed across all packages after fixing the existing-movie
  YouTube reservation regression. Changed transfer, backup, HTTP and database
  packages were rerun after their final changes. A standalone mounted-source
  build initially could not read host Git metadata; vet and a build with host
  VCS stamping disabled passed. The Docker release build is independent of that
  metadata.
- Real authenticated qBittorrent acquired generated 1080p and 2160p movies under
  independent reservations, reopened the application while both were queued,
  imported both through real FFprobe and retained both complete seeding sources.
- Generated container scans attached both HD/4K files to one movie, retained
  paths, bytes, inodes and modification times, and survived restart with the
  saved version policy. Overlapping TV targets still require review.
- Authenticated backup download/upload/restore preserved both registered movie
  versions and their policy, revoked old sessions and retained recovery barriers.
  Simple and hardened Compose startup, PUID/PGID, HTTPS proxy handling, storage
  copy/activation/restart and existing feature-container acceptance passed.
- Schema 43 to 44 preserved registered IDs, pending replacement intent, subtitle
  tracks/jobs and staged subtitle edit bytes/row IDs. Separate-copy organization
  preserved sidecars; replacing and rolling back HD left 4K untouched. Policy,
  profile and monitoring changes during release fetch prevented stale queueing.
- Prior hosted CI 41736345383 exposed a backup-list publication race: a reader
  could see a record before JSON encoding finished. Records now publish from a
  complete synced temporary file without replacement. A deterministic concurrent
  publication test verifies that readers never see an unfinished record.

The version controls and both file entries were inspected in a disposable browser
session. The resulting dropdown styling uses the existing automation form layout.
No owner-server media or download client was used for these checks.

## Version 0.30 inspected quality and copy-free title updates

- FFprobe records `color_transfer`; generated media tests cover the SDR, HDR10
  and HLG mapping. Migration 046 applied to a populated older-schema database
  and leaves pre-HDR probes unknown rather than claiming SDR.
- Organization and import naming tests assert inspected resolution plus the
  HDR/SDR tag, multi-version suffix composition and the absence of a quality
  claim for unverified files.
- Embedded-title jobs write and verify a metadata-only stage, replace the
  original in place, record the previous title and both checksums, keep torrent
  seeding hardlinks, and resume after an interruption between the rename and the
  saved phase advance. Retag verification now compares color transfer,
  primaries, space and range; a generated HDR10 Matroska fixture retained HDR10
  signaling and the new title.
- Undo-move accepts a current file only when a completed metadata-only plan
  records its exact bytes; arbitrary content changes still require a reviewed
  restore.
- Real Linux FFmpeg/FFprobe acceptance generated and rewrote MP4, M4V,
  Matroska, MOV, AVI and WebM files. Each verified stage replaced the original
  with no retained duplicate, preserved a hardlinked seeding source, recorded the
  previous title, and resumed after an interruption between the rename and the
  saved phase advance.
- Frontend formatting, type checks, 186 interaction tests and the production
  bundle passed. Go tests and vet passed across all packages.

## Version 0.31 built-in qBittorrent client

- Supervisor lifecycle tests with a stub daemon cover enable (row created,
  daemon started, configuration generated, tested), peer-port restart, missing
  binary failure surfacing, disable-while-default refusal and generic
  edit/delete refusal. Database tests migrate schema 47 and verify the builtin
  check constraint.
- Real Linux acceptance inside the production image lineage: qbittorrent-nox
  installed from the same Debian bookworm packages, the generated
  configuration authenticated the real RPC, the daemon ran, and disable
  stopped it gracefully with the process verified gone from /proc.
- Frontend formatting, type checks, 188 interaction tests and the production
  bundle passed. Go tests and vet passed across all packages.

## Version 0.32 manual torrent uploads

- Parser tests cover single-file and multi-file metainfo, exact-span infohash,
  the private flag, duplicate keys, truncated input, trailing garbage, missing
  or malformed names, missing v1 pieces and the oversized upload bound.
- Queue tests cover movie binding with the recorded infohash, duplicate-hash
  refusal, unknown titles, replacement rejection without a registered target,
  season-pack targets matching the season search, episode-filtered targets,
  the episode reservation guard, unloaded seasons and empty uploads.
- Frontend formatting, type checks, 190 interaction tests and the production
  bundle passed. Go tests and vet passed across all packages.

## Version 0.33 Mullvad VPN kill switch

- Stubbed tunnel lifecycle tests cover enable with generated route-suppressed
  configuration, honest failure states, disable, redaction of the private key
  in tool output, and refusal of incomplete configurations.
- Kill-switch tests on the bundled-client supervisor cover the stopped state
  with its explanation while the tunnel is down, the restart with the tunnel
  interface binding when it returns, and the stop when it drops again.
- Frontend formatting, type checks, 190 interaction tests and the production
  bundle passed. Go tests and vet passed across all packages.

- A production deployment with PUID 1000 exposed the setpriv init-groups
  limitation (arbitrary numeric IDs have no passwd entry). The entrypoint now
  uses --clear-groups, matching the gosu path for unknown IDs, and the fixed
  entrypoint was verified on the affected deployment before releasing.
- The race detector caught the supervisor reading the bounded stderr capture
  while the child process was still running and the exec copier still writing.
  The capture is now a locked buffer shared by writer and readers; the fix was
  validated by the Linux race suite.
- Two CI-only failures were diagnosed and fixed: the gated enable returned an
  unexplained stopped state because the supervisor tick had not run yet (the
  enable path now states the kill-switch reason immediately), and the live
  supervision test counted unrelated qbittorrent-nox processes from earlier
  runner steps (it now compares against a pre-test baseline).
- The VPN dependency was missing from the HTTP server's constructor call, so
  the card reported the service as unavailable. The dependency is wired and the
  endpoint verified on the affected deployment before this release.

## Version 0.33.3 VPN connection with native wg/ip

- A real Mullvad configuration was used inside the production image: the
  interface came up, a socket bound to `reelscarey0` reached the internet
  through the tunnel, an unbound socket used the normal network, and teardown
  removed the interface and rules. The credential file was removed afterwards.
- Root cause of the failed connection: `wg-quick` re-execs through `sudo`,
  which the unprivileged application cannot run. The tunnel now uses `wg` and
  `ip` directly with the retained NET_ADMIN capability.
- `Address` accepts the full Mullvad list (`IPv4,IPv6`); IPv6 is optional when
  the container lacks IPv6 support. The card accepts an uploaded `.conf`,
  parses it in the browser and connects.
- The card refreshes its revision after a failed attempt, removing the spurious
  conflict on retry.
- Generic form controls now have an explicit field background, border and
  spacing; labels read as labels in both themes.
- Frontend formatting, type checks, 191 interaction tests and the production
  bundle passed. Go tests and vet passed across all packages.

## Version 0.33.4 kill-switch toggle

- VPN policy tests cover every combination: no VPN, enabled with the kill
  switch on and the tunnel down/up, and the kill switch off with the tunnel
  down. The status reports the saved preference.
- Supervisor tests cover the client stopping with the honest reason, restarting
  bound when the tunnel returns, restarting unbound with the kill switch off,
  and stopping again when it is turned back on.
- The VPN card toggles the switch through a revision-checked update that does
  not force a reconnect; the frontend tests assert the exact request.
- Frontend formatting, type checks, 192 interaction tests and the production
  bundle passed. Go tests and vet passed across all packages.

## Version 0.34 IRC announce feeds

- Parser tests use the published Autobrr sample announce lines for all six
  supported trackers, plus rejection tests for unrelated channel chatter.
  Size parsing covers binary and decimal units.
- A fake IRC server test proves connect, PING/PONG, channel filtering, announce
  delivery and context cancellation; the source lifecycle test covers encrypted
  settings, blank-password retention, dedupe and bounded history.
- The full Docker image was built on the acceptance server: frontend format,
  type checks, 193 interaction tests and production bundle passed; all Go tests
  and vet passed. A disposable acceptance container then verified schema 49,
  setup, six definitions, source creation with an encrypted password, empty
  event history and the VPN endpoint.
- The acceptance container and its temporary directories were removed. The live
  stack was inspected read-only and left untouched on 0.33.3.

## Version 0.34.1 IPTorrents setup guidance

- The indexer dialog now explains, per source type, exactly how to obtain the
  cookie and user agent, including DevTools → Network, HttpOnly cookies, exact
  host matching, IP-bound sessions and browser challenges. Cookie and
  user-agent labels and line breaks are stripped before saving.
- The direct tracker check now accepts a signed-in page when its torrent table
  parses, even without a logout link; a reached page with an unknown layout
  reports the IPTorrents display settings instead of a false auth failure.
- The full Docker image was built on the acceptance server; frontend format,
  type checks, interaction tests and Go tests passed.
- The Settings page was reorganized into a Sonarr/Radarr-style rail; an App
  interaction test verifies the active General section, switching to System,
  hiding the General save bar, and the Indexers links. Prettier and TypeScript
  checks pass; the full image build validates the remaining suite.

## Version 0.34.2 Collections in Settings

- The collection editor is a shared component used by Organize library and
  Settings → Media Management. The App test selects Media Management, asserts
  the built-in Anime collection is present and the Add collection action is
  available.
- Frontend Prettier, TypeScript and the App interaction suite passed; the full
  image build validates the complete suite.
## Version 0.34.3 IRC readiness

- New announce sources default to paused. The form stores encrypted credentials
  without connecting; enabling remains an explicit action.
- The announce list refreshes every 10 seconds while the page is open.
- The UI explains that a dedicated bot nick is required and that two clients on
  the same nick disconnect each other. Live tracker acceptance is deferred at
  the owner's request; parser, fake-IRC and image-build checks remain in place.
## Version 0.34.4 tracker download key

- Announce sources store an encrypted tracker download key: IPTorrents passkey,
  TorrentLeech/BeyondHD RSS key, FileList/AnimeBytes passkey or MyAnonamouse
  session cookie. The API exposes only `has_key` and `key_label`; the key is
  never returned.
- The source test covers saving and retaining the key across a blank-key edit.
  The Announce page adds a password field and shows "key saved" without
  displaying it. The key is not used until automatic grabbing is enabled.

## Version 0.34.5 seekable title verification

- End-to-end manual acceptance on the live server: an IPTorrents release was
  downloaded by the built-in qBittorrent client and imported as a verified
  `[1080p SDR]` movie file. The embedded-title update then failed because the
  verifier read the non-faststart MP4 through a pipe.
- Inspection and remux now use the same seekable Linux descriptor as probing.
  On the acceptance server, a generated non-faststart MP4 rewrites correctly;
  mp4/m4v/mkv/mov/avi/webm, HDR, chapters, attachments and packet-preservation
  tests pass. `go test ./...`, `go vet ./...` and `go build ./...` pass.
- The interface copy sweep removed decorative page subtitles, section summaries
  and explanatory footers while keeping operational warnings and field help.
  Prettier, TypeScript, all 194 interaction tests and the production build pass.
- On the live stack, 0.34.5 rewrote the embedded title of a non-faststart MP4 in
  place with no retained copy, then the generated test download was removed.
  Schema 49, the VPN tunnel and kill switch remained up, and the built-in client
  stayed enabled as the torrent default.

## Version 0.35 Torrents, YouTube downloader and series sources

- Torrent service tests cover disabled-client reporting, listing, pause,
  recheck, reviewed removal with data deletion, invalid hashes and bounded
  manual magnet/metainfo input with the built-in tag and folder.
- YouTube tests cover quality serialization, progress fields, baseline then
  exact SxxExx queueing, duplicate prevention, existing-file/unmonitored skips,
  playlist-order preview and the reviewed import boundary. Automatic import is
  only approved through the same verified import review.
- Frontend tests cover the MeTube-style fetch/select/review/queue flow and the
  import evidence dialog; the Series page test covers the added source panel and
  the Torrents page test covers listing and the disabled state. Prettier,
  TypeScript, 196 interaction tests and the production build pass.
- Live 0.35.0 (schema 51) kept the VPN up, the built-in client enabled and
  selected as the torrent default, and automation paused. Torrents listed a
  manual Sintel magnet, reported metadata fetch, accepted reviewed removal with
  data deletion and returned to empty. YouTube inspected a public video with a
  thumbnail and duration, queued a 720p download, reported live progress/speed
  and cancelled it; the test staging directory and job record were removed. A
  per-show YouTube source on a real show previewed a channel, saved paused,
  recorded its baseline and was removed without queueing a download.

## Version 0.36 Playlist to series

- Playlist tests cover saved-show identification, full-episode preference over
  clips, explicit episode numbers and the episode-title fallback, duplicate and
  unmatched marking, preselection, explicit missing-episode queues, replacement
  consent and the acquisition reservation.
- The frontend batch test covers the show/season preview, episode table,
  preselection, the download action and the exact queue request. The YouTube
  single-video tests cover the renamed steps and review actions.
- YouTube imports now append the inspected resolution/SDR/HDR quality tag to the
  organized episode filename, matching regular downloads.

## Version 0.36.1 MKV YouTube downloads

- The downloader adds `--merge-output-format mkv` and `--remux-video mkv`; the
  existing probe/import validation accepts the resulting MKV and the quality tag
  is appended to the organized filename.
- The playlist batch action selects eligible missing episodes and, only after an
  explicit replacement choice, eligible episodes already in the library.

## Version 0.37 Archive extraction

- Archive set tests cover old-style and part-named RAR volumes, ZIP, 7z, .001
  splits, missing first volumes and multiple sets. Error classification covers
  passwords/encryption, incomplete volumes, CRC failures and generic extractor
  failures.
- A generated ZIP is extracted with the real 7z tool and imported through the
  normal movie importer; the extraction manifest, virtual file list and cleanup
  are verified. On the acceptance server the live RAR test extracted RAR5 single
  and multi-volume fixtures with unrar and classified an encrypted fixture as a
  password error.
- Schema 52 adds the durable extraction path. Docker now installs unrar
  (non-free) and p7zip-full in the runtime and test stages.
- A queued playlist job is processed end to end with an explicit language list
  and 1080 quality; this covers the subtitle-options language array column that
  the worker reads.

## Version 0.37.1 YouTube progress for fragmented formats

- The progress parser accepts NA totals/rates, and the queue renders downloaded
  bytes with an indeterminate bar instead of a false 0%. A unit test covers the
  NA line and rejects unrelated or invalid progress lines.

## Version 0.37.2 Canonical YouTube replacement names

- Organization replacement planning recomputes the YouTube acquisition
  destination from the captured show snapshot, episode mapping and inspected
  probe quality. The swap review therefore shows and applies the canonical
  quality-tagged filename; provenance is updated to the same path.
- Active YouTube jobs ignore fragment-sized totals from yt-dlp and render an
  indeterminate bar with downloaded bytes/speed; the verified completion stores
  the real total. This prevents a short fragment from appearing as 84% of a
  one-hour episode.

## Version 0.37.3 Multi-episode replacement reviews

- Idle checks now ignore other YouTube jobs in awaiting_review while still
  blocking on queued, downloading or importing jobs. This allows reviewing a
  batch of acquired episode replacements one after another without cancelling
  the others. A unit test covers both states.

## Version 0.38.0 interface overhaul

Run locally on Windows (development tools only; Docker remains the target):
- `npm run format:check`, `npm run check`, `npm test` (39 files, 220 tests)
  and `npm run build` passed.
- `go vet ./...`, `go test ./...` and `go build ./cmd/reelscarey` passed.
  `gofmt -l` lists only the pre-existing `internal/requests/lists_test.go`.
- Visual review against a disposable local backend with an empty database in
  dark and light themes. The Torrents console was reviewed with browser-side
  fixture responses only; no real qBittorrent was attached, so the live
  qBittorrent and Docker acceptance suites were not run for this change.

## Version 0.38.1 settings and admin pages

- npm format:check, check, test (39 files, 220 tests) and build passed.
- go vet ./..., go test ./... and go build ./cmd/reelscarey passed.
- Visual review against a disposable local backend with an empty database.
  Live qBittorrent and Docker acceptance were not run locally.

## Version 0.38.2 announce feeds and calendar coverage

- npm format:check, check, test (39 files, 220 tests) and build passed.
- go vet ./..., go test ./... and go build ./cmd/reelscarey passed.
- Visual review against a disposable local backend; live acceptance not run.


## Version 0.38.3 dismissible coverage notes and stable episode toggles

- npm format:check, check, test (39 files, 221 tests) and build passed.
- go vet ./..., targeted go test and go build ./cmd/reelscarey passed.
- Visual check with fixtures: dismissal state, restore link and unchanged
  scroll position after an episode monitor toggle.


## Version 0.38.4 built-in client included by default

- gofmt clean for changed files; go vet ./internal/... passed.
- go test ./internal/transfer ./internal/httpapi ./internal/automation passed,
  including rewritten built-in lifecycle tests (provision, opt-out persistence,
  kill switch, peer port) and a new selection-order test.
- Frontend format:check, check, full test suite and build passed.
- The opt-in live bundled lifecycle (REELSCAREY_LIVE_BUNDLED) and Docker smoke
  tests were not run locally; CI runs the bundled lifecycle on Linux.
- The first v0.38.4 publish attempt failed in the backup container smoke: it
  assumed a fresh preference revision and a single saved client. The script now
  reads the live revision and finds the named client by id; the tag was
  re-pointed to the fix and the publish re-run. Nothing was pushed by the
  failed attempt (it verifies before pushing).
- Published: the first two tag runs failed in the backup container smoke (a
  hardcoded preference revision, then a GET with no route). The script now
  reads the revision from the client list. The tag was re-pointed and the
  final run passed, including the release-candidate startup, backup restore
  and storage recovery verification.

## Version 0.38.5 seeder priority, title searches and unambiguous TV imports

- gofmt clean for changed files; go vet and the full go test suite passed
  (schema version assertions moved to 53 for migration 053).
- Frontend format:check, check, the full test suite (39 files, 223 tests) and
  the build passed. New tests cover seed dominance, focused-search scoping
  and batch continuation, the new API routes, auto-import and the review
  path for ambiguous labels.
- The live qBittorrent and Docker acceptance suites were not run locally; the
  publish workflow's release-candidate verification runs them on the tag.

## Version 0.38.6 calendar actions, bulk searches and episode lifecycle

- gofmt clean; go vet and the full Go suite passed (schema assertions moved to
  54 for migration 054). New tests cover synopsis propagation, monitor-all
  scoping, bulk search scoping, and per-episode unlink.
- Frontend format:check, check, the full suite (39 files, 225 tests) and the
  build passed. New tests cover calendar bulk confirmation and reported counts,
  remembered filters and Reset, and the popup's search actions.
- Live qBittorrent and Docker acceptance were not run locally; the publish
  workflow verifies the release candidate on the tag.

## Version 0.38.7 scoped replacement searches and real episode deletion

- gofmt clean; go vet and the full Go suite passed. New tests cover deletion,
  the shared-file refusal, unlink-without-delete and honest timeline events.
- Frontend format:check, check, the full suite (39 files, 227 tests) and the
  build passed. New tests cover the locked Replacing view and the season-scoped
  purpose list.
- Live and Docker acceptance were not run locally; the publish workflow verifies
  the release candidate on the tag.

## Version 0.38.8 search links, queue history and single-page libraries

- gofmt clean; go vet and the full Go suite passed (schema assertions 55).
- Frontend format:check, check, the full suite (40 files, 236 tests) and the
  build passed. New tests cover feed date layouts, info links, completed-queue
  exclusion and history events, NZBGeek prefill, A-Z grouping, WIP persistence
  and the single-page fallback.
- Live provider/Docker acceptance was not run locally; the publish workflow
  verifies the release candidate on the tag.

## 2026-11-07: faster release builds and configurable runners

- `Dockerfile` accepts `RUN_GO_TESTS` (default `1`). The backend stage runs
  `go test ./... && go vet ./...` only when it is `1`; `go build` always runs.
  `docker-publish.yml` passes `RUN_GO_TESTS=0` to the release-candidate and
  publishing builds; local/manual `docker build` and the acceptance image keep
  the default so their in-image tests still run.
- `ci.yml` and `docker-publish.yml` build through
  `docker/build-push-action@v7` with GitHub Actions BuildKit cache scopes
  (`reelscarey-ci`, `reelscarey-publish`, `reelscarey-transmission`,
  `mode=max`, `ignore-error=true`). The acceptance image build moved from
  `docker build` to the same action with `load: true`, and the disposable
  Transmission test image now uses buildx cache explicitly.
- Push/tag/schedule/manual jobs select runners from `REELSCAREY_RUNNER`,
  `REELSCAREY_ACCEPTANCE_RUNNER` and `REELSCAREY_PUBLISH_RUNNER` (default
  `reelscarey-ci`). Pull-request jobs are pinned to `ubuntu-latest` so fork
  code never runs on a self-hosted machine.
- Verified locally: file review, YAML parsing of both workflows with PyYAML
  6.0.3, and `actionlint` 1.7.7 (built from source, run on both workflow
  files) with no findings. The Dockerfile's `RUN_GO_TESTS` shell conditional
  was executed in a POSIX shell and skips the tests for `0` while the
  following build line still runs. Docker is unavailable and no workflow run,
  image build or container smoke test was performed for this change, so no
  build-time improvement is claimed until CI measures it.

## Version 0.39.0 bundled Usenet, seeding limits and subtitle redesign

- gofmt clean; go vet and the full Go suite passed (schema 56). New tests cover
  the seeding policy evaluation and API, and the bundled Usenet lifecycle
  (stub binary + fake API), queue, deletion review and settings.
- Frontend format:check, check, the full suite (41 files, 246 tests) and the
  build passed. The overwritten legacy Usenet test file was restored and the
  new page tests moved to UsenetPage.test.tsx.
- Not run locally: a Docker image build and the gated live SABnzbd test
  (REELSCAREY_LIVE_BUNDLED_SAB=1). The publish workflow builds the release
  candidate; verify the image and the live daemon before relying on them.

## Version 0.39.1 whole-library selection, future-only monitoring, release dates

- gofmt clean; go vet and the full Go suite passed (schema 57). New tests cover
  5000-ID monitoring, mixed-revision refusal, future-only monitoring, TMDb
  release_dates parsing, per-kind calendar rows and filters, and a bulk search
  that queues with only the built-in client configured.
- Frontend format:check, check, the full suite (42 files, 253 tests) and the
  build passed. New tests cover >200 selection, A-Z/date filters, live calendar
  refresh, batch progress and the Dashboard.
- Docker and live provider acceptance were not run locally; the publish
  workflow verifies the release candidate on the tag.

## Version 0.39.5 bounded bulk searches

- gofmt clean; go vet and the full Go suite passed (schema 65). New regression
  tests cover a search-only bulk pass across batches, a pass with no matching
  releases, and refusal to start again after the request clears.
- Frontend format:check, check, the full suite (47 files, 263 tests) and the
  build passed. A new test covers the Calendar reporting that it is waiting for
  the search already holding the scheduler.
- The live 0.39.4 database showed a Calendar episode request re-searching the
  same four episodes airing that day every few seconds. The focused pass now
  stops after each target is visited once instead of counting still-missing
  items forever.

## Version 0.39.6 Calendar bulk-search response envelope

- The Calendar and Series bulk-search progress hook now reads the documented
  `{items}` envelope from `GET /automation/runs`. It previously treated the
  response as a bare array, so every poll threw and the interface stayed on
  "Starting search…" while the backend actually ran the search.
- The polling error path now stops after its bounded attempts with a visible
  pointer to Automation instead of retrying silently forever.
- Frontend format:check, check, the full suite (47 files, 264 tests) and the
  build passed. Tests now use the real `{items}` envelope and add a regression
  for an unreadable run list. Go is unchanged from 0.39.5.

## Version 0.39.7 all missing shows and automatic downloads

- An explicit title or Calendar bulk search now also checks monitored Anime
  shows whose per-show automatic Anime search is off, using standard
  season/episode identity for that request. Scheduled cycles keep the per-show
  opt-in. The saved Anime policy is unchanged by the search.
- Go tests cover the disabled-Anime bulk queue and the scheduled exclusion. The
  full Go suite, vet and build passed.
- The frontend is unchanged from 0.39.6 (47 files, 264 tests).
- Live 0.39.7 ran an enabled download-mode schedule: one run checked all eight
  released missing episodes across five monitored shows, including the two
  Anime shows whose per-show search was off, and queued the matching Black
  Clover episode. The seven others had no release satisfying the profile and
  exact identity rules. The schedule is enabled at 360 minutes; the two missing
  Anime shows were switched to season matching so scheduled cycles include them.

## Version 0.39.8 auto-pause for public torrents

- The Torrents page has a **Pause public when done** toggle. When enabled, a
  supervised pass pauses finished torrents from public sources (the Nyaa
  indexer and manual uploads) in the built-in client; private-tracker and
  unknown-origin torrents keep seeding. Pausing never deletes anything, and
  each pause writes a History entry.
- Queued downloads record their source classification in a new
  `downloads.indexer_public` / `music_downloads.indexer_public` column
  (migration 066). Existing rows keep 0, the fail-safe "never auto-pause"
  value.
- `GET/PUT /api/v1/torrents/public-pause` reads and replaces the option behind
  a revision fence. The supervised seeding loop now runs both policies.
- Go tests cover the option round trip, which torrents are paused or skipped,
  queue-time classification for Nyaa/private/manual sources, and the HTTP
  routes; migration-schema and backup assertions moved to 66. gofmt, vet, the
  full Go suite and the build passed.
- Frontend format:check, check, the full suite (47 files, 265 tests) and the
  build passed. The new toggle has a Toolbar test covering both directions
  with revision fencing.

## Version 0.40.0 explicit add-time monitoring

- Movie adds offer **Add unmonitored** and **Add monitored**. Both send an
  explicit value, so the result no longer depends on the release-date default.
- TV adds offer a dropdown: **Add without monitoring**, **Add monitoring
  missing**, **Add monitoring current season** and **Add monitoring future
  episodes**. `POST /api/v1/library/series` accepts `monitor` with those four
  modes; legacy `monitored` still works and `monitor` wins when both are sent.
  Unknown modes return 400 `invalid_monitoring`.
- The server applies the one-time choice to regular seasons and episodes:
  none pauses the show; missing selects started seasons and aired episodes;
  current selects the current (or next) season; future selects the current and
  later seasons with episodes releasing today or later. Specials stay
  unmonitored. No episodes are loaded and no downloads are queued by the add.
- Go tests cover the full season/episode matrix for all four modes, plus HTTP
  validation and the `monitor` body. gofmt, vet, the full Go suite and the build
  passed.
- Frontend format:check, check, the full suite (47 files, 267 tests) and the
  build passed. New tests cover the movie unmonitored button, all four show
  menu choices and their request bodies.

## Version 0.40.1 dropdown surface sizing

- Radix Themes copies the `radix-themes` class onto portalled menu, select and
  popover content. The app's page-only `min-height: 100vh` and page background
  were therefore applied to every dropdown, stretching it to the viewport.
- Page sizing now requires the new `rs-app-theme` class on the root Theme; the
  theme tokens stay on all surfaces. Verified in Chromium: the four-item Add
  show menu is 104px high instead of filling the viewport.
- A new Theme test asserts the root class and that the portalled menu neither
  carries the page-only class nor computes `min-height: 100vh`. Type-check and
  the affected Movie, TV and Torrents tests pass.

## Version 0.41.0 release feed check and shared TMDb default

- The release feed check is enabled every 15 minutes and matches feed entries
  only to monitored missing titles; registered files are never replaced. Feed
  identity is deduplicated per indexer, download URLs stay inside the adapter
  boundary, and the Automation page exposes the feed status and recent matches.
  The full Go suite and the frontend suite passed with the feed work before this
  release.
- `REELSCAREY_TMDB_TOKEN` can supply a deployment-wide shared TMDb Read Access
  Token. It is used only while no token is saved in Settings, so an owner can
  replace it; removing the saved token falls back to the shared value. The
  interface labels it **Shared default** and never displays or returns the
  token. `GET /api/v1/providers/tmdb` adds `using_default` and
  `default_available` without changing the existing fields.
- Go tests cover the environment token validation, default/saved/fallback token
  selection and the HTTP status contract. gofmt, vet, the full Go suite and the
  build passed.
- Frontend format:check, check, the full suite (48 files, 270 tests) and the
  build passed, including the shared-default replace/fallback flow.

## Version 0.41.1 persistent and precise bulk-search results

- The Calendar and Series completion alert no longer uses a dismissable notice
  key, so a previous "don't show again" choice cannot hide a new result. It
  stays visible until Dismiss is clicked or another search starts.
- Search runs now expose `matched`: targets where an eligible release was
  found, whether it was queued or saved for review. Messages report
  checked/found/queued plainly, for example "Searched 4 items; found 2 matching
  releases and queued 2 downloads", "found 1 matching release, but queued no
  downloads", "found 2 matching releases, saved for review in Automation",
  "no matching releases were found", or "No items needed a search."
- The Automation run label also shows the matched count.
- Go tests cover the matched count in search-only and download modes. Frontend
  format:check, check, the full suite (49 files, 273 tests) and the build passed,
  including direct message-variant tests and a hidden-notice regression.

## Version 0.41.2 accurate TV import copy

- The Automation download-mode notice now states that a single-episode TV
  download imports automatically when one video exactly matches the reserved
  episode; packs, multi-file releases and ambiguous names wait for
  episode-to-file review. The indexer guide says the same instead of claiming
  every TV import needs review.
- Type-check and the Automation UI test pass.

## Version 0.42.0 TV pack imports and pack-first searches

- A completed TV download now imports without review when every reserved episode
  has exactly one completed file whose parsed label names it. This covers labeled
  season packs and shared range files; extra videos that do not name a reserved
  episode are ignored. Replacements, missing target files, duplicate/ambiguous
  labels and multi-target Anime mapped packs keep the explicit mapping review.
- Season release searches mark pack releases and list them first. A show-scoped
  automatic search tries one eligible season pack per season with multiple
  missing episodes, then falls back to individual episodes. The Calendar bulk
  search still searches its per-target batch.
- Quality pack targets verify the show title and season or complete-series
  evidence before applying the ordinary profile rules. The pack queue guard
  rechecks every reserved episode, profile and revision after metainfo.
- Go tests cover automatic labeled pack import, ambiguous review fallback, a
  pack-first show search with batch size 1, and pack identity/ranking. gofmt,
  vet, the full Go suite and the build passed.
- Frontend format:check, check, the full suite (49 files, 273 tests) and the
  build passed. Release lists show a Pack label and the updated import notice.

## Version 0.43.0 Sonarr and Radarr backup title import

- Settings → **Sonarr / Radarr Import** accepts a backup ZIP, previews the saved
  movie or series title list with monitored state and TMDb/TVDB identity, and
  adds only selected titles. This is the path for Radarr movies with future
  release dates that are not yet in ReelScarey.
- The importer reads only the Radarr `Movies` (or `Movies`/`MovieMetadata`) and
  Sonarr `Series` tables. Indexers, download clients, credentials, API keys,
  paths, files, tags and quality profiles are never read or imported. The
  temporary database is deleted after preview.
- Radarr movies preserve the backup's monitored flag and therefore appear in
  Wanted/Calendar with their future release date. Sonarr TVDB IDs resolve to
  TMDb shows; added shows start with future-only monitoring.
- Go tests cover Radarr v3/v4-style schemas, Sonarr series, rejection of a
  non-backup ZIP, HTTP preview/apply and duplicate skipping. The full Go suite,
  vet and build passed.
- Frontend format:check, check, the full suite (50 files, 274 tests) and the
  build passed, including the backup review/apply flow.

## Version 0.43.1 Radarr and Sonarr calendar import

- The same Settings page accepts a Radarr or Sonarr `.ics` calendar export as
  well as a backup ZIP. The calendar parser reads only `SUMMARY`, `DTSTART` and
  `UID`; descriptions, categories and URLs are ignored.
- Radarr release-kind suffixes (`Theatrical`, `Digital`, `Physical`, `Cinema`)
  are stripped, duplicate release events for one movie collapse to the latest
  date, and the event date/year is shown in the review. Calendar titles dated
  before today start unselected.
- Calendar events have no TMDb ID, so the preview resolves them through an exact
  TMDb title search (with the event year for movies). Multiple or missing matches
  stay unselected for manual addition.
- Tests cover the Radarr and Sonarr calendar formats, `ParseAny` detection and
  the HTTP calendar preview/apply path. The full Go suite, vet and build passed;
  frontend format:check, check, the full suite (50 files, 274 tests) and the
  build passed.

## Version 0.44.0 cast roles and quick-add

- Cast cards keep the current TMDb person link and add an **Other roles** action.
  The dialog reads the person's latest 24 combined cast credits, excludes the
  title currently open, filters adult and duplicate entries, and sorts by date
  then popularity.
- Movie roles reuse **Add unmonitored/Add monitored**; TV roles reuse the normal
  monitoring dropdown. Movies go to Movies, TV to TV Shows, and Japanese
  animation (TMDb animation genre plus Japanese language/country) to Anime.
  Already-saved titles are marked **In library**.
- `GET /api/v1/metadata/people/{id}/credits` returns the bounded list and
  `in_library`; opening the dialog is read-only and never adds titles or queues
  downloads by itself.
- Go tests cover filtering, sorting, the 24-role limit, Anime classification,
  exclusion and library state. The full Go suite, vet and build passed.
- Frontend format:check, check, the full suite (51 files, 275 tests) and the
  build passed, including the cast modal quick-add flow.

## Version 0.44.1 movies searched before their saved release date

- Scheduled, manual, bulk and feed matching share the same eligible-target query.
  Movies no longer require a valid saved date on or before today: every monitored
  missing movie with no active job is searched, so a release that lands earlier
  than its scheduled date is still found. TV episodes still require a valid aired
  date on or before today.
- The SQL argument lists were updated with the removed movie date predicate across
  scheduled search, focused search, pack searches, Anime mapped searches and the
  release feed. Invalid episode dates still authorize no provider work.
- Go tests cover invalid episode dates remaining blocked, monitored missing movies
  with empty, invalid, undated or future saved dates still being searched, and
  cursor rotation including a future-dated movie. The full Go suite, vet and build
  passed.
- Frontend format:check, check, the full suite (51 files, 275 tests) and the build
  passed. The Automation copy now states that movies do not wait for their saved
  release date.

## Version 0.46.1 existing-library hints and duplicate episode variants

- Discovery hints now remove trailing `-`, `.`, `_` and space separators after
  truncating an episode label, so `Hunter x Hunter - S02E63 - A x Hard x Master.mkv`
  pre-fills `Hunter x Hunter` rather than `Hunter x Hunter -`. The numbered
  filename still retains `A x Hard x Master` as the exact episode-title evidence
  for the cross-season fallback; show-title tokens and quality-looking words in
  the episode title are not treated as release labels.
- Files that resolve to one canonical episode are no longer a dead end. The scan
  records them as a duplicate group, keeps every variant in review, and exposes
  up to 100 groups through read-only `GET /library/imports/{id}/duplicates`.
  `POST /library/imports/{id}/duplicates/resolve` requires an explicit reviewed
  choice, queues that one file through the ordinary verification path and marks
  the other variants skipped with a message naming the chosen file. No two files
  are registered for one episode. A variant covering an additional episode is
  refused and stays in review.
- The match review resets its query, remote results, chosen title, episodes,
  review checkbox and error when the reviewed file changes, and initializes from
  the cleaned hint. A Fire Force-like previous query therefore cannot leak into a
  Hunter x Hunter review.
- Go tests cover hint cleanup, `Hunter x Hunter - S02E63 - A x Hard x Master.mkv`
  identity, duplicate grouping and reviewed resolution (including a refused
  multi-episode variant), and unreviewed resolution rejection. Frontend tests
  cover the grouped duplicate resolution action and the stale-query reset.
  `go test ./internal/transfer` passed except
  `TestLargeExistingRegistrationDoesNotReadWholeVideo`, which could not create its
  sparse fixture because the development volume was full
  (`There is not enough space on the disk`). The remaining transfer tests passed
  with that test skipped, and `go test ./internal/httpapi`, `go vet`, the frontend
  format check, TypeScript check and the targeted frontend suite passed.

## Multiple library scan roots

- Migration 071 adds `library_root_config`, `library_scans.roots` and
  `music_scan_roots`, backfills existing scan root snapshots and removes the
  single-active-scan index so several scans can wait while the worker leases one.
- Video discoveries scan every configured root or one selected configured root.
  The scan stores its root snapshot, identity/prefix checks use the matching root,
  and a failed root marks the scan failed instead of reporting a partial success.
- Music scans accept configured roots in one scan and record the root that owns
  each registered file. The default Music root resolves the exact existing
  `Music` or lowercase `music` directory; a missing folder is unavailable.
- Root updates reject absolute paths, `..`, missing folders, symbolic links,
  duplicates, parent/child overlaps and cross-library overlaps. They never change
  organization destinations or naming templates.
- Go tests cover persistence across reopen, unsafe/overlapping roots, lowercase
  default detection, video multi-root discovery and failure, music multi-root
  registration and API auth/CSRF. Frontend tests cover add/remove root editing,
  choosing one/all video roots, and scanning multiple configured music roots.

## AcoustID and Chromaprint matching

- Migration 072 adds the `music_fingerprints` cache and adds `acoustid` to the
  encrypted integration allowlist; existing integration rows and triggers are
  preserved.
- `internal/music/acoustid_test.go` uses a fake fingerprinter, a fake AcoustID
  HTTP server and fake MusicBrainz responses. It covers a strong unique release
  group, recording-ID-first track matching, the 0.85 score floor, multiple and
  missing release groups, tagged release-group/exact-album disambiguation,
  no match, invalid key, rate limit, missing key and
  `fpcalc`, 20-file deferral, cached fingerprint reuse, size/mtime invalidation,
  status without key disclosure and bounded `fpcalc` output parsing.
- `internal/httpapi/music_test.go` covers AcoustID status/save/delete
  authentication and CSRF, no key in responses, fingerprint route validation
  and 400/429 error mapping.
- The Docker runtime installs Debian `libchromaprint-tools`; the disposable
  music container smoke script checks `fpcalc -version`. This was validated with
  generated/fake media only: no owner media, real AcoustID account or live
  provider request was used.

## Announce feeds: per-source TLS certificate verification (2026-11-11)

A live IPTorrents announce source failed on the production instance with
`tls: failed to verify certificate: x509: certificate has expired or is not yet
valid: current time 2026-11-11T01:18:30-06:00 is after 2026-01-24T23:05:21Z`.

- Root cause confirmed against the tracker, not the clock: `irc.iptorrents.com`
  (port 6697) serves a Let's Encrypt R12 certificate valid from 2025-10-26 to
  2026-01-24, while the host and the `reelscarey` container both report
  2026-11-11 UTC. A strict `openssl s_client -verify_return_error` check returns
  `verify error:num=10:certificate has expired`. Port 6667 is open but plaintext
  and triggered the tracker's "Reconnecting too fast" throttle, so only 6697 is a
  usable endpoint.
- Added a per-source **Skip TLS certificate verification** option
  (`SourceSettings.SkipTLSVerify`, `SourceStatus.SkipTLSVerify`,
  `SourceUpdate.SkipTLSVerify *bool`). It defaults to off, maps to
  `tls.Config.InsecureSkipVerify`, and logs a startup warning while enabled. The
  pointer field means the pause/resume toggle, which omits it, keeps the saved
  value instead of clearing it.
- The option lives inside the encrypted settings blob, so no schema migration was
  needed.
- `internal/announce/irc_test.go` adds `TestSourceSkipTLSVerifyPersists` (save,
  unrelated update keeps it, explicit false clears it) and
  `TestIRCTLSCertificateHandling`, which starts a self-signed TLS IRC stand-in
  (`tlsTestServer`) and proves an untrusted certificate is rejected by default
  and read through when skip verify is on.
- `internal/announce/service.go` and `internal/announce/irc.go` carry the setting
  through CRUD and the TLS dial. The UI adds the checkbox with warning help text
  and an IPTorrents certificate note; `docs/ANNOUNCE.md` (walkthrough step, new
  "TLS certificate errors" section, security bullet) and `docs/API.md` document
  it for other users. Version bumped to 0.53.3 across `web/package.json`,
  `web/package-lock.json`, the `Dockerfile` `ARG VERSION` and
  `cmd/reelscarey/main.go` (which had drifted to 0.53.1).
- Gates: frontend `format`/`format:check`/`check`/`test` (56 files, 318 tests) and
  `build` passed; backend `gofmt -l` clean, `go test ./...` passed, `go vet ./...`
  clean, `go build ./cmd/reelscarey` succeeded.
- No owner credentials (passkey or NickServ password) were used or written during
  the local checks; the tracker was only probed with throwaway nicks until it
  began throttling.

## Announce feeds: join only after IRC registration (2026-11-11)

Live IPTorrents acceptance on the production instance showed the 0.53.3 TLS fix
was necessary but not sufficient: with skip verify enabled the source connected,
stayed up and captured nothing.

- Read-only socket inspection (`ss -tni` inside the container) showed the
  connection ESTABLISHED to 51.75.76.181:6697 with `bytes_received` growing only
  by PING/PONG exchanges. A single throwaway-nick `openssl s_client` probe then
  showed the server answering an early JOIN with `451 You have not registered`
  and later closing with `Registration Timeout`. The client had been sending
  JOIN before the server welcome, so the join was dropped and never retried.
- `internal/announce/irc.go` now waits for `001` before NickServ identify, the
  invite command and JOIN; a 90-second registration deadline (a field, shortened
  in tests) closes a connection whose server never welcomes; server `ERROR`
  lines and channel-related 4xx/5xx numerics become the source error instead of
  silence; a successful join logs `announce source joined the IRC channel`.
- New tests: `TestIRCClientWaitsForWelcomeBeforeJoining` (deliberately delayed
  welcome, 451 for any early JOIN), `TestIRCClientReportsJoinRefusal` (477),
  `TestIRCClientReportsServerError` (throttle text) and
  `TestIRCClientRegistrationTimeout`. Existing announce and TLS tests still pass.
- `docs/ANNOUNCE.md` gains a "Connected but no announces" section. Version
  bumped to 0.53.4.
- The only live IRC connections made for this diagnosis were the application's
  own source and one throwaway-nick probe; no owner credentials were used or
  written.

## Announce feeds: normalize IRC formatting (2026-11-11)

Production 0.53.4 logged a successful `#ipt.announce` join and remained healthy,
but still stored no events. The current Autobrr IPTorrents reader strips IRC
formatting before applying the same announce pattern; ReelScarey had been
matching the raw text instead.

- `stripIRCFormatting` now removes mIRC/IRC bold, italic, underline, reverse,
  reset, monospace, numeric color and hex color markers before definition
  matching. The original text is still passed to `Record` for evidence and
  SHA-256 deduplication.
- `TestDefinitionParseStripsIRCFormatting` covers a formatted IPTorrents
  announce and hexadecimal colors/monospace. Existing parser, registration and
  TLS tests continue to pass.
- The setup/troubleshooting documentation notes the normalization. Version
  bumped to 0.53.5.

Production verification on 2026-11-11 deployed the 0.53.5 image with the usual
configuration backup and health-check rollback. The container was healthy,
joined `#ipt.announce`, and retained an empty source error. Read-only copies of
the live database showed `last_event_at` advancing and the stored announce
count increasing from zero to one and then four. The latest rows contained
correctly parsed IPTorrents release names, `Anime` and `TV/Web-DL` categories,
and binary sizes. No additional IRC probe was used. The image build passed all
56 frontend test files (318 tests), formatting, TypeScript and the production
build. Its optional repeated Go test pass encountered two unrelated concurrent
SQLite `SQLITE_BUSY` failures in automation and library tests; the complete Go
test, vet and build gates had already passed natively, so the established
release build was rerun with that optional repetition disabled.
