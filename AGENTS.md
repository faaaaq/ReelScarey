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
- internal/metadata owns TMDb normalization and its MovieProvider boundary.
- internal/transfer owns Torznab/qBittorrent adapters, encrypted integration
  settings, release selections, leased download jobs and journaled imports.
- internal/library owns the shared library catalog. Future providers live in
  their feature modules behind narrow, consumer-owned interfaces.
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

## Validation
- Frontend: `cd web && npm ci && npm run format:check && npm run check && npm test && npm run build`.
- Backend, after the frontend build: `go test ./...`, `go vet ./...`,
  and `go build ./cmd/reelscarey`.
- Linux CI additionally runs `go test -race ./...` and an authenticated Docker
  smoke test. The original foundation CI failure was an incomplete platform
  lockfile; regenerate locks without an existing node_modules directory.
- Linux CI installs qBittorrent-nox and FFmpeg from Ubuntu packages and runs the
  opt-in REELSCAREY_LIVE_QBT=1 test with its own localhost client, protocol
  fixture, private torrent and generated web-seeded video.
- Test input boundaries, persistence across reopen, migration rollback, API
  contracts, and meaningful user behavior. Use temporary directories only.
- Run gofmt on Go changes and `npm run format` for the frontend. Keep dependencies locked and explain additions.
- Record validation failures and environment limits; never mark an unrun check
  as passed. Use local Git commits for stable milestones.
