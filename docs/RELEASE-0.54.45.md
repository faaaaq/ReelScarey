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
