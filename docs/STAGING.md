# Staging deployment and rollback procedure

Release candidates are promoted through a staging instance before production.
Docker is the only supported deployment target. Never deploy `latest` to
staging or production: pin the exact `sha-<full commit>` image tag (or digest)
that the publish workflow produced for the reviewed commit, and record the
digest in the promotion notes.

## 1. Before deployment

1. **Application backup** — trigger a manual live backup in the staging
   instance's Backups page (or `POST /api/v1/backups`). A snapshot contains the
   SQLite database, accounts, profiles, connection credentials and the matching
   `secrets.key`. It never contains media, subtitles, downloads or container
   configuration.
2. **Media and config copies** — separately copy (rsync) the `/config` folder
   (database + encryption key), and the music library folders under `/media`
   that the deployment can modify (converted variants, tag-edit backups). The
   application backup cannot restore filesystem changes made after the
   snapshot.
3. Record free disk space. The stream cache may add up to
   `cache_limit_mb` (default 512 MiB) inside the media root; conversion
   variants add their own copies.
4. Confirm the exact image digest: `docker image inspect <tag>` and save
   `Id`/`RepoDigests` for rollback.

## 2. Deployment

```bash
docker pull <registry>/reelscarey@<digest>   # or the sha-<commit> tag
REELSCAREY_IMAGE=<pinned tag> docker compose up -d
```

Migration `103_music_transcoding` runs automatically at container startup
(entrypoint runs migrations before exec'ing the binary). It is additive: new
tables (`music_quality_profiles`, `music_conversion_jobs`,
`music_stream_cache`), new `music_preferences` columns with defaults, new
`music_files` variant columns and new `music_server_users` columns. Existing
rows keep their values; nothing is rewritten.

## 3. Post-deployment verification (staging)

| Check | How |
| --- | --- |
| Migration 103 applied | Startup logs show the migration without error; the schema version reads 103 (`sqlite3 /config/reelscarey.db 'SELECT MAX(version) FROM schema_migrations'` inside the container, or the diagnostics endpoint) |
| Service healthy | `GET /health` (or the healthcheck status) and an authenticated admin session |
| Music catalog intact | Artists/albums/tracks list, counts match the pre-deployment backup summary |
| Downloads + conversion | Queue a small library optimization (`POST /api/v1/library/music/optimize/apply`, ≤ 5 files); the job reaches `completed`, the variant appears under `variant_role='variant'`, and the original is retained per the saved retention policy |
| Converted exports | `GET /files/{id}/export?profile=mp3-compatible` and `opus-balanced`: correct Content-Type, `ID3`/`OggS` magic, tags and artwork present (MP3/FLAC/M4A), byte-range 206 on a repeated ranged request |
| Streaming | Web player plays a FLAC-sourced track at the saved streaming quality; a Subsonic client streams with explicit `format`/`maxBitRate`; per-user stream quality resolves |
| Stream cache | `GET /api/v1/library/music/cache` shows entries growing and `size_bytes ≤ limit_bytes`; `.reelscarey-stream-cache` is inside the media root |
| Video/anime regression | Play one movie and one TV episode; library browsing, Wanted/Calendar read-only pages render |
| Worker health | No job stuck in `processing`; a `docker restart` mid-queue reclaims and completes the job |
| Logs | `docker compose logs --since 10m` — no stack traces, no absolute filesystem paths, no secrets in responses or logs |

## 4. Rollback

**Application rollback.** Redeploy the previous image tag and restart:

```bash
REELSCAREY_IMAGE=<previous pinned tag> docker compose up -d
```

Migrations are append-only. Rolling the binary back does **not** reverse
database changes, and the previous binary version may refuse to start against
a newer schema (fail closed). Verify in staging — never assume.

**Database rollback (required when the older binary refuses the schema, or
data must be returned to the pre-deployment state):**

1. `docker compose down` (this preserves named volumes; never use `-v`).
2. Restore `/config` (database + `secrets.key`) from the pre-deployment copy
   taken in step 1 — or use the Backups page's reviewed restore if the older
   binary is still running.
3. Leave `/media` untouched: media files created since the snapshot (conversion
   variants, tag-edit backups) stay on disk as unreferenced files; the catalog
   simply does not list them. Delete verified variant files manually only if a
   byte-exact pre-deployment media state is required — never delete an
   original music file without its verified backup.
4. Start the previous image and repeat the verification table above.

**Runtime failure recovery.** If the container crash-loops after deployment:
capture `docker compose logs` for the report, `docker compose down`, restore
the `/config` backup, start the previous pinned image, and re-verify. A failed
conversion never deletes the only valid source; failed jobs keep their staged
output quarantined and their source intact.

## 5. Promotion

Production promotion happens only after every staging verification row passes
and the remote CI acceptance run (including the race suite and the container
smokes) is green for the exact commit being promoted.
