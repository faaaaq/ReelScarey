# Ubuntu server live-testing install

For a normal one-file Compose install, use [INSTALL.md](INSTALL.md). This guide
retains the isolated test layout and explicit non-root deployment option.

Use Ubuntu x86-64 with Docker Engine and Compose v2. This installation runs one
container from the existing `faaaaq/reelscarey` Docker Hub repository. No source
build, Node, Go, Redis or external database is required on the server.

## Install

Save `compose.server.yaml` and `server.env.example` from the same release into a
new folder, for example `~/reelscarey-live`. Then:

```sh
cp server.env.example server.env
sudo install -d -o 10001 -g 10001 -m 0750 /srv/reelscarey-test/media /srv/reelscarey-test/downloads
```

Edit `server.env`: replace the example IP with your server's LAN IP in both
`REELSCAREY_BIND_IP` and `REELSCAREY_PUBLIC_URL`. The public URL is the exact URL
used in your browser, including the port. Use the release's immutable `sha-...`
tag or digest for `REELSCAREY_IMAGE` so later pushes cannot change this test.

The repository denied an anonymous pull during release preparation. If that
remains the case, authenticate on the server using a Docker Hub account with
pull access. Enter credentials directly into Docker; do not send tokens in chat.

```sh
docker login
# Run all following commands from the same installation folder.
docker compose --env-file server.env -f compose.server.yaml config --quiet
docker compose --env-file server.env -f compose.server.yaml pull
docker compose --env-file server.env -f compose.server.yaml up -d
docker compose --env-file server.env -f compose.server.yaml ps
docker compose --env-file server.env -f compose.server.yaml exec reelscarey reelscarey doctor
docker compose --env-file server.env -f compose.server.yaml exec reelscarey reelscarey setup-token
```

Open the configured browser URL and create your administrator account using the
one-time setup code. There is no default password. The code disappears after
setup. The healthcheck may show `starting` for the first 30 seconds. Use
`docker compose --env-file server.env -f compose.server.yaml logs --tail 100`
if startup fails.

The port binds to the selected LAN interface. Keep it on your private network;
use HTTPS through your existing reverse proxy for remote browser access and set
`REELSCAREY_PUBLIC_URL` to that HTTPS origin. No router port-forward is required.

## Persistent data and permissions

| Container path | Storage | Purpose |
| --- | --- | --- |
| `/config` | Named volume | Database, administrator and encryption key |
| `/media` | Your test media folder, writable | Registered library and sidecars |
| `/downloads` | Your download folder, read-only | Completed client downloads |
| `/youtube-staging` | Named volume | Acquired YouTube files awaiting review |
| `/backups` | Named volume | Backup ZIPs and restore safety copies |

Named volumes initialize with the image's permissions. The image runs as
UID/GID **10001:10001**. Existing media/download mounts must grant that user
appropriate access; do not change ownership recursively across an existing
library. The initial commands above are for the new dedicated test folders.
When connecting a download client, map its completed-download path to the same
host folder mounted at `/downloads` and enter the matching paths in Settings.

Keep the installation folder/project name stable so Compose reuses its volumes.
`docker compose down` retains named volumes; **do not add `-v`** when preserving
this installation. SQLite configuration belongs on local server storage, not an
unverified network filesystem. Media backup is separate from configuration and
library-database backup.

## First live test

1. Run **System checks** and confirm every required check is ready.
2. Create a backup in **Backups**, download it, and keep an off-server copy.
3. Configure providers directly in Settings. Do not send API keys or session
   cookies in chat. Schedules and automatic acquisition remain opt-in.
4. Use a generated or owned test video in the dedicated test folder. Review its
   discovery/match before registration. Confirm the file and episode mappings.
5. Test one download client/provider path with owner-selected test material,
   then test subtitle review, organization/rollback and restart persistence.
6. Share the app URL reachable from this workstation and which test workflow you
   want exercised. Server access is not established merely by publishing an image.

Private tracker acceptance remains deferred unless explicitly selected. The
cancelled OCR, translation and audio-alignment features are absent. Existing
reference-text alignment and manual timing edits remain available.

## Upgrade or roll back

Before changing `REELSCAREY_IMAGE`, make and download a verified backup, retain
this Compose/environment configuration, and record the current image digest.
Then `pull` and `up -d` using the commands above. Run System checks and verify
library counts after restart. Use the Backups interface for reviewed restoration;
never run an older image against a database upgraded beyond its supported schema.