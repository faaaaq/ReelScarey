# Docker Compose installation

Use Docker Engine with Compose v2 on Linux x86-64. Save
[compose.simple.yaml](../compose.simple.yaml) as `compose.yaml`, adjust your host
folders and `REELSCAREY_PUBLIC_URL`, and run:

```sh
docker compose up -d
```

No separate environment file or source build is needed. If Docker Hub requires
authentication, run `docker login` with an account that can pull
`faaaaq/reelscarey`. Version 0.26.2 introduces this layout and PUID/PGID support.

## Your reverse proxy

The example joins your existing `npm` network. In Nginx Proxy Manager, create a
Proxy Host forwarding to **http**, **reelscarey**, port **8787**, and enable HTTPS
for your domain. Set `REELSCAREY_PUBLIC_URL` to that exact HTTPS address (no path).
No host port needs to be published. The proxy must preserve the original Host.
This explicit URL enables secure cookies and rejects unexpected browser origins.

For direct LAN access instead, remove the `networks` sections, add
`ports: ["8787:8787"]` to the service, and use
`REELSCAREY_PUBLIC_URL: http://YOUR-SERVER-IP:8787`.

Retrieve the one-time setup code and create your administrator in the browser:

```sh
docker compose exec reelscarey reelscarey setup-token
```

The code is also in the host config folder at `state/setup-token`; it is removed
after setup. It is never printed in normal logs. No default password is created.

## Folders and permissions

`PUID` and `PGID` select the Linux account that can access your library (find its
IDs with `id`). The container initializes permissions only in its private state,
backup and staging folders, then drops root before running the application.
Media and download ownership/permissions are never changed. Grant the selected
account access to your existing Anime and TV folders. Downloads are read-only;
seeding sources remain untouched. `TZ` accepts a timezone such as America/Chicago.

The example uses your exact host paths, including `/mnt/masstorage`; check that
spelling against your server before starting Docker.

| Container location | Persistent content |
| --- | --- |
| `/config/state` | Database, account, encrypted credentials and encryption key |
| `/config/backups` | Scheduled/manual backup archives and restore safety copies |
| `/config/youtube-staging/work` | Reviewed acquisition staging |
| `/data` (named volume) | Retained versions and library recovery files |
| `/data/anime` | Your Anime bind mount |
| `/data/tvshows` | Your TV bind mount |
| `/downloads` | Your completed downloads, read-only |

In **Organize library**, set the Anime folder to `anime` and TV folder to
`tvshows` before scanning or importing. Folder names are case-sensitive. These
are paths relative to `/data`. The Movies collection remains available but has
no host movie mount in this example; add one before using it. Imports and
existing-file registration still require the normal review workflow.

Keep the `library-state:/data` mount: it preserves retained versions and recovery
files outside the individual library mounts when the container is replaced.
Keep the Compose project/folder name stable. Do not use `docker compose down -v`
when keeping your installation. Configuration backups contain the database and
key, not media or the retained-version volume; back up those separately.

## Updates and existing installations

Create and download a verified backup in **Backups** before updating. Then:

```sh
docker compose pull
docker compose up -d
docker compose exec reelscarey reelscarey doctor
```

Existing installations should keep their current volume mappings and
`REELSCAREY_CONFIG_DIR`. Do not point an existing flat `/config` database at
`/config/state`: that is a different, fresh database location. The old
[server Compose](../compose.server.yaml) and [source-build Compose](../compose.yaml)
remain supported with their explicit non-root user and separate volumes. For a
new simple installation, use this template from the beginning.

Advanced installations can still specify `user: "10001:10001"`, a read-only
container filesystem, and dropped capabilities with pre-owned writable mounts.
When `user:` is set, PUID/PGID must match it or be omitted; automatic permission
initialization is disabled. No application process is run as root.