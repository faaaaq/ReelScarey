# ReelScarey 0.54.44

Repairs the formatting gate that blocked 0.54.43 on both Docker architectures.
This patch carries forward collaboration-aware music matching and candidate
review in the download dialog from [0.54.43](RELEASE-0.54.43.md).

[PR #33](https://github.com/faaaaq/ReelScarey/pull/33) brings parallel checks
and acceptance to the current branch. Linux checks, Windows smoke and browser
tests start together. Nightly and manual acceptance split the full Go race
suite from the live client, generated-media and Docker battery. The combined
check requires both to pass. AMD64 publication defaults to a hosted Linux
runner. [Runner overrides](RELEASING.md#runner-selection) are documented.

All 463 frontend tests passed. The AMD64 Docker publisher passed the Go test
and vet gates and exercised Compose startup, backup/restore, feature,
automatic-library and storage-recovery smoke tests. ARM64 passed its native
runtime and restart checks. Windows smoke and the browser job passed on the
release revision. The separate Linux application check was queued at
publication. Scheduled/manual live acceptance has its own workflow run.
See [release application checks](https://github.com/faaaaq/ReelScarey/actions/runs/44979073979)
and the [acceptance workflow](https://github.com/faaaaq/ReelScarey/actions/workflows/ci.yml).

The local full Go run hit Windows sandbox socket and filesystem errors.
The local binary version check and targeted music and offline TIDAL login,
search and download regressions passed.

## Publication and live deployment

Docker Hub published `faaaaq/reelscarey:0.54.44` and `faaaaq/reelscarey:latest`
on November 21, 2026. Both pulls verified the same AMD64/ARM64 manifest for
commit `f21bd22dc39f77e7b9ac3cb1b8890a7c6e24e541`.

```text
sha256:ea5d3e840a65a2ade988763e8ecc87f4baf7cb2deae592124e8d496833d210d2
```

See the [successful publish run](https://github.com/faaaaq/ReelScarey/actions/runs/44979073975).

The scareypit Compose upgrade is pending. This release session's SSH
connection was denied before authentication, so the running version remains
unverified.
