# ReelScarey 0.54.46

Adds a full MediaMonkey-inspired music player at **Music -> Player**. The left
library pane switches between tracks, artists and albums with contextual
indexes. The center provides search, pagination, detailed file metadata,
in-place artist/album previews and links to the existing management pages. The
right Now Playing pane displays artwork and an editable queue with jump,
move, remove and clear actions. Desktop, medium and mobile layouts preserve
usable browsing and queue controls.

The shared bottom transport adds shuffle and repeat off/all/one alongside the
existing previous/play/next, seek, volume, streaming-quality and recovery
controls. Queue entries can be added without starting playback, played next,
reordered and restored on that browser after reload. Play actions operate only
on loaded tracks, up to 100 per library/artist view or the loaded album, and
provider-only rows remain disabled rather than implying unavailable audio can
play. Search and artist/album selections survive reload and browser history.
Queue state is browser-local, not a server-synchronized playlist.

No backend schema, provider or public API contract changed. The existing
authenticated audio engine, artwork, streaming, seeking and error recovery are
reused; the existing Music Library and detail workflows remain available.

## Validation

Fresh `npm ci` reported no vulnerabilities. Frontend formatting and TypeScript
checks passed, all 82 frontend test files (479 tests) passed, and the
production build completed. The suite covers workspace navigation, loaded
track metadata, queue insertion/order, move/remove/clear, shuffle/repeat and
persistence. All seven real-browser music tests passed against the Go server,
including actual audio decoding, MP3 fallback, seek/resume, rapid skipping,
failure recovery, streaming-quality persistence and the responsive full-player
workspace. Full `go test ./...`, `go vet ./...` and `go build
./cmd/reelscarey` passed on the release content.

The previously scheduled acceptance run
[45041301096](https://github.com/faaaaq/ReelScarey/actions/runs/45041301096)
was not green: its full race suite hit the one-hour `internal/music` timeout,
and its container metadata rollback case returned `reload_required`. Those
results predate this frontend-only release and are recorded separately, not as
successful acceptance. The release publish workflow has its own architecture,
startup, backup/restore, feature and storage-recovery gates.
