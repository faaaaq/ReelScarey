# ReelScarey contributor instructions

ReelScarey is one application: a Go modular monolith, one SQLite database,
and a React/TypeScript UI embedded into the application binary. Read
ARCHITECTURE.md and ROADMAP.md before expanding a module.

## Scope and honesty
- Docker is the sole supported deployment target. Do not treat standalone native
  or Windows builds, packages, preview upgrades, or acceptance as product goals.
  Development tools and historical validation may still run outside containers.
- Complete the foundation before the movie vertical slice. Complete that slice
  before adding substantial TV, automation, or subtitle functionality.
- Never present fixtures, empty adapters, inert controls, or roadmap items as
  working integrations. UI screens must distinguish unavailable features.
- Keep ROADMAP.md and docs/API.md consistent with implemented behavior.
- Do not push, publish, or modify external resources without user authorization.
- The project license requires an owner decision; do not silently choose one.

## Boundaries
- cmd/reelscarey is the composition root; internal/httpapi owns HTTP transport.
- internal/config owns process environment, internal/settings owns persisted
  preferences, internal/database owns SQLite and versioned SQL migrations.
- internal/auth owns the single administrator, password hashes and sessions;
  internal/securestore owns encrypted credentials and local key files.
- Request accounts have separate hashed sessions. The HTTP boundary must deny
  them all administrator APIs by default and expose only explicitly scoped
  requester routes. Request/list/notification reads and writes must bind to the
  resolved account, never an account ID supplied by a requester. Persist and
  validate encrypted Discord webhooks and Trakt client IDs with the existing key.
  List fetches use fixed provider hosts and fence changes made during a fetch.
  Ambiguous Discord delivery is never silently retried. See docs/REQUESTS.md.
- internal/metadata owns TMDb normalization and its MovieProvider/SeriesProvider boundaries.
- internal/transfer owns Torznab/qBittorrent adapters, encrypted integration
  settings, release selections, leased download jobs, TV episode reservations/
  reviewed file mappings and per-file journaled imports. TV shares the importer.
  It also owns explicit existing-library discovery and reviewed in-place
  registration. Discovery never authorizes a metadata match or a filesystem
  write; registered existing files must not fabricate download/release rows.
  Newznab/SABnzbd share review, reservations and importer; NZB checksums have a
  separate namespace. Uncertain SABnzbd submissions must reconcile, never
  automatically resubmit. Only completed post-processing in an exact dedicated
  mapped output folder may supply import candidates.
- internal/library owns the shared catalog, TV seasons/episodes and revision-checked monitoring. Future providers live in
  their feature modules behind narrow, consumer-owned interfaces.
  It also owns read-only Wanted/Calendar projections. Reads must not scan disk,
  contact providers, change monitoring or enqueue work. Registered availability
  and incomplete cached metadata must be labeled honestly; invalid dates must
  not authorize scheduled searches.
  Catalog browsing filters use saved parent status/monitoring; never imply that
  a partially registered show is complete. Keep literal queries parameterized,
  sort expressions allowlisted, and pagination stable. Shared UI copy must be
  functional; do not reintroduce slogans, promotional welcome art or decorative
  page labels. See docs/LIBRARY_BROWSING.md.
  Bulk monitoring must operate on explicit bounded IDs, compare the entire review
  before writing, preserve profiles/TV child choices and fence existing editors/
  automatic queue decisions through the current revisions. Keep selection visibly
  page-scoped. Retain the current layout and neutral graphite/blue palette; do not
  reintroduce olive backgrounds. See docs/BULK_MONITORING.md.
  Effective profile labels/filters must distinguish inheritance from explicit
  assignments. Automation owns bulk profile writes; compare the complete review,
  including profile rules and schedule revisions, before changing assignments.
  Preserve monitoring/files and fence pending automatic queue decisions. Quality
  owns canonical profile decoding; use transaction-aware reads for snapshots.
  See docs/BULK_PROFILES.md.
- internal/quality owns editable profiles, conservative release identity and ranking.
  Anime automation is per-show opt-in. Season/absolute modes are single-episode;
  mapped mode requires reviewed order consent, and specials and exact packs have
  separate opt-ins. Explicit aliases
  never infer canonical ordering; strip identity labels before quality scoring.
  Fence query/policy/category/numbering changes inside queue reservations and
  retain immutable search evidence on jobs. See docs/ANIME_SEARCH.md.
  Provider order mappings retain explicit typed labels and scoped identity links;
  AniList title similarity/counts never infer canonical episodes. TMDb proposals
  require series group membership and exact loaded episode IDs. Cross-season
  Anime packs require explicit reviewed targets and immutable order snapshots;
  automatic queues must reject these manual selections and use their own fenced
  mapped cohorts. Keep each shared file
  consecutive within one canonical season. See docs/ANIME_PROVIDER_ORDERS.md.
- internal/organization owns collection/naming rules, immutable import snapshots
  and reviewed registered-file reorganization journals. Keep canonical media and
  episode IDs; Anime is an explicit category with reviewed absolute aliases.
  Ordinary media workers must acquire ReadMedia once before their claim/IO and
  honor durable pending organization reservations. Avoid nested read locks.
  Publish without replacement, verify journal output ownership, switch subtitle
  and media references atomically, then clean up verified old library links.
  Main-root changes remain offline deployment migrations. Use the reviewed
  maintenance copy/activation flow; retain originals, verified configuration
  backups and startup/recovery barriers. Storage browsing is bounded, read-only
  and restricted to explicitly mounted roots. See docs/STORAGE.md and
  docs/ANIME_ORGANIZATION.md for scope and safety limits.
- internal/subtitle owns registered-media inspections, OpenSubtitles credentials/
  protocol, language/track preferences, leased jobs and journaled sidecar storage.
  Providers receive canonical metadata/hash only, never local paths. Existing
  video/sidecars must not be replaced by provider downloads; reviewed local
  sidecar edits must retain a verified previous version and journal publication
  and rollback while excluding ordinary media workers. Automatic selections require provider
  identity and hash/exact original-release evidence. Uncertain timing needs review.
- internal/youtube owns explicit public source review, durable acquisition and
  reviewed journaled video/subtitle imports. Preserve canonical target IDs,
  no-overwrite publication, source evidence and shared transfer reservations;
  custom collections must not fabricate provider identities. Sidecars require
  explicit language choice, bounded content, review and recovery alongside video.
- internal/automation owns interval scheduling, leased searches and bounded audit
  history. It delegates downloads/imports to transfer; automatic queueing must be
  fenced inside the reservation transaction against changed policy and monitoring.
- internal/instance owns OS config locks; internal/maintenance owns verified
  offline backups and new-folder restores; internal/operations owns explicit
  local checks, setup guidance and authenticated recovery review.
- web contains React, TypeScript, Vite, Tailwind, and Radix Themes. Use maintained
  accessible components. Go serves the built web/dist directory via embed.
- Do not import HTTP handlers into domain packages or create separate daemons.

## Reliability and safety
- Use context-aware operations, bounded HTTP bodies and timeouts, structured
  logs, parameterized SQL, and transactions for multi-step writes.
- Secrets must never appear in logs, API responses, or committed files. Session
  tokens travel only in HttpOnly cookies; CSRF tokens are separate browser state.
  `setup-token` is an explicit local-owner CLI command, never a log message.
- All application APIs require authentication except setup/login/session status.
  Every authenticated mutation requires CSRF and same-origin validation. Never
  add a bypass for tests to production code; use disposable test sessions.
- Store provider tokens only through securestore; its AES-GCM key must be backed
  up with the database. Missing/wrong keys must fail closed, never silently reset.
- Migrations are append-only after release. Back up SQLite before upgrading.
  Parent-table rebuilds must preserve existing IDs/consumers and pass the
  migration runner's foreign-key check before committing.
- Never test against an existing user library. Imports must validate containment,
  refuse overwrite, stage and verify copies, preserve torrent sources, and journal
  state for crash recovery. Media tests use generated files in disposable roots;
  real-client tests start a separate authenticated client.
- Durable jobs will use database leases and idempotent handlers. Do not promise
  exactly-once delivery or use an in-memory queue as the source of truth.
- New schedules must start paused and in search-only mode. Missing Torznab is a
  waiting state, not an error that prevents library/profile management. Existing
  files may produce upgrade suggestions but cannot be replaced by this milestone.

## Validation
- Frontend: `cd web && npm ci && npm run format:check && npm run check && npm test && npm run build`.
- Backend, after the frontend build: `go test ./...`, `go vet ./...`,
  and `go build ./cmd/reelscarey`.
- Linux CI additionally runs `go test -race ./...` and an authenticated Docker
  smoke test. The original foundation CI failure was an incomplete platform
  lockfile; regenerate locks without an existing node_modules directory.
- Linux CI installs qBittorrent-nox and FFmpeg from Ubuntu packages and runs the
  opt-in REELSCAREY_LIVE_QBT=1 test with its own localhost client, protocol
  fixtures, private movie/TV torrents and generated web-seeded videos. TV mapping
  must be confirmed explicitly before import, including in integration tests.
- Test input boundaries, persistence across reopen, migration rollback, API
  contracts, and meaningful user behavior. Use temporary directories only.
- Generated organization acceptance uses REELSCAREY_LIVE_ORGANIZATION=1 with
  FFmpeg/FFprobe: discover/register owned shared Anime media, review explicit
  absolute aliases, reorganize across reopen, retain styled ASS references and
  seeding links. Never use owner media. Exercise stale reviews, foreign outputs,
  partial-copy/publication recovery and durable worker exclusion.
- Automatic Anime acceptance uses REELSCAREY_LIVE_ANIME=1 on Linux with a private
  authenticated qBittorrent profile, local Torznab and generated owned video.
  Exercise the actual scheduler, explicit absolute alias, opt-out/alias edits
  after queueing, reopen, mandatory mapping review, FFprobe import and retained
  seeding bytes. Provider/account mocks alone do not prove this workflow.
- Provider-order pack acceptance runs with REELSCAREY_LIVE_QBT=1. Generate
  separate videos for specials and multiple canonical seasons; change the saved
  order after queueing, reopen before/after mapping, require explicit review and
  verify canonical folders and retained seeding sources. Public AniList identity
  acceptance uses REELSCAREY_LIVE_ANILIST=1 without owner credentials or media.
- Usenet acceptance uses isolated real SABnzbd and generated yEnc/NNTP articles,
  never owner accounts. REELSCAREY_LIVE_SAB=1 requires REELSCAREY_SABNZBD
  (executable/script); scripts may specify REELSCAREY_SABNZBD_PYTHON. SABnzbd
  is not bundled into the application.
- Automation checks must cover pause/lease/profile/monitoring changes during
  metainfo fetch, bounded retries/cursor rotation, duplicate prevention, TV review
  gating and preservation of registered files during upgrade detection.
- Subtitle checks cover quota/auth cooldowns, private download URL allowlists,
  missing/embedded/existing track detection, stale choices, policy fences and
  partial/published journal recovery without consuming download quota again.
  Generated FFmpeg/FFprobe acceptance uses REELSCAREY_LIVE_SUBTITLES=1; never
  an owner account or existing video. Preserve unrecognized staging files.
- Run gofmt on Go changes and `npm run format` for the frontend. Keep dependencies locked and explain additions.
- Record validation failures and environment limits; never mark an unrun check
  as passed. Use local Git commits for stable milestones.
