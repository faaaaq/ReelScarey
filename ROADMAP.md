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
- [ ] Docker build and runtime smoke test executed successfully

## 2 — Movie workflow (current)
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
- [ ] Verify authenticated search/add against live TMDb with the owner's token
  entered directly in Settings. No real token has been supplied to this run.
- [ ] Configure/test a Torznab indexer; return normalized, real release results.
- [ ] Configure/test qBittorrent and enqueue the selected release once.
- [ ] Introduce persistent jobs, leases and restart-safe download reconciliation.
- [ ] Detect completion, map paths, probe files and plan safe imports.
- [ ] Hardlink without touching seeding sources; copy safely across filesystems.
- [ ] Journal imports, update library availability and display actionable failures.

Acceptance: a real movie moves from TMDb search through a real indexer and
download client to a verified library file. Restart during download/import must
recover; collision and cross-filesystem tests must pass. Disposable services and
temporary libraries only. Never use unauthorized content as test material.

## 3 — Television
- Series/seasons/episodes, monitoring and availability
- Episode and season searches, multi-episode release mapping
- Reuse download/import services; configurable, validated episode naming

## 4 — Automation
- Scheduled searches and monitoring, quality/language profiles
- Filtering/scoring, cutoff/upgrade detection and duplicate prevention
- Failed download recovery, retry controls and useful job visibility

## 5 — Subtitles
- Provider interface and first real provider
- Language preferences, existing subtitle detection and release-aware matching
- Automatic searches and safe subtitle storage

## 6 — Production hardening
- Setup wizard, broader integration tests and error recovery exercises
- Security review, secret/key lifecycle and threat model
- Performance/resource budgets on representative NAS hardware
- Release packaging, backup/restore drills and upgrade documentation
- Owner chooses open-source license before public distribution

## Validation record

2026-10-22: Foundation native checks passed. Its first GitHub run failed because
the npm lock omitted cross-platform dependencies. The lock was regenerated in a
clean directory; a clean Windows install and Linux resolution check now pass.
The movie catalog backend suite and 11 frontend tests pass. Native process smoke
checks include setup, login, persistent sessions/settings, logout and password
recovery/revocation. Full details and remaining live-provider/container checks
are in [validation evidence](docs/VALIDATION.md).
