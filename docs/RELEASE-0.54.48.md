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

## Publication and live deployment

Versioned publication and the live scareypit upgrade are pending at this commit.
This section will record the verified registry digest, attestation, backups and
resulting application health after publication.
