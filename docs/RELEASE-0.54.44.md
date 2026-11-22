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

All 463 frontend tests passed, along with formatting, type checking, the
production build, Go vet and the Go binary version check. Local music
candidate and offline TIDAL login, search and download regressions passed.
The local full Go suite hit sandbox and Windows environment failures. The
[application checks](https://github.com/faaaaq/ReelScarey/actions/workflows/ci.yml)
and [publish workflow](https://github.com/faaaaq/ReelScarey/actions/workflows/docker-publish.yml)
record release verification independently. The scareypit upgrade requires
separate SSH verification following publication.
