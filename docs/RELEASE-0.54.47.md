# ReelScarey 0.54.47

Adds persistent administrator library playlists to **Music -> Player**, replacing
the limited Artist Index. Tracks can be added to an existing playlist or used to
create one; playlist views support playback, rename, exact move up/down ordering,
removal and confirmed deletion. Ordered membership is stored in SQLite with
canonical music-track IDs and revision-checked mutations. Catalog refreshes
preserve stable logical track membership, and the Subsonic named-user playlist
model remains separate. The transient browser playback queue retains its
existing 500-entry restoration bound.

Shuffle now loads the complete selected scope before replacing the queue: every
result in the current All tracks search, every artist page, the complete album,
or the complete saved playlist. Sequential loading, failure recovery and action
fencing prevent partial or stale results from replacing newer playback choices.
Ordinary Play and Queue actions remain loaded-page scoped.

Also includes the episode `sortTitle` cleanup. Generated TV/Anime episode NFOs
contain an explicit blank `<sorttitle></sorttitle>`, and the shared NFO worker
clears non-empty direct-child values beside newly registered episode media even
when NFO generation is disabled. **General settings -> Episode ordering** adds a
confirmed, resumable administrator batch for existing registered episodes. The
XML-aware, bounded write path preserves unrelated metadata and video bytes,
refuses unsafe or malformed inputs, and reports per-file failures. Emby or
Jellyfin may require its normal metadata refresh to consume changed sidecars.

Migration 114 creates the independent library-playlist tables and advances
playlist revisions before catalog-track cascades. The new administrator playlist
API and episode sort-title cleanup route retain the existing authentication,
CSRF and same-origin boundaries.

## Validation

Fresh `npm ci` completed successfully. Frontend formatting and TypeScript checks
passed, all 84 frontend test files (497 tests) passed, and the production build
completed. All eight real-browser music tests passed against the Go server and
generated audio, including persistent playlist creation, adding tracks, reload,
and complete-playlist Shuffle.

Full `go test ./...`, `go vet ./...` and `go build ./cmd/reelscarey` passed on the
combined release content. Playlist tests cover migration/upgrades, canonical
membership preservation, revision conflicts, exact ordering, authorization and
CSRF. Sort-title tests cover XML preservation, malformed and unsafe sidecars,
atomic replacement, disabled-export cleanup, new registrations, bounded batches,
requester denial and administrator API behavior.

The substantial migration, public API and NFO write-path changes passed the
[primary application checks](https://github.com/faaaaq/ReelScarey/actions/runs/45320699351)
at commit `beb9547b83dc6bbbca6ae3246d7d818520318be7`, including the Linux
race-enabled suite, browser tests and Windows smoke.

The [release-commit application checks](https://github.com/faaaaq/ReelScarey/actions/runs/45325365615)
passed the Linux race-enabled suite and browser tests. Their Windows smoke job
was retried twice, but all three attempts failed because Chocolatey returned
504/503 while fetching FFmpeg; the subsequent smoke assertion correctly reported
that FFprobe was unavailable. The Windows binary build and route-level package
tests passed on each attempt, and the identical feature tree passed the complete
Windows smoke job in the primary checks above. The release-commit run is not
represented as fully green.

The earlier scheduled acceptance run
[45254007529](https://github.com/faaaaq/ReelScarey/actions/runs/45254007529), on
the preceding 0.54.46 source, failed its full-race and generated-subtitle
acceptance jobs. Those failures are recorded separately and are not represented
as passing acceptance for this release.

## Publication and live deployment

Docker Hub published `faaaaq/reelscarey:0.54.47` and `faaaaq/reelscarey:latest`
on November 23, 2026. Both registry inspections verified the same AMD64/ARM64
manifest for release commit `677fd4b291e0dfa70134427fa6d6966f53ef1382`.

```text
sha256:51140360ae1b4eff9728cee490ecb67f8a078ec41fdfb6c86d9db5fc78fec30c
```

The first [publish run](https://github.com/faaaaq/ReelScarey/actions/runs/45325365686)
failed in the AMD64 build on the existing
`TestAuditEConversionInterruptedMidConvertThenRetry` assertion with
`context canceled`; it stopped before the manifest and no versioned tag was
created. The focused test passed 20 consecutive local repetitions. Retrying the
failed publishing job then passed both architectures, startup and recovery
checks, final tag pulls and signing. `gh attestation verify` verified the signed
digest against the release commit and primary branch.

The live Compose service now uses `faaaaq/reelscarey:0.54.47`, reports version
`0.54.47`, is running and healthy, and has schema 114 with both library-playlist
tables. The running image ID, registry digest and OCI revision label all match
the verified release.

The stopped-service configuration backup is
`/home/faaaaq/configs/reelscarey/config-backup-20261123230100`; its
`state/reelscarey.db` contains the actual pre-upgrade schema-113 database and
`state/secrets.key` retains the matching encryption key. The original Compose
file is `/home/faaaaq/configs/reelscarey/compose.yml.bak-20261123230100`.

The first deployment attempt used the inactive `config/reelscarey.db` for its
schema assertion instead of the active `config/state/reelscarey.db`. That
incorrect assertion triggered a Compose-only rollback to 0.54.46 after migration
114 had already run; the older binary refused the newer database as designed.
The compatible 0.54.47 image was immediately restored, then health, version,
image identity, active schema and backup contents were verified. No database
downgrade or restore was performed. This recovered deployment failure is recorded
separately from the successful application and publication validation.
