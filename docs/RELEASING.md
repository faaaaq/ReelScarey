# Container publishing

Docker remains the only supported deployment target. The primary branch is
`codex/foundation`. Its Docker Hub workflow uses the existing repository variable
`DOCKERHUB_USERNAME` and secret `DOCKERHUB_TOKEN`; it does not create accounts or
change repository visibility. Manual dispatch is restricted to the primary ref.

The workflow reads the application version from web/package.json, embeds it in
the binary and OCI labels, and publishes `latest`, the version, and `sha-<full commit>` tags.
BuildKit attaches maximal build provenance and an SBOM. Use the digest or commit
tag for review and rollback; `latest` changes on each successful publication.

The pinned official GitHub attestation action signs the exact published digest
using OIDC only when the repository is public or the owner has explicitly set
`ENABLE_PRIVATE_ATTESTATIONS=true` after confirming Enterprise Cloud eligibility.
[GitHub requires Enterprise Cloud for private/internal repository attestations](https://docs.github.com/en/actions/how-tos/secure-your-work/use-artifact-attestations/use-artifact-attestations).
Otherwise the workflow explicitly records that signing remains pending; unsigned
BuildKit provenance must not be described as a signed release. No successful
publication or signature is implied by the presence of this workflow.

Before publishing, run the application checks and disposable Docker acceptance,
verify backup/restore, and inspect the actual workflow result. The fast
application checks run on branch pushes and pull requests; the live
qBittorrent/SABnzbd/NZBGet/FFmpeg and container acceptance suite is manual.
Run it before a release with `gh workflow run ci.yml`; the publish workflow
also builds a release candidate and runs its container smoke scripts before
pushing. Hosted runner billing, quota or permissions failures must be reported
separately from local checks. Public distribution still requires the owner's
license choice. Do not silently select a license, enable a paid plan or change
visibility to clear a gate.

The Ubuntu x86-64 live-testing release builds a local candidate and runs backup,
existing-workflow and storage-recovery acceptance before publishing. After login,
it pushes the image, pulls the exact registry digest and verifies its source label
and backup/restore behavior. Existing repository visibility and credentials stay
unchanged. Use [LIVE_TESTING.md](LIVE_TESTING.md) and compose.server.yaml to install.

The storage acceptance mounts a named-volume subdirectory through Docker's
[documented volume-subpath option](https://docs.docker.com/engine/storage/volumes/#mount-a-volume-subdirectory),
so the calling user never needs access to Docker's private host directories.

The 0.26.2 publisher additionally exercises the single-file Compose template with
PUID/PGID, private-directory initialization, separate library mounts, timezone,
backup/restore, full container recreation and HTTPS proxy Host/Origin validation.
It checks symlink/overlap refusal before initializing private folder permissions.
The original explicit-user/read-only layout remains in the release checks.

## Build speed, caching and self-hosted runners

The release candidate and the published image are built by
`docker/build-push-action` with the GitHub Actions BuildKit cache (`type=gha`,
`mode=max`, `ignore-error=true`). Cache scopes keep the three builds apart:

| Scope | Written by | Read by |
| --- | --- | --- |
| `reelscarey-publish` | release candidate and published image | both publish builds |
| `reelscarey-ci` | nightly/manual acceptance image build | acceptance image build |
| `reelscarey-transmission` | disposable Transmission test image | the same step |

The cache is an optimization only. `ignore-error=true` means a cache outage
never fails a build, and a cold cache still produces the same image. GitHub
evicts cache entries beyond the repository storage quota, so a release can
always fall back to a cold build. GitHub also scopes Actions caches by ref and
only falls back to the default branch, so a cache written by one tag is not
guaranteed to be visible to the next tag; a manual publish dispatch from the
primary branch `codex/foundation` warms the scope tag builds can fall back to.
Within one publish job the self-hosted runner reuses its BuildKit worker state
between the release-candidate and the publishing build, which is what makes the
second build cheap.

`Dockerfile` accepts `RUN_GO_TESTS` (default `1`). The backend stage runs
`go test ./... && go vet ./...` only when it is `1`; `go build` always runs.
Local and manual `docker build` invocations, and the acceptance image build,
keep the default. `docker-publish.yml` passes `RUN_GO_TESTS=0` to both the
release-candidate and the publishing build because the application checks
already ran the full Go suite natively, including the race suite in acceptance.

Tag pushes do not trigger `ci.yml` (`push.branches` does not match tags), so
the publish workflow cannot wait for a checks run. Skipping the in-image copy
is only safe when the tag points at a commit whose branch push passed the
checks workflow; nothing in the publish workflow re-runs the Go suite. The
remaining image gates are the release-candidate smoke scripts (startup,
Compose, backup/restore, seven-feature, automatic-library and storage
recovery), the digest pull with its source-label check, and the backup smoke
against the published digest. If a release ever needs a Go test gate inside
the publish workflow, drop `RUN_GO_TESTS=0` from the release-candidate build
rather than publishing an image that was never tested.

On the QEMU-emulated runner the following stay unavoidable: the runtime stage's
apt installs, the frontend stage's `npm ci`, formatting/type/test/build gates
and production bundle, the pinned yt-dlp virtual environment, and the Go
compile itself. Unchanged layers are reused by digest, so they only re-run when
their inputs change. Removing the reused Go test compile and execution from the
publish builds is expected to be the largest saving; no measured duration is
claimed here because this change has not run in CI yet.

The frontend is still built and tested inside the image rather than reused from
the checks job. Importing a `web/dist` artifact would create a second,
artifact-dependent build path and would remove the in-image
format/check/test/build gates; the npm download cache already covers the
cheapest part of that stage, so it was left unchanged.

### Runner selection

The workflows pick runners from repository variables. The default is the
existing self-hosted label `reelscarey-ci`.

| Variable | Used by | Default |
| --- | --- | --- |
| `REELSCAREY_RUNNER` | push/schedule/dispatch jobs in `ci.yml`, and publish/acceptance unless overridden | `reelscarey-ci` |
| `REELSCAREY_ACCEPTANCE_RUNNER` | live acceptance job (manual dispatch and nightly schedule) | `REELSCAREY_RUNNER` |
| `REELSCAREY_PUBLISH_RUNNER` | publish job (tag push or manual dispatch) | `REELSCAREY_RUNNER` |

Each value is one runner label, for example `reelscarey-publish` or
`ubuntu-latest`. Set the variables under Settings → Secrets and variables →
Actions → Variables. `pull_request` jobs in `ci.yml` always run on
`ubuntu-latest`, even when a variable is set, because pull requests can come
from forks and untrusted code must not run on a self-hosted machine. GitHub
does not expose repository secrets to fork pull requests, and the checks job
does not need any.

To add a second self-hosted runner:

1. Install the runner on a Linux x64 machine with the default labels
   `self-hosted`, `linux`, `x64` plus a custom label such as
   `reelscarey-publish` or `reelscarey-acceptance`.
2. Install Docker Engine with the Compose plugin and BuildKit/buildx; both
   workflows build through `docker/build-push-action`. An amd64 image on an
   ARM machine also needs QEMU/binfmt registration.
3. For an acceptance runner, additionally provide `qbittorrent-nox`, `ffmpeg`,
   `par2`, `unrar`, passwordless `sudo` (the cross-filesystem tmpfs test and
   package installs use it), Node 24 and Go. The live tests start disposable
   localhost clients and never an owner account.
4. Set `REELSCAREY_PUBLISH_RUNNER` or `REELSCAREY_ACCEPTANCE_RUNNER` to the
   custom label so checks, acceptance and publish stop serializing on one
   machine.
5. Keep Docker Hub credentials in the repository secrets. A self-hosted runner
   only receives them for trusted branch pushes, tags and manual dispatch, not
   for fork pull requests.

Hosted runners also work (`REELSCAREY_RUNNER=ubuntu-latest`) and install the
live-test packages from Ubuntu's archive, but hosted minutes, quota and billing
failures are reported separately from local checks.

