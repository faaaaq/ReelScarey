# ReelScarey 0.54.48

Makes full-scope and large-collection Shuffle reflect one concrete playback
order while keeping the interface responsive with very large libraries.

The Now Playing queue now shows the actual shuffled upcoming order, and Next,
ended and Previous follow that same order. Enabling Shuffle preserves current
and prior history while randomizing only the upcoming suffix; disabling it
leaves the installed order unchanged. Full-scope Shuffle still loads every
matching library, artist, album or playlist track before replacing the queue.

Queue installation now uses linear-time Set-based deduplication and Fisher-Yates
shuffling instead of quadratic duplicate checks. Now Playing renders a sliding
preview of at most 100 positions with up to 25 history rows while retaining the
complete in-memory queue, full up-next counts and precomputed duration totals.
Queue-only screens no longer rerender for every media-time update.

Large live queues remain uncapped during the session. Browser persistence saves
at most the current track and next 499 entries, with a small separate
track-identified progress/preferences record rather than repeatedly serializing
the whole queue. Immediate pagehide and mismatched-record restore cases retain
the correct track and timeline.

No backend schema, provider or public API contract changed. Existing playback,
persistent playlists and NFO cleanup behavior remain intact.

## Validation

Fresh `npm ci` completed successfully. Frontend formatting and TypeScript checks
passed, all 84 frontend test files (509 tests) passed, and the production build
completed. Full `go test ./...`, `go vet ./...` and `go build
./cmd/reelscarey` passed on the release content.

All nine real-browser music tests passed against the Go server and generated
audio. The large-library scenario loaded 1,203 playable tracks, retained the
complete queue, rendered exactly 100 rows, persisted 500 entries, decoded real
audio and verified that Next started the exact track shown next in Now Playing.
A separate unit regression covered 2,500 tracks. Further tests cover concrete
shuffle order, history and repeat traversal, sliding absolute queue indexes,
bounded storage/reopen, track-specific progress restoration, and queue-only
render isolation from media-time updates.

An earlier complete browser run had one disposable-server readiness timeout
before the large-library scenario could run. Its retry passed, and a final
complete run with retries disabled then passed all nine tests in 17.2 seconds.

The [release application checks](https://github.com/faaaaq/ReelScarey/actions/runs/45358294852)
also passed with Linux race-enabled tests, vet/build/smoke, Windows smoke and
browser coverage.

## Publication and live deployment

Docker Hub published `faaaaq/reelscarey:0.54.48` and `faaaaq/reelscarey:latest`
on November 24, 2026. Both registry inspections verified the same AMD64/ARM64
manifest for release commit `173f891ef48d640dd8c28979b9e08f3e9556b128`.

```text
sha256:3ebff0062b0b5879abf4304142f5d13ee6b02dfd60a62dd23b54ac2359b7b787
```

See the [successful publish run](https://github.com/faaaaq/ReelScarey/actions/runs/45358294678).
Both architecture builds, startup and recovery checks, final tag pulls and
signing passed. `gh attestation verify` verified the signed digest against the
release commit and primary branch.

The scareypit Compose service was upgraded from 0.54.47 to 0.54.48 after the
registry check. The pre-upgrade configuration was preserved at
`/home/faaaaq/configs/reelscarey/config-backup-20261124025825`, including the
active `state/reelscarey.db` and matching `state/secrets.key`; the Compose backup
is `/home/faaaaq/configs/reelscarey/compose.yml.bak-20261124025825`. The recreated
container reports `0.54.48`, is running and healthy, and retains schema 114. Its
image ID, registry digest and OCI revision label match the verified release.
