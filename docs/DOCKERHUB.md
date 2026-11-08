# ReelScarey

**One container to manage your movie, TV and Anime library** — metadata, monitoring,
indexers, download clients, verified imports, subtitles, requests and backups.

ReelScarey is a self-hosted **acquisition and library manager** in the spirit of
Sonarr/Radarr. It decides what to look for, talks to your indexers and download
clients, verifies completed files with FFprobe, and imports them without touching
your seeding sources.

> **It is not a media server or player.** ReelScarey does not stream or transcode.
> Point your existing player at the folders it manages.

---

## Features

### Library and metadata

- Movies, TV shows and a separate Anime catalog, backed by TMDb metadata.
  Anime shows support optional English-dub-only release filtering with an
  editable rule.
- Canonical seasons and episodes, on-demand metadata refresh and revision-checked
  monitoring.
- Poster and table browsing with literal search, status/monitoring filters,
  stable sorting and bookmarkable views.
- Wanted and a monthly release calendar built from cached dates and tracked jobs.
- Existing-library discovery and reviewed in-place registration; no files are
  moved during registration.
- Movie versions (HD/4K, editions), collection and franchise links, cast/crew,
  trailers and title details.
- Music: MusicBrainz catalog, existing-file scan and reviewed registration,
  reviewed organization and verified tag writing with rollback, audio release
  search, automatic client queueing, a leased download worker, audio
  preferences, wanted albums and a search schedule.

### Search and acquisition

- Torznab and Newznab indexers, plus direct IPTorrents, XSpeeds, NYAA, private RSS
  and optional Prowlarr/NZBGeek presets.
- qBittorrent (including a supervised built-in client), Transmission 4, SABnzbd
  and NZBGet, with named clients, per-protocol defaults and encrypted credentials.
- Conservative release matching and ranking: title/year identity, resolution,
  source, codec, seeders, language and per-show release rules.
- Manual torrent uploads (`.torrent`) and magnet links through the normal
  reservation and import pipeline.
- Optional Mullvad WireGuard tunnel for the built-in torrent client only, with a
  separate kill-switch choice.

### Imports and organization

- FFprobe verification, SHA-256 evidence and journaled imports that resume after
  a restart.
- Linux hardlinks where possible; verified staged copies otherwise.
- No overwrite: publication is exclusive and existing files are refused.
- Sources stay in place for seeding; torrents are never deleted by an import.
- Reviewed renaming and organization with Movies/TV/Anime/custom collections,
  Plex/Jellyfin/Emby naming presets and custom tokens.
- Absolute Anime episode aliases, provider-order mappings and reviewed packs.
- Versioned replacements with retained previous versions and rollback.
- Reviewed catalog removal that keeps files or moves them to retained trash.

### Subtitles

- OpenSubtitles.com and SubDL providers with encrypted credentials.
- SRT, WebVTT, ASS and SSA output; styled originals keep their formatting.
- Embedded and sidecar detection before any provider request.
- Automatic matching requires catalog identity plus a file hash or exact
  original-release evidence; uncertain timing stays in review.
- Per-show language/track overrides, reviewed timing alignment and rollback.

### Automation

- Persistent scheduled searches with leases, bounded retries and restart recovery.
- Search-only or download modes; automation starts paused.
- Editable quality/language profiles and per-title assignment.
- Conservative automatic queueing with policy/monitoring fences inside the
  reservation transaction.
- Resolution upgrade suggestions that never replace an existing file.

### Requests

- Separate request accounts with hashed sessions and a request-only interface.
- TMDb search and approval workflow; approval adds the title with monitoring.
- Import lists: TMDb, Trakt, IMDb via MDBList, private lists with user tokens,
  IMDb CSV, and **Trakt watchlists** (public profile or user token).
- Optional Discord webhook notifications and a companion bot with `/request`
  and `/releases` commands. Verify your own Discord setup before relying on it.

### Operations

- Live configuration/database backups, schedules, retention, ZIP upload/download
  and a verified restore that pauses workers for review.
- Storage browser for explicitly mounted read-only roots.
- Registered-file health checks and repair/replacement reviews.
- Activity queue, durable history, dashboard and system health checks.
- Optional read-only Emby activity snapshots (what is being watched).

---

## Quick start

Save this as `compose.yaml`, adjust the paths and URL, then run
`docker compose up -d`.

```yaml
services:
  reelscarey:
    image: faaaaq/reelscarey:latest
    pull_policy: always
    container_name: reelscarey
    environment:
      PUID: 1000
      PGID: 1000
      TZ: America/Chicago
      # Must be the exact URL you open in the browser (no path).
      REELSCAREY_PUBLIC_URL: http://192.168.1.20:8787
      REELSCAREY_CONFIG_DIR: /config/state
      REELSCAREY_MEDIA_DIR: /data
    ports:
      - "8787:8787"
    volumes:
      - /srv/reelscarey/config:/config
      - /srv/reelscarey/media:/data
      - /srv/reelscarey/downloads:/downloads:ro
    restart: unless-stopped
```

Behind an existing reverse proxy, remove `ports`, join the proxy network, and set
`REELSCAREY_PUBLIC_URL` to the HTTPS address. The proxy must preserve the original
Host header.

---

## First-run setup

1. **Create the folders.** `config` must be private and writable by `PUID:PGID`.
   `media` is writable for imports. `downloads` is read-only so seeding sources are
   never modified.
2. **Start the container.**
   ```sh
   docker compose up -d
   ```
3. **Get the one-time setup code.**
   ```sh
   docker compose exec reelscarey reelscarey setup-token
   ```
   It is also in `config/state/setup-token` and is removed after setup. No default
   password exists.
4. **Open the URL** from `REELSCAREY_PUBLIC_URL` and create the administrator.
5. **Connect TMDb.** Settings → Primary Connections → enter your TMDb **API Read
   Access Token**, then test and save.
6. **Add an indexer** (Indexers) and a **download client** (Settings → Download
   Clients). The built-in qBittorrent client is provisioned automatically on images
   that ship it.
7. **Add a title**, search releases, and follow the import in **Activity**.
8. **Back up early.** Open **Backups** and create a snapshot. Keep `secrets.key`
   with it; without the key, encrypted provider credentials cannot be recovered.

---

## Volumes

| Container path | Purpose |
| --- | --- |
| `/config` | Database, account, encryption key, setup token |
| `/config/state` | Config subdirectory used by the simple template |
| `/config/backups` | Managed backup archives and restore safety copies |
| `/config/youtube-staging/work` | Reviewed acquisition staging |
| `/data` | Media root (writable for imports) |
| `/downloads` | Completed downloads (mount read-only) |
| `/backups` | Separate backup volume in the server template |

Keep the Compose project name and volume mappings stable. Do not run two
instances against the same config directory.

---

## Environment

| Variable | Default | Meaning |
| --- | --- | --- |
| `PUID` / `PGID` | `1000` | Host account that owns library access |
| `TZ` | container default | Timezone, e.g. `America/Chicago` |
| `REELSCAREY_PUBLIC_URL` | empty | Exact browser origin; required for remote/HTTPS use |
| `REELSCAREY_CONFIG_DIR` | `./data/config` | SQLite and key directory |
| `REELSCAREY_MEDIA_DIR` | `/media` | Media root |
| `REELSCAREY_DOWNLOADS_DIR` | `/downloads` | Download root |
| `REELSCAREY_BACKUPS_DIR` | sibling `backups` | Private snapshot directory |
| `REELSCAREY_LISTEN_ADDR` | `0.0.0.0:8787` in the image | HTTP bind address |
| `REELSCAREY_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `REELSCAREY_BROWSE_ROOTS` | empty | Optional read-only storage roots (JSON) |
| `REELSCAREY_KEEP_NET_ADMIN` | empty | Opt in to `NET_ADMIN` for the Mullvad kill switch |
| `DOWNLOADS_READ_ONLY` | `true` | Set `false` only for the built-in client's writable folder |

---

## Updating

Create and download a backup first, then:

```sh
docker compose pull
docker compose up -d
docker compose exec reelscarey reelscarey doctor
```

The templates set `pull_policy: always`, so `docker compose up -d` also re-checks
`:latest`. Pin a version tag or digest if you need rollback control. In-place
downgrades are not supported; restore a backup instead.

---

## Image tags

| Tag | Meaning |
| --- | --- |
| `latest` | Newest successful primary-branch build or release; moves on each publication |
| `0.x.y` | Immutable application version |
| `sha-<commit>` | Exact source commit |

Images are built for **linux/amd64**.

---

## Requirements and limits

- Docker Engine with Compose v2 on Linux x86-64. Docker is the only supported
  deployment target.
- One container, one SQLite database, no Redis or external database.
- SQLite must live on local storage, not a network filesystem.
- FFmpeg/FFprobe and the built-in qBittorrent client are bundled.
- A Usenet server and any external download client remain owner-supplied.
- **No playback, streaming, transcoding, HLS/DASH or media-server API.** Use your
  existing player for that.
- **Music is implemented end to end**, including mapping blocked downloads. No
  books.
- No live TV, DVR, trailers streaming or built-in reverse proxy/TLS.
- The Emby integration is read-only activity; it does not control playback.
- Discord/MDBList/provider acceptance depends on your own accounts and network.

---

## Security notes

- The app binds loopback by default outside the image; the container sets
  `0.0.0.0:8787` so a proxy or published port can reach it.
- Only loopback Hosts are accepted unless `REELSCAREY_PUBLIC_URL` names your exact
  browser origin. Use HTTPS at a reverse proxy and forward the original Host.
- Sessions are HttpOnly cookies with CSRF protection. Request accounts are denied
  administrator APIs by default.
- Provider credentials are encrypted with `secrets.key` using AES-256-GCM. Back up
  the key with the database and restrict access to the whole config backup.
- Back up before every upgrade. Never publish the config directory.

---

## Documentation

Full documentation lives in the repository:
<https://github.com/faaaaq/ReelScarey>

- Installation and upgrades — `docs/INSTALL.md`
- Movie workflow — `docs/MOVIE_WORKFLOW.md`
- TV downloads and imports — `docs/TV_DOWNLOADS.md`
- Anime and organization — `docs/ANIME_ORGANIZATION.md`
- Indexers and title details — `docs/INDEXERS_DETAILS.md`
- Download clients — `docs/DOWNLOAD_CLIENTS.md`
- Subtitles — `docs/SUBTITLES.md`
- Automation — `docs/AUTOMATION.md`
- Requests, lists and Discord — `docs/REQUESTS.md`
- Backups — `docs/BACKUPS.md`
- REST API — `docs/API.md`
- Architecture — `ARCHITECTURE.md`
- Roadmap — `ROADMAP.md`

---

## License

License selection is pending the repository owner's decision. No open-source
license is granted by this listing. Dependencies retain their own licenses.
