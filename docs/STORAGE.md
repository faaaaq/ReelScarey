# Storage browsing and guided disk migration

Version 0.23 adds the **Storage** page. Folder browsing requires an administrator
and shows names, types, sizes and modification dates. It never reads file bodies,
registers media or changes files. The existing **Import library** flow still owns
metadata matching and verified registration within configured collections.

## Browse mounted folders

The media and download mounts are available by default. Additional disks/shares
can be mounted read-only and explicitly named in `REELSCAREY_BROWSE_ROOTS`:

```yaml
environment:
  REELSCAREY_BROWSE_ROOTS: '{"archive":"/archive"}'
volumes:
  - /srv/archive:/archive:ro
```

Restart the container after changing deployment settings. At most eight extra
roots are allowed; names use lowercase letters, digits and hyphens, with a maximum
of 40 characters. Paths must be absolute container paths. The server excludes
configuration/backup roots and their ancestors/descendants. Root and child links
are refused, hidden path components are excluded, and special files cannot be
opened. Listings are bounded to 10,000 entries with 100 entries per page. URLs
preserve folder and page selection. Unavailable folders remain selectable so the
administrator can recover by choosing another mount.

## Guided offline main-root migration

The app continues to use its original container media path, normally `/media`.
The migration copies the complete physical tree into a **new folder** on another
mounted disk. It never deletes the original or silently edits Compose/host files.
Stop ReelScarey while using the offline commands. Prepare an existing destination
parent writable by container UID/GID 10001 and a separate writable backup mount.

Example using a new host disk mounted at `/srv/new-disk`:

```sh
docker compose stop reelscarey
docker compose run --rm --no-deps -v /srv/new-disk:/migration-target reelscarey media-root-plan /migration-target/library
# Review source, destination, file/folder counts, byte count and digest.
docker compose run --rm --no-deps -v /srv/new-disk:/migration-target reelscarey media-root-copy DIGEST
```

Replace `DIGEST` with the exact returned review digest. Copying first creates and
verifies a private configuration/key backup, whose path is returned in the result.
The full inventory is retained in `/config/media-root-migration.json`; the Storage
page shows its summary. Planning does not copy media. A changed source, review,
destination parent or conflicting import journal blocks the copy.

After successful copying, change the host `MEDIA_PATH` in `.env` to
`/srv/new-disk/library`, preserving the container path `/media`, then run:

```sh
docker compose run --rm --no-deps reelscarey media-root-activate DIGEST
docker compose up -d --no-build reelscarey
```

Activation verifies the exact copied tree and receipt through the new `/media`
mount. The original mount cannot pass activation. Workers remain held for review
in **System checks** before resuming. Existing schedules are retained behind that
hold. No catalog IDs or relative paths are reassigned. Old filesystem-dependent
reviews may require recreation because copies have new file identities.

### Interruption, cancellation and bounds

- Retrying `media-root-copy DIGEST` resumes a stopped copy. Completed files are
  reverified. Publication uses Linux no-replacement directory rename; an existing
  foreign destination is preserved. Every file is SHA-256 checked and synced;
  regular file modes/timestamps, empty folders, extras, subtitles and retained
  versions are copied. Source and destination inventories are rechecked before
  publication. The original is kept throughout.
- A durable pending marker blocks server startup during copying and until mount
  activation or cancellation. Do not start an older application image during a
  pending migration. Uncertain receipts or changed published data remain blocked
  for offline attention; they are never guessed or overwritten.
- To cancel, mount the **original disk at the original container path**, then run
  `docker compose run --rm --no-deps reelscarey media-root-cancel DIGEST`.
  The original root's identity must still match. Current original files, copied
  folders, safety backups and temporary data are retained. Cancellation does not
  delete a copied destination or repair an incorrect host mount.
- Private scratch files live in the returned `temporary_copy_folder`, beside the
  copied destination. Interrupted/unknown scratch files remain outside the final
  library and are preserved. Inspect them before any manual cleanup.
- Reviews support up to 100,000 entries and a 64 MiB manifest. Links, devices,
  case-colliding names, unsupported publication semantics and active import/move
  journals must be resolved first. Each command is bounded to 24 hours. Larger
  trees require a separately verified offline migration. Docker on Linux is the
  supported target; representative NAS/share behavior remains a separate check.

Keep independent media backups. Configuration archives contain the catalog and
keys and do not replace a backup of media, extras or retained subtitle versions.
