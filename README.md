# ReelScarey

**Media management, without the horror.**

A unified, self-hosted media application in Go and React. This is the first
foundation milestone, not yet a replacement for Sonarr/Radarr/Prowlarr/Bazarr.

Working now: one server for UI and REST API, SQLite with versioned migrations,
persisted library settings, live dashboard counts and database status, read-only
library collections, responsive navigation, and explicit upcoming-feature screens.

Not implemented: metadata search, adding movies/TV, indexers, download clients,
imports, background jobs, subtitles, authentication, or production hardening.
No existing media is scanned or changed. FFprobe is packaged in the Dockerfile;
the application does not call it yet.

## Run with Docker

Requires Docker Engine/Desktop with Compose v2. The Docker configuration is
included; consult [validation](docs/VALIDATION.md) for what was actually run.

```sh
cp .env.example .env
docker compose up --build -d
docker compose logs -f reelscarey
```

On PowerShell, use `Copy-Item .env.example .env` for the first command.
Open [ReelScarey](http://localhost:8787). The port is published on loopback only.
This release has **no authentication**; do not expose it to shared networks or
the internet.

The named `reelscarey_config` volume persists the database under `/config`.
Change `MEDIA_PATH` and `DOWNLOADS_PATH` in `.env` for your host folders; they map
to `/media` and `/downloads` and are read-only in this milestone. Do not change
them to writable until the tested import workflow is available. The image runs
as UID/GID 10001; if you replace the config volume with a bind mount, give that
user read/write access. `docker compose down` keeps the database; adding `-v`
deletes the named volume and its data.

## Local development

Requires Go 1.27+ and Node 24.12+ (24.x). Build the frontend before compiling Go,
because frontend assets are embedded in the executable.

```sh
cd web
npm ci
npm run build
cd ..
go test ./...
go build -o bin/reelscarey ./cmd/reelscarey
./bin/reelscarey
```

On Windows build with `go build -o bin/reelscarey.exe ./cmd/reelscarey` and run
`.\bin\reelscarey.exe`. Open [the local app](http://127.0.0.1:8787).
To iterate on UI, keep Go running and run `npm run dev` in `web`; Vite proxies
the API to port 8787. Rebuild UI and Go for changes to the embedded production UI.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `REELSCAREY_LISTEN_ADDR` | `127.0.0.1:8787` | HTTP bind address |
| `REELSCAREY_CONFIG_DIR` | `./data/config` | SQLite directory (created if absent) |
| `REELSCAREY_MEDIA_DIR` | `/media` | Media root; Windows default `./data/media` |
| `REELSCAREY_DOWNLOADS_DIR` | `/downloads` | Download root; Windows default `./data/downloads` |
| `REELSCAREY_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |

Relative paths resolve against the working directory. Docker explicitly sets
the three paths to `/config`, `/media`, and `/downloads`. UI preferences are stored
in SQLite and survive process restarts. Folder settings are reserved for future
imports and do not mutate files. Structured JSON logs go to standard output.

## Checks

```sh
cd web
npm run check
npm run format:check
npm test
npm run build
cd ..
go test ./...
go vet ./...
go build ./cmd/reelscarey
```

After building into `bin`, run `node scripts/smoke.mjs` for a real process
startup/restart and API/frontend smoke test. It creates and removes its own
temporary config directory and does not use your library.

Linux CI also runs the Go race detector and a container restart/persistence
smoke test. `reelscarey healthcheck` checks database readiness with a timeout.
`GET /healthz` reports process liveness; `GET /readyz` checks SQLite.

## Backup and restore

Stop ReelScarey before copying the entire config directory/volume, including any
SQLite WAL/SHM files. Use SQLite-aware backup tools for live backups; copying the
database file alone during writes is unsafe. Restore into an empty config
location with the correct ownership, then start the matching application version.
Never attach two application instances to the same config directory. Keep SQLite
on local storage. Back up before upgrades; migrations run transactionally at
startup, and newer or altered migration histories are rejected. In-place
downgrades are not supported.

## Project references

- [Architecture and entity model](ARCHITECTURE.md)
- [Roadmap and acceptance criteria](ROADMAP.md)
- [REST API](docs/API.md)
- [Contributor rules](AGENTS.md)
- [Validation evidence](docs/VALIDATION.md)

The next milestone is one complete movie workflow: TMDb → Torznab → qBittorrent
→ restart-safe monitoring → safe import. Build and verify it before broadening
into television or subtitles.

## License

License selection is pending the repository owner's decision. No open-source
license is granted by this initial scaffold. Dependencies retain their own
licenses. This is an independent implementation; no code was copied from the
four applications that inspired its scope.
