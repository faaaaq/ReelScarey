# ReelScarey 0.54.45

Music track tables now show the preferred primary file's bitrate, file size
and file type alongside the existing title, album/artist context and duration.
Album, unified library, artist, top-track and chart views share the same
formatting. A **Show filenames** toggle adds a basename-only filename column on
demand; it is off by default. Provider-only tracks and unknown file properties
show an em dash instead of inferred values.

The additive track-response fields describe the same primary file selected for
playback. Album, artist and unified-track queries return them directly, while
matched top tracks and charts load the selected primary file's properties.
POSIX and Windows paths both produce the expected display filename.

Validation used a clean checkout of the current primary branch without the
unrelated source-upgrade working-tree changes. Fresh `npm ci` reported no
vulnerabilities. Frontend formatting, TypeScript checking, all 80 test files
(463 tests) and the production build passed. Full `go test ./...`,
`go vet ./...` and `go build ./cmd/reelscarey` passed, and the resulting binary
reported `0.54.45`. Focused Go regressions verify album, artist, unified,
top-track and chart metadata; the album UI test verifies formatted values and
both filename toggle states.

The additive API fields passed the complete
[feature application checks](https://github.com/faaaaq/ReelScarey/actions/runs/44994575971),
including Linux race-enabled tests, vet/build/smoke, Windows smoke and browser
checks, before the release commit was pushed.

## Publication and live deployment

Docker Hub published `faaaaq/reelscarey:0.54.45` and `faaaaq/reelscarey:latest`
on November 22, 2026. Both pulls verified the same AMD64/ARM64 manifest for
commit `b3a59fd6f695337815e50e882327e28535d39c64`.

```text
sha256:c6e1cb4a67991eb046b8c0e93c4936eaf6443a34acda1f468713288fe8e7ab25
```

See the [successful publish run](https://github.com/faaaaq/ReelScarey/actions/runs/44997559693).
Both architecture builds, container smoke tests, final tag pulls and signing
passed. `gh attestation verify` also verified the signed digest against the
release commit.

The scareypit Compose service was upgraded from 0.54.44 to 0.54.45 after the
registry check. The pre-upgrade configuration was preserved at
`/home/faaaaq/configs/reelscarey/config-backup-20261122014021`, including the
SQLite database and encryption key; the Compose backup is
`/home/faaaaq/configs/reelscarey/compose.yml.bak-20261122014021`. The recreated
container reports `0.54.45`, is running and healthy, and retains schema 113.
