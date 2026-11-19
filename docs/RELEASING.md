# Container publishing

Docker remains the only supported deployment target. The primary branch is
`codex/foundation`. Its Docker Hub workflow uses the existing repository variable
`DOCKERHUB_USERNAME` and secret `DOCKERHUB_TOKEN`; it does not create accounts or
change repository visibility. Manual dispatch is restricted to the primary ref.

The workflow runs on every push to `codex/foundation`, on `v*` tags and on a
manual dispatch from the primary ref. It reads the application version from
web/package.json and embeds it in the binary and OCI labels. Branch pushes
publish the immutable `sha-<full commit>` candidate tag; `latest` (the
compose.yaml default) moves only on an explicit release event — a `v*` tag
or a manual dispatch. BuildKit attaches maximal build provenance and an SBOM.
Use the digest or commit tag for review and rollback; `latest` moves only on
successful release publication (a `v*` tag or dispatch). The release-candidate
smoke suite therefore runs on every primary-branch push, so keep that branch
releasable; a failed run blocks that publication and leaves the previous
`latest` in place.

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
qBittorrent/SABnzbd/NZBGet/FFmpeg and container acceptance suite runs nightly and on manual dispatch.
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
unchanged. Use [LIVE_TESTING.md](LIVE_TESTING.md) and compose.yaml to install.

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
Within one publish job the runner reuses its BuildKit worker state
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

Linux application checks start alongside the Windows smoke and browser tests.
On scheduled and manual runs, a successful Linux checks job starts two jobs in
parallel. One runs the full Go race suite. The other runs live client, media
and container acceptance. The `acceptance` check requires both to pass.

| Repository variable | Jobs | Default runner |
| --- | --- | --- |
| `REELSCAREY_RUNNER` | Linux checks on trusted pushes, schedules and dispatches, plus the full Go race suite | `reelscarey-ci` (self-hosted Linux x64) |
| `REELSCAREY_ACCEPTANCE_RUNNER` | Live acceptance on schedules and dispatches | `ubuntu-latest` |
| `REELSCAREY_PUBLISH_RUNNER` | AMD64 Docker publishing on primary branch pushes, version tags and primary branch dispatches | `ubuntu-latest` |

Each override takes one runner label under Settings → Secrets and variables →
Actions → Variables. Set a dedicated acceptance or publish override only to
change that job's runner. Pull request checks use GitHub-hosted runners.
Windows smoke, browser tests, ARM64 publishing and manifest assembly keep their
own hosted runner selections.

For a self-hosted live acceptance or AMD64 publish override, register a Linux
x64 runner with a distinct label and Docker Engine, Compose and Buildx.
Live acceptance also uses passwordless sudo to install its disposable client
and media tools. Give concurrently executing Docker jobs separate Docker
daemons and network namespaces because the smoke scripts use fixed container
names and localhost ports.
