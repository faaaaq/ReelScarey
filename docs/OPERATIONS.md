# Installation, backup and recovery

This is a private development build. NAS hardware validation, an independent
security review and the owner's license decision remain release gates.

## First run and storage

Docker Compose runs the complete application in one non-root container. The Go
server embeds the built React UI; FFmpeg, FFprobe and pinned yt-dlp are included.
Use the Compose instructions in README. Config and YouTube staging use persistent
volumes; media and download locations are separate host mounts. No host Go, Node,
Python or FFmpeg installation is required for deployment.

Run `docker compose exec reelscarey reelscarey version` to identify a running
build. Build-time source tools and disposable native regression tests are only
for development; standalone installation packages are no longer produced.

Keep the configuration directory on local storage with reliable SQLite locking.
Use separate config, media and download roots. Give only the app's OS account
access to config. Unix private files use mode 0600/directories 0700; Windows
owners must restrict the directory's NTFS ACLs. A portable file mode does not
replace Windows ACLs. Download sources are read-only inputs to the importer.

Run `reelscarey setup-token` with the same config environment to get the initial
administrator setup code. It is not printed in startup logs. After account
creation, Setup guides metadata and optional provider configuration. Torznab may
remain unconfigured; catalog, profiles and subtitle settings still work.

Use System checks after setting storage paths. GET only reads the last report
from this process. Run checks explicitly to inspect DB integrity, encrypted
records, path separation, folder access, hardlink publication and FFprobe
availability. The probes create/remove only tiny private config/media files;
downloads are checked for read access. `reelscarey doctor` provides the same
report locally and exits nonzero if attention is needed. Provider live tests
remain on their settings pages. Results are snapshots, not background monitors.
The **Library import method** preference is separate from the publication
check: copy-only imports are valid, but the media filesystem must still support
the private no-overwrite hardlink used to publish each file.

## Administration backup interface

Open **Backups** for live snapshots, scheduling, retention, ZIP download/upload
and a verified restore with a safety snapshot and controlled restart. See the
[backup manager guide](BACKUPS.md) for scope, limits and interruption recovery.
Video/subtitle/download files and host deployment settings need separate backups.
The offline procedures below remain available when the app cannot start.

## Verified offline backup

Stop the app first (including older builds which do not participate in the new
process locks). Leave the original config intact. Run with its config environment:

```text
reelscarey backup /private-backups/reelscarey-2026-10-23
reelscarey backup-check /private-backups/reelscarey-2026-10-23
```

For Docker, run maintenance commands in a temporary container using the same
configuration volume after stopping the application. Mount a private backup
parent at /backups, writable by UID 10001 for creation and read-only for checking.
The destination
parent must exist and the destination folder must be new. Backup uses SQLite's
consistent snapshot operation to include committed WAL data, then verifies the
migration ledger, database integrity and every encrypted record. It writes a
manifest with SHA-256 checksums, the schema and application version. Only the
database, matching secrets.key and any remaining initial setup-token are copied.
Media, downloaded torrents, environment variables and reverse-proxy settings
need their own backups. For reviewing and moving media between host folders
after a restore or migration, see [Storage](STORAGE.md) and the media migration
commands described there. Limits: 32 GiB database, 1 KiB key/setup files, 10 minutes
per command. Do not put backups inside config, media or downloads.

For example, after preparing a private host backup parent writable by UID 10001:

```sh
docker compose stop reelscarey
docker compose run --rm --no-deps -v /srv/reelscarey-backups:/backups reelscarey backup /backups/before-upgrade
docker compose run --rm --no-deps -v /srv/reelscarey-backups:/backups:ro reelscarey backup-check /backups/before-upgrade
# Start the current build again, or proceed to the upgrade after verification.
docker compose up -d --no-build reelscarey
```

Backups include password hashes and decryptable provider credentials. Encrypt
and access-control the storage and transport, keep an off-device copy and test
recovery regularly. Checksums detect corruption, not a malicious replacement of
both data and manifest. Only restore trusted backups. Missing keys are never
regenerated to hide an error. A backup without its key cannot recover encrypted
credentials. Never start a backup folder directly as the app's configuration.

## Restore and review

Stop the app. Keep the original config and backup unchanged. With the source
config environment still selected, restore into a NEW, separate config folder:

```text
reelscarey backup-check /private-backups/reelscarey-2026-10-23
reelscarey restore /private-backups/reelscarey-2026-10-23 /srv/reelscarey/config-recovered
```

Existing destinations are refused. Restore verifies into private staging, checks
again before publication, revokes copied sessions and pauses search/subtitle
schedules. It leaves a recovery-mode marker so all background workers wait.
Change REELSCAREY_CONFIG_DIR to the new folder, fix its ownership/ACLs, and start
the same or a newer compatible build. The saved administrator password works;
sign in afresh. Newer migrations run at normal startup, not during backup/check.

Open System checks. Review provider endpoints, media/download paths, pending
downloads and import journals. A restored client may still point at the original
qBittorrent server: never run a recovery clone against shared live jobs while the
original is active. Run checks, confirm the review box, and Resume workers only
when this installation should own those jobs. Resume does not enable paused
search/subtitle schedules; enable those separately after reviewing preferences.
Already-running torrents in qBittorrent are outside ReelScarey's worker hold.

If a command fails, its newly created destination may remain incomplete. A
marker prevents the app from starting an unfinished restore. Preserve it for
inspection and retry into another new folder; never remove a marker to bypass
verification or overwrite a working configuration. Interrupted private staging
folders may remain after a power loss; inspect them manually before cleanup.

## Upgrade, rollback and packaging

Stop the app, make and verify an offline backup with the current version, retain
the current executable/image, and then install the new version. Start once,
confirm `/readyz`, sign in, run System checks and inspect activity. The OS process
lock prevents cooperating 0.8+ instances from owning the same local config;
stale lockfiles are harmless and must not be deleted to bypass a running owner.
Network filesystem lock semantics are not a supported substitute for local config.

## Bounded shutdown

On SIGINT/SIGTERM the HTTP server drains first (10 seconds on a normal stop,
5 minutes for a reviewed restore restart). Background workers are then cancelled
and joined against a shared 30-second drain deadline. A worker that ignores
cancellation cannot hang shutdown forever: after the deadline the process
exits, terminating the worker, and durable leases, recovery journals and
startup reconciliation finish or reset its work on the next start. The startup
log distinguishes a clean drain ("all background workers stopped cleanly") from
a timed-out one ("shutdown finished after the worker drain deadline expired",
naming the workers still running at exit). A timed-out shutdown is not a
graceful one; check the named workers' journals after the next start.

In-place schema downgrades are unsupported. To roll back, stop the new build and
use the retained old build with its own compatible stopped config snapshot in a
separate folder. New-format recovery holds require 0.8+; do not point older builds
at restored jobs until those jobs have been isolated and reviewed manually.

Deployment builds one runtime image through Dockerfile. Node and Go build stages
and pip/setuptools never ship in that runtime. CI retains disposable binary
regressions for backup/recovery, then verifies the actual container. Tests and
operational recovery commands remain part of the source repository.

Set `REELSCAREY_VERSION` when building a named image from this checkout. Compose
runs a prebuilt image; to upgrade, pull the new image (or build a local one with
`docker build -t reelscarey:local .` plus `REELSCAREY_IMAGE=reelscarey:local` in
`.env`), then:

```sh
# First stop the app and create/check an offline backup as described above.
docker compose pull reelscarey
docker compose up -d --no-build reelscarey
docker compose exec reelscarey reelscarey version
docker compose exec reelscarey reelscarey healthcheck
docker compose logs --tail=50 reelscarey
```

The container has a read-only root filesystem, a bounded temporary directory,
no extra capabilities, an init process, a PID limit and a graceful stop budget.
Retain the previous image plus its compatible verified database backup for
rollback; never point an older image at a newer schema. Restarting or rebuilding
must preserve the named config and staging volumes. Do not use `down -v` for an
upgrade. Backups and retained media versions are runtime data, not repository
cleanup targets.

No public release is published automatically. Dependency licenses remain their
authors' property; the project itself awaits the owner's license choice.
