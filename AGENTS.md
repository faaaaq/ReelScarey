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
- internal/library owns the shared library read model. Future providers live in
  their feature modules behind narrow, consumer-owned interfaces.
- web contains React, TypeScript, Vite, Tailwind, and Radix Themes. Use maintained
  accessible components. Go serves the built web/dist directory via embed.
- Do not import HTTP handlers into domain packages or create separate daemons.

## Reliability and safety
- Use context-aware operations, bounded HTTP bodies and timeouts, structured
  logs, parameterized SQL, and transactions for multi-step writes.
- Secrets must never appear in logs, API responses, or committed files.
- Migrations are append-only after release. Back up SQLite before upgrading.
- Never test against a real library. No media mutation is implemented yet.
  Future import must validate containment, refuse overwrite, stage and verify
  copies, preserve torrent sources, and journal state for crash recovery.
- Durable jobs will use database leases and idempotent handlers. Do not promise
  exactly-once delivery or use an in-memory queue as the source of truth.

## Validation
- Frontend: `cd web && npm ci && npm run format:check && npm run check && npm test && npm run build`.
- Backend, after the frontend build: `go test ./...`, `go vet ./...`,
  and `go build ./cmd/reelscarey`.
- Linux CI additionally runs `go test -race ./...` and a Docker smoke test.
- Test input boundaries, persistence across reopen, migration rollback, API
  contracts, and meaningful user behavior. Use temporary directories only.
- Run gofmt on Go changes and `npm run format` for the frontend. Keep dependencies locked and explain additions.
- Record validation failures and environment limits; never mark an unrun check
  as passed. Use local Git commits for stable milestones.
