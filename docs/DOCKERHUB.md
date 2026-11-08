<!--
Docker Hub short description (100 character limit):
One container to manage movies, TV, anime and music: search, import, subtitles.
-->

# ReelScarey

**One container to manage your movie, TV, Anime and music library.**

ReelScarey is a self-hosted acquisition and library manager in the spirit of
Sonarr and Radarr. It decides what to look for, searches your indexers, sends
releases to your download client, verifies completed files with FFprobe, and
imports them into your naming scheme without touching the copies that are still
seeding.

> **ReelScarey is not a media server.** It does not play, stream or transcode.
> Point Plex, Jellyfin or Emby at the folders it manages.

| | |
| --- | --- |
| Image | `faaaaq/reelscarey:latest` |
| Web UI / API | `8787/tcp` |
| Platform | `linux/amd64` |
| Database | SQLite on local disk — no Redis, no external DB |
| Runtime | One container; FFmpeg/FFprobe and a supervised qBittorrent included, SABnzbd optional |

---

## What you get

### Library and metadata

- Movies, TV shows, a separate Anime catalog and a Music catalog, backed by
  TMDb and MusicBrainz.
- Canonical seasons and episodes, on-demand metadata refresh and
  revision-checked monitoring.
- Added, registered and imported titles automatically enter a durable metadata
  queue; each library page can queue a full refresh for its collection.
- New titles default to **future-only monitoring**, so adding an old show does
  not mark its entire back catalog for search.
- Poster and table browsing with literal search, status/monitoring filters,
  stable sorting and bookmarkable views.
- Wanted, a monthly release calendar and a live dashboard built from cached
  dates and tracked jobs.
- Existing-library discovery and reviewed in-place registration; registration
  never moves files.
- Movie versions (HD/4K, editions), collections and franchises, cast/crew,
  trailers and title details.
- Music: artist/album/track catalog, existing-file scan, reviewed registration
  and organization, verified tag writes with rollback, album search, queueing
  and a leased download worker.

### Search and acquisition

- Torznab and Newznab indexers, plus direct IPTorrents, XSpeeds, NYAA, private
  RSS and optional Prowlarr/NZBGeek presets.
- qBittorrent (including a supervised built-in client), Transmission 4, SABnzbd
  and NZBGet, with named clients, per-protocol defaults and encrypted
  credentials.
- Conservative release matching: title/year identity, resolution, source,
  codec, seeders, language and per-show release rules.
- **Release feed sync** (Sonarr/Radarr-style): every install starts with a feed
  check enabled every 15 minutes. It grabs monitored, missing titles from
  enabled feeds and leaves registered files untouched.
- Manual `.torrent` uploads and magnet links through the same reservation,
  submission and journaled import pipeline.
- Optional Mullvad WireGuard tunnel for the built-in torrent client, with a
  separate kill-switch choice.

### Imports and organization

- FFprobe verification, SHA-256 evidence and journaled imports that resume after
  a restart.
- Choose hardlink-when-possible or copy-only library imports; Linux hardlinks
  where possible, verified staged copies otherwise.
- **No overwrite.** Publication is exclusive and existing files are refused.
- Sources stay in place for seeding; an import never deletes a torrent.
- Reviewed renaming and organization with Movies/TV/Anime/custom collections,
  Plex/Jellyfin/Emby naming presets and custom tokens.
- Absolute Anime episode aliases, provider-order mappings and reviewed packs.
- Versioned replacements with retained previous versions and rollback.
- Reviewed catalog removal that keeps files or moves them to a retained trash.

### Subtitles

- OpenSubtitles.com and SubDL providers with encrypted credentials.
- SRT, WebVTT, ASS and SSA output; styled originals keep their formatting.
- Embedded and sidecar detection before any provider request.
- Automatic matching requires catalog identity plus a file hash or exact
  original-release evidence; uncertain timing stays in review.
- Per-show language/track overrides, reviewed timing alignment and rollback.
- Reference-text alignment only — this is not audio alignment, OCR or
  translation.

### Automation

- Persistent scheduled searches with leases, bounded retries and restart
  recovery.
- Search-only or download modes; scheduled searches start paused.
- Editable quality/language profiles and per-title assignment.
- Conservative automatic queueing with policy and monitoring fences inside the
  reservation transaction.
- Resolution upgrade suggestions that never replace an existing file.

### Requests and integrations

- Separate request accounts with hashed sessions and a request-only interface.
- TMDb search and an approval workflow; approval adds the title with monitoring.
- Import lists: TMDb, Trakt, IMDb via MDBList, private lists with user tokens,
  IMDb CSV and Trakt watchlists.
- Optional Discord webhook notifications and a companion bot with `/request`
  and `/releases` commands. Test with your own Discord server before relying on
  it.
- Optional read-only Emby activity snapshots (what is being watched).

### Operations

- Live configuration/database backups, schedules, retention, ZIP upload/download
  and a verified restore that pauses workers for review.
- Storage browser for explicitly mounted read-only roots.
- Registered-file health checks and repair/replacement reviews.
- Activity queue, durable history, dashboard and system health checks.
- `reelscarey doctor`, `healthz` and `readyz` endpoints for scripted checks.

---

## Quick start

### Before you begin

- A Linux x86-64 host with Docker Engine and Compose v2. Published images are
  **linux/amd64 only**. Docker is the only supported deployment target.
- Local disk for the config directory. Do not put SQLite on NFS, SMB or another
  network filesystem.
- A TMDb **API Read Access Token** (free from TMDb).
- Optional: indexers and a download client. The image includes a supervised
  qBittorrent client and an opt-in SABnzbd client.

### Example `compose.yaml`

Save this as `compose.yaml`, change the host paths, `PUID`/`PGID`, timezone and
public URL, then start it.

```yaml
services:
  reelscarey:
    image: faaaq/reelscarey:latest
    pull_policy: always
    container_name: reelscarey
    restart: unless-stopped
    init: true
    environment:
      # Host account that should own config files and access the library.
      PUID: 1000
      PGID: 1000
      TZ: America/Chicago

      # The exact URL you type in the browser: scheme + host + port, no path,
      # no trailing slash. Change this before exposing the app to your LAN.
      REELSCAREY_PUBLIC_URL: http://192.168.1.20:8787

      # Everything private lives under /config: database, account, secrets.key,
      # setup token, backups and YouTube staging.
      REELSCAREY_CONFIG_DIR: /config/state
      REELSCAREY_MEDIA_DIR: /media
      REELSCAREY_DOWNLOADS_DIR: /downloads

      # Optional shared TMDb Read Access Token. New installs work immediately
      # with this default; each owner can replace it in Settings. Leave empty
      # to require a token in Settings. Never commit a real token.
      REELSCAREY_TMDB_TOKEN: ""
    ports:
      - "8787:8787"
    volumes:
      - /srv/reelscarey/config:/config
      - /srv/reelscarey/media:/media
      # Read-only keeps torrent/usenet sources safe from accidental edits.
      - /srv/reelscarey/downloads:/downloads:ro
      # Separate mounts (and the read-only download root) force verified
      # copies. For hardlink imports, use one writable parent instead:
      #   - /srv/reelscarey/data:/data
      # and set REELSCAREY_MEDIA_DIR=/data/media plus
      # REELSCAREY_DOWNLOADS_DIR=/data/downloads above.
```

### Start and create the administrator

```sh
docker compose up -d
docker compose exec reelscarey reelscarey setup-token
```

Open the URL from `REELSCAREY_PUBLIC_URL`, paste the one-time setup code and
create the administrator. The form also asks whether to prefer hardlinks or
always copy completed downloads; the choice is available later under
**Settings → General → File imports**. The code is also stored at
`/config/state/setup-token` and is removed after setup. **No default password
exists.**

### Shared TMDb token

Set `REELSCAREY_TMDB_TOKEN` to a TMDb API Read Access Token to make metadata
work immediately after setup. It is never shown in the interface or logs. The
Settings page labels it **Shared default**; saving another token there replaces
it for that installation, and **Use shared default** removes the saved token and
falls back to the deployment value. Leave the variable empty to require each
installation to save its own token.

### First-run checklist

1. **Settings → Primary Connections**: enter your TMDb API Read Access Token,
   then test and save.
2. **Indexers**: add Torznab/Newznab, a direct tracker or a private RSS feed.
3. **Download clients**: add an external client, or configure the bundled
   qBittorrent and optional SABnzbd clients and choose protocol defaults.
4. **Organize library**: set your Movies/TV/Anime folders and naming presets.
5. **Settings → General → File imports**: confirm hardlink-when-possible or
   always-copy for completed downloads. Separate or read-only download mounts
   make imports copy in practice.
6. **Add a title**, search releases, and follow the download and import in
   **Activity**.
7. **Backups**: create a snapshot immediately. Keep `secrets.key` with it.

---

## Reverse proxy and HTTPS

ReelScarey has no built-in TLS or reverse proxy. When a proxy is in front of it:

- Remove the `ports:` block and attach the service to the proxy network.
- Set `REELSCAREY_PUBLIC_URL` to the exact HTTPS origin, for example
  `https://media.example.com`.
- Make the proxy preserve the original `Host` header.
- Do not append a path to the public URL.

The app rejects unexpected browser origins. Only loopback Hosts are accepted by
default; setting `REELSCAREY_PUBLIC_URL` is what authorizes your real hostname.
Use HTTPS at the proxy so session cookies are marked Secure.

---

## Volumes

| Container path | Purpose |
| --- | --- |
| `/config` | Parent for private state; keep this on local disk |
| `/config/state` | Database, administrator account, sessions, `secrets.key`, setup token |
| `/config/backups` | Managed backup archives and restore safety copies |
| `/config/youtube-staging/work` | Reviewed YouTube acquisition staging |
| `/media` | Media root, writable for reviewed imports |
| `/downloads` | Completed downloads; mount read-only unless a built-in client must write |
| `/backups` | Alternative separate backup volume for hardened layouts |
| `/youtube-staging` | Alternative separate staging volume for hardened layouts |

Keep the Compose project name and volume mappings stable. Never run two
instances against the same config directory.

---

## Environment variables

| Variable | Default | Meaning |
| --- | --- | --- |
| `PUID` / `PGID` | `10001` | UID/GID the app runs as. Set to your host user, for example `1000`. Only private config, backup and staging folders are chowned; media and download ownership is never changed. |
| `TZ` | container default (UTC) | Timezone for schedules and displayed times, for example `America/Chicago`. |
| `REELSCAREY_PUBLIC_URL` | empty | Exact browser origin. Required for any host other than loopback. No path, query or credentials. |
| `REELSCAREY_CONFIG_DIR` | `/config` | SQLite and key directory. The examples use `/config/state`. |
| `REELSCAREY_MEDIA_DIR` | `/media` | Writable media root. |
| `REELSCAREY_DOWNLOADS_DIR` | `/downloads` | Completed download root. |
| `REELSCAREY_BACKUPS_DIR` | sibling `backups` | Private snapshots. With `/config/state` this is `/config/backups`. |
| `REELSCAREY_YOUTUBE_STAGING_DIR` | sibling `youtube-staging/work` | YouTube review staging. |
| `REELSCAREY_LISTEN_ADDR` | `0.0.0.0:8787` in the image | HTTP bind address. |
| `REELSCAREY_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `REELSCAREY_BROWSE_ROOTS` | empty | JSON map of up to eight explicit read-only browse roots. |
| `REELSCAREY_KEEP_NET_ADMIN` | unset | Keep `NET_ADMIN` for the optional Mullvad kill switch; also requires `cap_add: [NET_ADMIN]`. |

Notes:

- `REELSCAREY_YTDLP_PATH` is configured inside the image. Do not override it.
- `DOWNLOADS_READ_ONLY` is **not** read by the application. It is a variable
  used by the repository's `compose.server.yaml` to toggle the read-only bind
  mount.

---

## Optional: built-in download clients

The image bundles clients that ReelScarey can supervise itself. External
qBittorrent, Transmission, SABnzbd and NZBGet connections remain fully
supported.

- **qBittorrent** is provisioned automatically on images that include it and
  acts as the torrent fallback when no default client is chosen.
- **SABnzbd** is opt-in. It starts only after you save news-server credentials
  on the **Usenet** page and is never auto-enabled. A Usenet server is always
  owner-supplied.

Both write into dedicated folders under the download root. The quick-start
example mounts `/downloads` read-only, which is the safe default for imports
but blocks the built-in clients from downloading. To use them, create a
dedicated folder owned by `PUID:PGID` and make the download mount writable; the
built-in clients keep their own dedicated subfolders. Imports still read only
completed files and never delete seeding sources.

### Optional: Mullvad WireGuard

The VPN tunnel covers the built-in torrent client only. Add the capability
under `services.reelscarey`:

```yaml
    cap_add:
      - NET_ADMIN
```

and add this to the existing `environment:` block:

```yaml
      REELSCAREY_KEEP_NET_ADMIN: "1"
```

Then upload the Mullvad WireGuard configuration on **Download clients**. A
`/dev/net/tun` device is not needed. The default remains capability-free; do
not add `NET_ADMIN` unless you use this feature.

---

## Updating

Create and download a verified backup in **Backups** first, then:

```sh
docker compose pull
docker compose up -d
docker compose exec reelscarey reelscarey doctor
```

The example sets `pull_policy: always`, so `docker compose up -d` also re-checks
`:latest`. In-place downgrades are not supported; restore a backup instead.

---

## Image tags

| Tag | Meaning |
| --- | --- |
| `latest` | Newest successful primary-branch build or release; moves on each publication |
| `0.40.0` | Immutable application version |
| `sha-<commit>` | Exact source commit |

Published images target **linux/amd64**. Pin a version tag or digest if you
need reproducible deploys and controlled rollback.

---

## Requirements and current limits

- Docker Engine with Compose v2 on Linux x86-64. Docker is the only supported
  deployment target.
- One container, one SQLite database. No Redis, no external database.
- SQLite must live on local storage, not a network filesystem.
- FFmpeg/FFprobe, qBittorrent-nox and SABnzbd are bundled; SABnzbd stays
  opt-in. A Usenet server and any external indexer/client accounts are
  owner-supplied.
- **No playback, streaming, transcoding, HLS/DASH or media-server API.** Use
  your existing player for that.
- Music is implemented end to end. There are no books or audiobooks.
- No live TV, DVR, trailer streaming or built-in reverse proxy/TLS.
- Emby integration is read-only activity; it does not control playback.
- Discord, Trakt, MDBList, indexers and subtitle providers depend on your own
  accounts and network.
- This is pre-1.0 software. Read `ROADMAP.md`, back up before upgrades and
  verify provider behavior in your own environment.

---

## Security notes

- No default password. The setup token is one-time and removed after use.
- Sessions are HttpOnly cookies with CSRF and same-origin validation.
- Request accounts are denied administrator APIs by default.
- Provider credentials are encrypted with `secrets.key` using AES-256-GCM.
  Back up the key with the database; without it, saved credentials cannot be
  recovered.
- The config directory contains the key, the database and session data. Treat
  the whole directory as a secret and never publish it.
- Run behind HTTPS and keep the image updated.

---

## Troubleshooting

**Login fails or the UI returns 403 / "host not allowed"**
`REELSCAREY_PUBLIC_URL` must exactly match the origin in the address bar,
including scheme and port, with no path and no trailing slash. Restart the
container after changing it.

**I lost the setup code**
Run `docker compose exec reelscarey reelscarey setup-token` before the
administrator account exists. It is also at `/config/state/setup-token` until
setup completes. After setup, use `reset-password` instead.

**Permission denied when reading or importing media**
Set `PUID`/`PGID` to the owner of your library. ReelScarey never changes media
or download ownership. Fix ownership on the host, then retry.

**The built-in client cannot write**
The download mount is read-only. Use a dedicated writable folder for the
built-in client, or remove `:ro` from the mount.

**Credentials stopped working after a restore**
Restore `secrets.key` together with the database. A database without its
matching key fails closed rather than silently resetting.

**The container is unhealthy**
Check `docker compose logs reelscarey`, run
`docker compose exec reelscarey reelscarey doctor`, and probe `/healthz` and
`/readyz`.

---

## Documentation and support

Repository and issue tracker: <https://github.com/faaaaq/ReelScarey>

The full documentation lives in the repository:

| Topic | File |
| --- | --- |
| Installation and upgrades | `docs/INSTALL.md` |
| Movie workflow | `docs/MOVIE_WORKFLOW.md` |
| TV downloads and imports | `docs/TV_DOWNLOADS.md` |
| Anime organization | `docs/ANIME_ORGANIZATION.md` |
| Indexers and title details | `docs/INDEXERS_DETAILS.md` |
| Download clients | `docs/DOWNLOAD_CLIENTS.md` |
| Usenet | `docs/USENET.md` |
| Subtitles | `docs/SUBTITLES.md` |
| Automation and release feed | `docs/AUTOMATION.md` |
| Requests, lists and Discord | `docs/REQUESTS.md` |
| Backups | `docs/BACKUPS.md` |
| REST API | `docs/API.md` |
| Architecture | `ARCHITECTURE.md` |
| Roadmap | `ROADMAP.md` |

---

## License

License selection is pending the repository owner's decision. No open-source
license is granted by this listing. Dependencies retain their own licenses.
