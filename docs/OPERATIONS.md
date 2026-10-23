# Installation, backup and recovery

This is a private development build. NAS hardware validation, an independent
security review and the owner's license decision remain release gates.

## First run and storage

Use the container instructions in README or extract the package matching your
OS and CPU. Native installs need FFprobe/FFmpeg on PATH for media validation;
packages contain the application and embedded UI, not those external tools.
Run `reelscarey version` to confirm the build. Configure the environment variables
from README before launching the executable with no arguments.

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

## Verified offline backup

Stop the app first (including older builds which do not participate in the new
process locks). Leave the original config intact. Run with its config environment:

```text
reelscarey backup /private-backups/reelscarey-2026-10-23
reelscarey backup-check /private-backups/reelscarey-2026-10-23
```

On Windows use `reelscarey.exe` and an absolute Windows folder path. The destination
parent must exist and the destination folder must be new. Backup uses SQLite's
consistent snapshot operation to include committed WAL data, then verifies the
migration ledger, database integrity and every encrypted record. It writes a
manifest with SHA-256 checksums, the schema and application version. Only the
database, matching secrets.key and any remaining initial setup-token are copied.
Media, downloaded torrents, environment variables and reverse-proxy settings
need their own backups. Limits: 32 GiB database, 1 KiB key/setup files, 10 minutes
per command. Do not put backups inside config, media or downloads.

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

In-place schema downgrades are unsupported. To roll back, stop the new build and
use the retained old build with its own compatible stopped config snapshot in a
separate folder. New-format recovery holds require 0.8+; do not point older builds
at restored jobs until those jobs have been isolated and reviewed manually.

Private CI produces Linux amd64/arm64 tarballs and a Windows amd64 zip with
SHA256SUMS. Verify the checksum before extracting into a new installation folder.
Linux amd64 and Windows execution are tested; arm64 is cross-built and needs
on-device validation. The container is built/tested separately. To reproduce:

```text
cd web
npm ci
npm run build
cd ..
go run ./scripts/package 0.8.0-dev dist/private-0.8.0
```

Output folders must be new; only whitelisted binaries/docs/dependency manifests
are packaged. No local config, secrets or media are included. No public release
is published automatically. Dependency licenses remain their authors' property;
the project itself awaits the owner's license choice.
