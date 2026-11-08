# ReelScarey

A unified, self-hosted media application in Go and React. Movie and TV downloads
and verified imports share durable scheduled searches and quality profiles.

Working now: administrator setup/login/logout, persistent sessions, encrypted
TMDb connection settings with real connection tests, TMDb movie search/details,
idempotent movie adds, filtered poster and table browsing, persisted library settings,
live dashboard counts, Torznab release search, qBittorrent downloads, persistent
download tracking and verified imports, plus TV search, canonical show adds,
season/episode lists, metadata refresh, persistent monitoring choices, episode and
season release searches, reviewed multi-episode file mapping and episode availability,
scheduled monitored searches, quality/language profiles, ranked release decisions,
opt-in automatic missing-item downloads and resolution upgrade suggestions,
OpenSubtitles.com connections, language/track preferences, embedded/sidecar
detection and safe automatic subtitle downloads for registered movie/TV files.
Managed Newznab sources and SABnzbd downloads share release search, automation,
reviewed TV mappings and safe importing. The Docker image can supervise a
headless SABnzbd client; a news server is always owner-supplied. See
[Usenet setup](docs/USENET.md).
Wanted and the monthly release calendar organize missing movies and loaded
episodes, saved dates and tracked job status. They work offline with explicit
search/Activity links. See [planning and metadata limits](docs/PLANNING.md).
One server owns the UI, REST API,
worker and SQLite database.

Movies and TV libraries support literal title search, status/monitoring filters,
stable sorting and bookmarkable poster/table views. The interface uses a compact
catalog layout and a live overview instead of promotional copy and welcome art.
See [library browsing and status limits](docs/LIBRARY_BROWSING.md).
Select titles on the current library page to review and apply parent monitoring
changes together. Profiles, season/episode choices and existing downloads are
retained. See [bulk monitoring](docs/BULK_MONITORING.md).
The library also shows/filters effective quality profiles and supports reviewed
profile assignment or return to default inheritance for selected titles. See
[profile assignment](docs/BULK_PROFILES.md).

Anime has a separate movie/show catalog with explicit absolute episode mappings.
Organize library configures Movies, TV Shows, Anime and custom collections,
Plex/Jellyfin/Emby presets or custom templates, immutable future-import naming and
reviewed batch video/subtitle reorganization. See [Anime and organization](docs/ANIME_ORGANIZATION.md)
for main-root setup, safeguards and exact limits. Anime shows can opt into automatic
single-episode searches using canonical season/episode numbers or explicitly saved
absolute numbers and alternate titles, or reviewed mapped singles and explicitly
enabled specials and packs. File mapping still requires review. See
[Anime automatic searches](docs/ANIME_SEARCH.md) and [remaining work](docs/REMAINING.md).
AniList/Kitsu identities and reviewed release-order mappings can cover separate cours
and specials without changing canonical TMDb episodes. TMDb episode-group proposals
join exact saved episode IDs. Reviewed manual packs can span seasons through
separate files, with immutable order history and mandatory import review. See
[Anime providers and episode orders](docs/ANIME_PROVIDER_ORDERS.md).

A Music tab catalogs MusicBrainz artists, albums and tracks, scans existing
files, registers reviewed matches in place, applies reviewed organization and
tag rewrites with rollback, searches audio releases and imports a completed
album folder, queues releases through the selected client, reconciles them with
a leased worker, and runs wanted-album searches on a schedule. See
[music catalog](docs/MUSIC.md).

YouTube source inspection supports explicit video selection from public channels
and playlists, mapped to existing movies/episodes or named custom collections.
Selected creator or automatic subtitles are reviewed and imported as VTT sidecars
with the video. See [YouTube sourcing](docs/YOUTUBE.md) for setup and limits.

Reviewed replacements can use discovered files, selected download upgrades and
YouTube videos, retaining the prior version for restoration. Spoken-language
verification and independent production security review remain outstanding. Automation starts paused and defaults to
search results for review; automatic downloads require explicit selection of that
mode. Existing-library discovery and registration require explicit review; subtitle
checks inspect only registered media.
FFprobe verifies completed video files before import. Sources remain in place
for seeding; replacements require explicit review and retain the previous video.

## Install with Docker Compose

Use [compose.simple.yaml](compose.simple.yaml) as your compose.yaml. Set your
folder mappings and browser URL, then run `docker compose up -d`. It supports
PUID, PGID, TZ and an existing reverse-proxy network without a separate env file.
See [installation and upgrades](docs/INSTALL.md) for the setup code, library folder
selection and existing-installation compatibility.

## Isolated live-testing installation

For an Ubuntu x86-64 server, use [the live-testing installation guide](docs/LIVE_TESTING.md)
and [compose.server.yaml](compose.server.yaml). It pulls the published Docker Hub
image and keeps configuration, staging and backups in persistent volumes.

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
Retrieve your one-time setup code with `docker compose exec reelscarey reelscarey
setup-token`, then create the administrator account in the browser. The code is
also in `/config/setup-token` and is removed after setup. No default password is
created. Go to Settings, enter your TMDb **API Read Access Token**, and save/test
the connection. Search for a movie and add it to the library. Existing config
volumes upgrade automatically; the first login upgrade requires account setup.
Add direct IPTorrents/XSpeeds, public NYAA, private RSS or Torznab connections in Indexers, then connect qBittorrent in Settings. In Movies, choose
Find releases, search, then select Download. Activity tracks download/import
progress and offers recovery actions. See [movie workflow setup](docs/MOVIE_WORKFLOW.md).

For TV, choose TV shows in Search, add a show, then open Episodes in
the TV library. Load each season's episodes from TMDb as needed and choose which
ones to monitor. Pausing a show or season preserves individual selections.
TV catalog features do not require Torznab or access to media files. Once an
indexer is connected, find episode/season releases, then review the completed
files in Activity before confirming their import. The original torrent sources
remain for seeding. See [TV operations](docs/TELEVISION.md) and
[TV downloads/imports](docs/TV_DOWNLOADS.md). Existing movie integrations are retained.

Open Automation to choose the search interval, quality profiles and per-library
item preferences. Search-only mode saves ranked matches and rejection reasons;
download mode queues missing items using the existing safe download/import pipeline.
TV files still require mapping review. Existing media below the resolution cutoff
can produce upgrade suggestions, but is never replaced. Torznab can be connected
later: profiles and schedule preferences work now, and scheduled searches wait
without making upstream requests. See [automation operations](docs/AUTOMATION.md).

Open Subtitles to connect your OpenSubtitles.com account and application API key
privately. English full, signs/songs and forced tracks are separate defaults;
automatic downloads start paused. Existing embedded tracks and sidecars are
checked before provider requests. Automatic matching requires confirmed catalog
identity plus a file-hash or exact original-release match; uncertain timing needs
review. No existing file is replaced. This feature works independently of Torznab.
See [subtitle setup and safety limits](docs/SUBTITLES.md).

The named `reelscarey_config` volume persists the database under `/config`.
The private `reelscarey_backups` volume stores managed snapshots under `/backups`.
Change `MEDIA_PATH` and `DOWNLOADS_PATH` in `.env` for your host folders; they map
to `/media` (writable for imports) and `/downloads` (read-only to preserve torrent
sources). The host download folder must contain the same files as qBittorrent's
configured download root. The sample mounts are separate and `/downloads` is
read-only, so imports use a verified staged copy. Hardlink imports require one
writable mounted filesystem shared by both roots: mount a single parent such as
`${DATA_PATH}:/data` and set `REELSCAREY_MEDIA_DIR=/data/media` and
`REELSCAREY_DOWNLOADS_DIR=/data/downloads`. The first-run setup asks whether to
prefer hardlinks or always copy, and **Settings → General → File imports** can
change it later. Symbolic-link imports are not supported because removing a
seeding source would break the library file. This source-build Compose runs
as UID/GID 10001; if you replace the config volume with a bind mount, give that
user read/write access. `docker compose down` keeps the database; adding `-v`
deletes the named volume and its data.

## Windows Docker host

On Windows with Docker Engine installed inside WSL rather than Docker Desktop,
run `./scripts/start-docker-wsl.ps1` after building the image. It starts the one
Compose service and keeps its WSL host alive in a hidden background process.
Pass `-Distribution NAME` for a different Docker-equipped distribution. Re-run
after a Windows reboot; the launcher does not install a Windows startup task.
Docker Desktop and continuously running Linux Docker hosts do not need it.

## Local development

Requires Go 1.27.3+ and Node 24.12+ (24.x). Build the frontend before compiling Go,
because frontend assets are embedded in the executable. These commands are for
development; Docker is the sole supported deployment target.

```sh
cd web
npm ci
npm run build
cd ..
go test ./...
go build -o bin/reelscarey ./cmd/reelscarey
./bin/reelscarey
```

Open [the local app](http://127.0.0.1:8787).
Read `data/config/setup-token` (or run `reelscarey setup-token` with the same
config environment) to create the administrator, then connect TMDb in Settings.
To iterate on UI, keep Go running and run `npm run dev` in `web`; Vite proxies
the API to port 8787. Rebuild UI and Go for changes to the embedded production UI.

Native imports also require `ffprobe` on PATH. Media/download directories must
already exist, remain separate from config and each other, and be accessible to
the application. Hardlink imports additionally need both roots on one writable
mounted filesystem; otherwise ReelScarey writes a verified copy. Mount a remote
qBittorrent's downloads locally before importing.

## Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `REELSCAREY_LISTEN_ADDR` | `127.0.0.1:8787` | HTTP bind address |
| `REELSCAREY_CONFIG_DIR` | `./data/config` | SQLite directory (created if absent) |
| `REELSCAREY_MEDIA_DIR` | `/media` | Media root |
| `REELSCAREY_DOWNLOADS_DIR` | `/downloads` | Download root |
| `REELSCAREY_BACKUPS_DIR` | sibling `backups` beside config; `/backups` in Docker | Separate private snapshot directory; parent must exist |
| `REELSCAREY_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error` |
| `REELSCAREY_PUBLIC_URL` | empty | Explicit browser origin for another hostname or HTTPS reverse proxy |

Only loopback Host names are accepted by default. For remote use, terminate TLS
at a reverse proxy, forward the original Host, and set `REELSCAREY_PUBLIC_URL`
to the exact browser origin (for example `https://media.example.com`). Cookies
then use Secure. Arbitrary forwarded headers are not trusted. Authentication is
implemented; a full production security review remains on the roadmap.

Relative paths resolve against the working directory. Docker explicitly sets
the paths to `/config`, `/media`, `/downloads`, and `/backups`. UI preferences are stored
in SQLite and survive process restarts. Folder preferences control the next
import's destination; saving a preference does not create or scan folders.
Structured JSON logs go to standard output.

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

The application, embedded web interface, FFmpeg/FFprobe and pinned YouTube tool
run in a single non-root container. Runtime images exclude Node, Go, pip and
standalone packaging tools. Interface pages load on demand; an old browser tab
offers a reload action if a page chunk changed during an upgrade. For a named
build, set `REELSCAREY_VERSION` in `.env` before `docker compose build`.

Open **Backups** to create live snapshots, configure schedules and retention,
download/upload ZIPs and review a restore. A restore saves a verified safety
backup, closes active work, restarts the app and holds workers for review.
Schedules start paused. Manual, uploaded and safety backups remain until deleted;
scheduled backups follow the configured count/age limits. Media files and host
configuration need their own backups. See the [backup manager guide](docs/BACKUPS.md).

For offline recovery, stop ReelScarey, then use `reelscarey backup NEW_FOLDER` and
`reelscarey backup-check BACKUP_FOLDER` to create and verify a consistent config
snapshot. `reelscarey restore BACKUP_FOLDER NEW_CONFIG_FOLDER` refuses existing
folders, revokes sessions and holds background workers for review. See the full
[installation and recovery guide](docs/OPERATIONS.md) before using these commands.
Keep SQLite on local storage. Back up before upgrades; in-place downgrades are
not supported. Never run two instances against the same config.

Include **secrets.key** in backups. It encrypts saved provider credentials; a
database backup without the matching key cannot decrypt them. Restrict access
to the entire config backup because it contains both the key and encrypted data.
Lost administrator password? See the local stdin-based recovery procedure in
[authentication operations](docs/AUTHENTICATION.md); it revokes all sessions.

## Project references

- [Architecture and entity model](ARCHITECTURE.md)
- [Roadmap and acceptance criteria](ROADMAP.md)
- [Request accounts, import lists and Discord notifications](docs/REQUESTS.md)
- [REST API](docs/API.md)
- [Authentication and secret operations](docs/AUTHENTICATION.md)
- [Contributor rules](AGENTS.md)
- [Validation evidence](docs/VALIDATION.md)

See [movie workflow operations](docs/MOVIE_WORKFLOW.md) for integration setup,
path mapping, recovery, import guarantees and current limitations. Scheduled
searches and quality profiles are documented in [automation](docs/AUTOMATION.md);
existing-media upgrades remain suggestions. Setup and System checks guide local
installation; Torznab remains optional. See [security boundaries](docs/SECURITY.md)
for the threat model and remaining public-release gates.

## License

License selection is pending the repository owner's decision. No open-source
license is granted by this initial scaffold. Dependencies retain their own
licenses. This is an independent implementation; no code was copied from the
four applications that inspired its scope.

See [indexers and title details](docs/INDEXERS_DETAILS.md) for direct tracker setup, cached cast/credits/trailers and local file/release history.

Import existing movies and TV videos through **Import library**: review canonical matches and register verified files in their current locations. See [existing-library import](docs/EXISTING_LIBRARY.md) for mounting, limits and safety checks.

## Version 0.22 follow-through

All seven follow-through features are implemented: scheduled metadata refresh
and richer calendar facts, reviewed refresh of linked AniList identities,
per-show subtitle preferences, reviewed subtitle timing/replacement/rollback,
YouTube subscriptions and bulk acquisition, source/codec scoring with Usenet age
filters, and reviewed bulk metadata matching. See [feature behavior and bounds](docs/SEVEN_FEATURES.md).
Provider credentials, private-service acceptance and the license still require
owner decisions or access where listed in the remaining-work record.
## Version 0.23 storage and reliability

The Storage page browses media/download folders and up to eight explicitly
mounted read-only roots. A guided offline CLI reviews and copies the main media
tree, creates a verified configuration backup, retains originals, refuses
replacement and requires activation of the new mount before workers resume.
See [storage behavior and commands](docs/STORAGE.md). Standard TV matching now
also accepts explicit 1x02, spaced S01 E02 and Season 1 Episode 2 labels while
rejecting ambiguous or multi-episode automatic selections. No database migration
or runtime dependency is added. [Workload checks](docs/WORKLOAD.md) define the
repeatable constrained container budget and its limits.
Named qBittorrent, SABnzbd and NZBGet connections are available under
**Settings → Manage download clients**. New connections start paused; choose
explicit defaults after reviewing folder mappings. See
[Download clients](docs/DOWNLOAD_CLIENTS.md) for setup and safety limits.

Version 0.26 also provides named Transmission 4 clients, Kitsu Anime identities,
SubDL subtitles with manual review and subtitle alignment from a reference file.
The previous subtitle version is retained for rollback. These features share the
single application container and existing configuration/library backup interface.
See [remaining scope](docs/REMAINING.md) and [release process](docs/RELEASING.md).
