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

The earlier scheduled acceptance run
[45254007529](https://github.com/faaaaq/ReelScarey/actions/runs/45254007529), on
the preceding 0.54.46 source, failed its full-race and generated-subtitle
acceptance jobs. Those failures are recorded separately and are not represented
as passing acceptance for this release.

## Publication and live deployment

Versioned publication and the live scareypit upgrade are pending at this commit.
This section will record the verified registry digest, attestation, backups and
resulting application health after publication.
