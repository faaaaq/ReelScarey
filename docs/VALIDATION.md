# Foundation validation — 2026-10-22

Implemented and tested locally against the initially empty private
`faaaaq/ReelScarey` repository. External integrations and background workers are
explicitly absent. No real media library or provider account was used.

## Passed

| Check | Evidence |
| --- | --- |
| Go tests | `go test ./...` passes all config, database, settings and HTTP tests |
| Static Go checks | `go vet ./...` passes |
| SQLite | New database, WAL/foreign keys/busy timeout, repeated migrations, settings across reopen, Unicode/spaces in config path |
| Migration safety | Failed SQL rolls back DDL and ledger; modified migration checksum and newer schema are rejected |
| Settings | Folder traversal/device names/collisions rejected; concurrent same-revision updates produce one success and one conflict |
| HTTP | Health/readiness, real library counts/list, settings, error envelopes, unknown API routes, content type/body limits, malformed JSON, cross-origin rejection, SPA deep links, missing assets, readiness failure |
| TypeScript | `npm run check` passes |
| Frontend behavior | `npm test`: 4 tests pass (API-driven dashboard/navigation, settings save, conflict/reload, offline/retry) |
| Production frontend | `npm run build` succeeds with Vite; generated JS/CSS embedded into Go |
| Windows executable | Native `go build` succeeds and application launches |
| Linux executable | `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build` succeeds; cross-compiled, not executed |
| Process smoke test | `node scripts/smoke.mjs`: starts the real binary, verifies readiness/UI/JS/API, saves preferences, restarts, verifies persisted values and CLI healthcheck |
| Live browser | Dashboard renders at narrow viewport; settings save succeeds, reload retains value, default restored; mobile navigation and movie empty state work |
| Dependency installation | Locked npm install succeeds; npm reports zero known vulnerabilities at installation time |

Local tools: Windows amd64, portable Go 1.27.3, Node 24.12.0, npm 11.6.2.
The native network smoke test required the execution environment's loopback
network permission; it passed with that permission. Initial test failures in
Windows SQLite URI handling and the bare `/api` route were fixed and the suite
rerun successfully.

## Not executed / limitations

- Docker is not installed; neither `docker build` nor Compose/container runtime
  checks have passed here. The Dockerfile and CI smoke job are prepared, and the
  roadmap intentionally leaves container verification unchecked.
- Go race detector requires CGO and a C compiler; no supported compiler is
  installed here. Linux CI is configured to run `go test -race ./...`.
- GitHub CI has not run because this milestone has not been pushed.
- Linux runtime behavior, ARM builds, long-running resource measurements,
  crash/power-loss recovery, backup/restore drills, and production security
  review are not validated by this milestone.
- Browser testing covered the narrow in-app viewport; desktop CSS is included
  but a full desktop/browser accessibility audit remains future work.
- This release has no authentication and is only suitable for local evaluation.
  Media folder preferences do not yet create or mutate folders.
- An open-source license still needs the owner's choice before distribution.

## Reproduce

Run the commands in [README.md](../README.md). `scripts/smoke.mjs` accepts an
optional binary path, creates its own temporary database and configured roots,
and removes only that test-created directory. The CI workflow includes an
additional Docker health/FFprobe/restart persistence check for a Linux runner.

The next implementation task is authenticated, write-only provider configuration
followed by real TMDb movie search and idempotent add-to-library behavior, then
the remaining Torznab → qBittorrent → import vertical slice.
