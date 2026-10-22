# ReelScarey roadmap

Status is evidence-based: checked items are implemented, not planned adapters.

## 1 — Foundation (current milestone)
- [x] Go server, structured logging, graceful shutdown and validated environment
- [x] SQLite initialization, transactional checksummed migrations and persistence
- [x] Versioned REST API, health/readiness, settings and library reads
- [x] React/TypeScript/Vite/Tailwind/Radix UI with responsive navigation
- [x] Dashboard from live API data and editable persisted library settings
- [x] Multi-stage Dockerfile, Compose mounts and healthcheck configuration
- [x] Architecture, API, contributor and operational documentation
- [x] Automated tests and native launch/API/frontend verification
- [ ] Docker build and runtime smoke test executed successfully

## 2 — Complete movie vertical slice (next)
1. Authenticate administration and implement write-only provider credentials.
2. Implement TMDb movie search/details with timeouts, rate-limit handling and tests.
3. Add a movie idempotently; persist real metadata/artwork and monitoring state.
4. Configure/test a Torznab indexer; return normalized, real release results.
5. Configure/test qBittorrent and enqueue the selected release once.
6. Introduce persistent jobs, leases and restart-safe download reconciliation.
7. Detect completion, map paths, probe files and plan safe imports.
8. Hardlink without touching seeding sources; copy safely across filesystems.
9. Journal imports, update library availability and display actionable failures.

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

2026-10-22: native Windows tests, production build and restart smoke test passed;
Linux amd64 cross-build passed. Four frontend behavior tests passed. Live browser
navigation/settings checks passed. Docker runtime and Linux race checks remain
unverified because Docker and a C toolchain are absent on this host. CI is written
but has not run on GitHub. See [validation evidence](docs/VALIDATION.md).
