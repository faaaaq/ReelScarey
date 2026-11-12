# Docker Compose installation

Use Docker Engine with Compose v2 on Linux x86-64. Copy
[compose.yaml](../compose.yaml) and [.env.example](../.env.example) (renamed to
`.env`) into an empty folder, then set three folders and one URL:

```sh
CONFIG_PATH=/srv/reelscarey/config
MEDIA_PATH=/srv/reelscarey/media
DOWNLOADS_PATH=/srv/reelscarey/downloads
REELSCAREY_PUBLIC_URL=https://reelscarey.example.com
PUID=1000
PGID=1000
TZ=America/Chicago
```

Then:

```sh
docker compose up -d
```

That is the whole mapping. ReelScarey reads `/config`, `/media` and `/downloads`;
backups and YouTube staging are private volumes created for you.

## The three folders

| Mount | Container path | Content | Access |
| --- | --- | --- | --- |
| `CONFIG_PATH` | `/config` | Database, account, encrypted credentials, encryption key | writable, private |
| `MEDIA_PATH` | `/media` | Your library, including any `movies`, `tvshows`, `anime`, `music` folders | writable for imports |
| `DOWNLOADS_PATH` | `/downloads` | The download client's completed folder | writable, but only inside ReelScarey's own `builtin` folders |

`/downloads` must contain the same files as the download client's completed
folder. It is mounted writable because the bundled built-in torrent and Usenet
clients write their own folders there; ReelScarey never modifies an existing
download, and imports publish a verified copy into the library. If you use only
an external download client, add `:ro` to that mount to make the whole folder
read-only. Two Docker volumes (`backups`,
`youtube_staging`) hold managed snapshots and reviewed YouTube staging and need
no host path.

If a folder cannot be mounted at those paths, mount it wherever you like and set
`REELSCAREY_MEDIA_DIR`, `REELSCAREY_DOWNLOADS_DIR`, `REELSCAREY_CONFIG_DIR`,
`REELSCAREY_BACKUPS_DIR` or `REELSCAREY_YOUTUBE_STAGING_DIR` to match. Those
variables change *where ReelScarey looks*, not *where you copy files*; the folder
must still be mounted into the container. `REELSCAREY_DOWNLOADS_DIR` is the
folder where your download client finishes files, read for import; it is not a
ReelScarey download destination. The paths must be absolute, and the
config, backup and staging folders must stay separate from each other and from
the media and download folders.

## Your reverse proxy

To put ReelScarey behind Nginx Proxy Manager or another proxy, remove the
published `ports` and attach the service to the proxy network:

```yaml
    networks:
      - npm
# ...
networks:
  npm:
    external: true
```

In Nginx Proxy Manager, create a Proxy Host forwarding to **http**,
**reelscarey**, port **8787**, and enable HTTPS for your domain. Set
`REELSCAREY_PUBLIC_URL` to that exact HTTPS address (no path). No host port needs
to be published. The proxy must preserve the original Host. This explicit URL
enables secure cookies and rejects unexpected browser origins.

For direct LAN access instead, keep `ports` and use
`REELSCAREY_PUBLIC_URL=http://YOUR-SERVER-IP:8787`.

## Setup code

Retrieve the one-time setup code and create your administrator in the browser:

```sh
docker compose exec reelscarey reelscarey setup-token
```

The code is also in the host config folder at `setup-token`; it is removed after
setup. It is never printed in normal logs. No default password is created.

## Permissions

`PUID` and `PGID` select the Linux account that can access your library (find its
IDs with `id`). The container initializes ownership only in its private config,
backup and staging folders, then drops root before running the application. Media
and download ownership and permissions are never changed. Grant the selected
account access to your existing library folders. Download files themselves are
only read; the built-in clients create their own subfolders.

When you set an explicit `user:` instead, PUID/PGID must match it or be omitted;
automatic permission initialization is disabled. No application process runs as
root.

## Library folders

Mount each existing library folder **inside** `/media` and then choose it in the
web interface:

```yaml
    volumes:
      - ${MEDIA_PATH:-./media}:/media
      - /mnt/movies:/media/movies
      - /mnt/tv:/media/tvshows
      - /mnt/anime:/media/anime
      - /mnt/music:/media/music
```

In **Organize library**, set the TV folder to `tvshows` and the Anime folder to
`anime` (paths relative to `/media`, case-sensitive). The Anime scan finds series
directly under `anime`; include anime films in the Movies scan. The Movies
collection stays available even without a movie folder.

For existing music, mount each host folder below `/media`. In **Settings → Media
Management → Library roots**, Music defaults to the exact `Music` folder beside
the video folders, or to an existing lowercase `music` folder when that is the
only match. A missing folder is shown as unavailable, never guessed. Add extra
Music roots to scan several directories. Video collections keep their existing
organization folders as default roots; adding scan roots does not change
destination folders or naming. Add the matching artists and open each album once
so tracks are loaded before registering files in **Music → Library Import**.

Keep the Compose project/folder name stable. Do not use `docker compose down -v`
when keeping your installation: it deletes the config volume. Configuration
backups contain the database and key, not media; back those up separately.

## Hardlink imports

Linux hardlinks require one writable mounted filesystem containing both the
library and the download folder, so the default separate mounts always copy.
To allow hardlinks, mount a single parent and point both roots inside it:

```yaml
    environment:
      REELSCAREY_MEDIA_DIR: /data/media
      REELSCAREY_DOWNLOADS_DIR: /data/downloads
    volumes:
      - ${DATA_PATH:-./data}:/data
```

The first-run setup asks which method to prefer, and
**Settings → General → File imports** can change it later. Symbolic-link imports
are not supported.

## Updates and existing installations

Create and download a verified backup in **Backups** before updating. Then:

```sh
docker compose pull
docker compose up -d
docker compose exec reelscarey reelscarey doctor
```

`:latest` moves with each successful release; pin `REELSCAREY_IMAGE` to a version
tag or digest for production rollback control.

Installations created with an older template that used `/config/state`,
`/data`, or `REELSCAREY_CONFIG_DIR` keep working unchanged: keep their current
volume mappings and variables. Those templates are no longer shipped, so compare
your `compose.yaml` with this one when you next edit it, and move a folder only
while the container is stopped.
