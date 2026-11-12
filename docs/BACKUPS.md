# Backup manager

Open **Backups** in the administration sidebar. It supports live manual backups,
scheduled backups, retention, ZIP download/upload, deletion and reviewed restore.
All endpoints require an administrator session; mutations also require the
existing CSRF token and same-origin checks. Request accounts cannot access them.

## Contents and storage

A snapshot contains SQLite settings and library records, accounts, profiles,
connection credentials and the matching `secrets.key`; a first-run `setup-token`
is included when present. SQLite `VACUUM INTO` captures committed WAL data without
stopping the server. Every snapshot is checked for migration history, database
integrity, foreign keys, matching encrypted credentials and per-file SHA-256.
The app never copies media, subtitles, downloads, container configuration or
environment variables. Back those up separately. Catalog restore cannot undo
filesystem changes made after the snapshot.

There is one Compose layout: a private `backups` volume mounted at `/backups`,
separate from `/config`, `/media` and `/downloads`. `REELSCAREY_BACKUPS_DIR` can
override that default path (then mount a folder there instead). PUID/PGID select the app account; explicit non-root
installations require writable, pre-owned mounts. The parent must exist; links
and overlap with state/media/downloads/staging are refused. Never use
`docker compose down -v` when preserving named volumes.
Archives contain credentials, encryption keys and password hashes. Keep exports
private and encrypted in independent storage. A backup volume on the same host
does not protect against host loss. ZIP checksums detect damage, not a malicious
party that can rewrite both data and manifest; only restore trusted backups.

Interface archives use ZIP with exactly the supported files at its root, no
directories, links, duplicate names or encrypted ZIP entries. Compressed upload
and database size are each limited to 512 MiB; expansion is bounded before any
entry is copied. Upload verifies the complete snapshot before saving it and
never starts a restore. Offline directory tools remain available for databases
up to 32 GiB; their format is documented in [operations](OPERATIONS.md). To
upload an offline snapshot, ZIP only its manifest and listed files at the root.

## Schedule and retention

Scheduling starts paused. Choose an interval of 1–30 days, retain 1–100 scheduled
copies and set an age limit of 1–365 days. Enabling schedules the first run one
interval from now. Use **Back up now** for an immediate manual snapshot. The
server checks due schedules on startup and every 30 seconds. Missed intervals
produce one backup, rather than a catch-up burst. Successful backups schedule the
next interval; failures retry after an hour and remain visible on the page.
Changing retention alone keeps the existing next-run time. Manual backups reset
the next due time after completion. A restarted unfinished job reports that it
was interrupted and preserves completed archives.

Retention runs only after a successful scheduled snapshot. It removes scheduled
copies exceeding either the count or age limit, while always retaining the
newest. Archives are reverified before automatic deletion. Manual, uploaded and
pre-restore safety backups require explicit deletion. Unknown files and damaged
archives are preserved for inspection. Disabling a schedule never deletes files.
Only one create/upload/preview/delete/restore/settings operation runs at a time.
The database schedule is persistent; the running job is marked durably and never
claims completion before archive publication. Recovery hold prevents scheduled
work until the restored installation is reviewed.

## Restore and interruption recovery

1. Select **Restore** to reverify the archive and review the snapshot time,
   original version, restored administrator and catalog counts.
2. Confirm replacement and type `RESTORE`. The request includes the exact
   reviewed archive digest; changed archives are refused. Remember the restored
   administrator's password. A first-run snapshot returns to account setup.
3. The app privately stages, verifies and migrates the candidate. Both
   administrator and requester sessions are revoked. Search, subtitle and backup
   schedules are paused. An invalid candidate cannot replace active files.
4. A durable restore request closes new mutations, drains HTTP requests and
   cancels/joins all workers. The database is closed before the switch. A fresh
   verified **Before restore** archive captures the latest current configuration;
   failure to create it cancels the restore.
5. A synchronized journal preserves the old database, WAL, key and setup/recovery
   state, publishes only the verified candidate files, and checks them again
   before committing. An interruption before commit rolls back at next startup;
   a committed restore finishes normally. Failed rollback stops startup and
   preserves the journal and both copies for inspection. Never delete these
   files to force startup. Known temporary files are cleaned after completion;
   unexpected files are preserved. The safety ZIP remains in the backup list.
6. The same container reopens the app. Sign in again, review **System checks**,
   connection settings, storage paths and download activity, then explicitly
   resume workers. Search, subtitle and backup schedules remain paused until
   separately re-enabled. Download-client activity outside ReelScarey is not
   stopped by restoring the app.

The browser reconnects after the controlled restart. A restore cancellation or
rollback is shown on the backup page. The deployment's volume mappings and
environment settings stay authoritative and are never taken from an upload.

If the server cannot start or the administrator password is unavailable, use the
offline [recovery](OPERATIONS.md) and [account recovery](AUTHENTICATION.md) tools.
