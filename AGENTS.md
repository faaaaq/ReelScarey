# ReelScarey contributor instructions

ReelScarey is one application: a Go modular monolith, one SQLite database,
and a React/TypeScript UI embedded into the application binary. Read
ARCHITECTURE.md and ROADMAP.md before expanding a module.

## Scope and honesty
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
- internal/metadata owns TMDb normalization and its MovieProvider/SeriesProvider boundaries.
- internal/transfer owns Torznab/qBittorrent adapters, encrypted integration
  settings, release selections, leased download jobs, TV episode reservations/
  reviewed file mappings and per-file journaled imports. TV shares the importer.
- internal/library owns the shared catalog, TV seasons/episodes and revision-checked monitoring. Future providers live in
  their feature modules behind narrow, consumer-owned interfaces.
- internal/quality owns editable profiles, conservative release identity and ranking.
- internal/subtitle owns registered-media inspections, OpenSubtitles credentials/
  protocol, language/track preferences, leased jobs and journaled sidecar storage.
  Providers receive canonical metadata/hash only, never local paths. Existing
  video/sidecars must not be replaced; automatic selections require provider
  identity and hash/exact original-release evidence. Uncertain timing needs review.
  internal/automation owns interval scheduling, leased searches and bounded audit
  history. It delegates downloads/imports to transfer; automatic queueing must be
  fenced inside the reservation transaction against changed policy and monitoring.
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
