# Container publishing

Docker is the supported deployment target. The primary branch is
`codex/foundation`. The publisher uses the repository variable
`DOCKERHUB_USERNAME` and secret `DOCKERHUB_TOKEN`.

## Release procedure

1. Check the primary branch, open work, recent Actions runs and Docker tags.
   A Git tag or a version in source is not proof of a published image. Use a
   clean checkout of the current primary branch to prepare the next patch.
2. Increment the version in `web/package.json`, both root version entries
   in `web/package-lock.json`, `cmd/reelscarey/main.go` and the `Dockerfile`
   default. Record the changes and actual validation in `docs/RELEASE-<version>.md`.
3. Run the frontend format, type, test and build checks and the Go test, vet and
   build checks against the release content. Build the frontend before running
   Go because the binary embeds `web/dist`.
4. For routine fixes, publish after local gates pass. A change to migrations,
   auth or other security boundaries, downloader/importer/organization write
   paths, or public API contracts must pass the branch checks before publication.
   Run the nightly/manual acceptance suite for the affected live client, media
   or Docker behavior when needed. Use `gh workflow run ci.yml --ref codex/foundation`
   for a manual run on primary.
5. Use a single publishing trigger for the release. With Git tag access, push
   the tested primary commit and tag it `v<version>`. If the integration can
   write the primary branch but cannot create tags, use the exact primary-branch
   push commit subject `Release <version>`, matching `web/package.json`. For a
   core change, validate the content first on the branch, then publish that
   validated tree. Avoid pushing an exact release-subject commit and a Git tag
   for the same release.
6. Watch both architecture builds and the manifest job. Record the successful
   run and digest after the manifest job has pulled both the version tag and
   `latest` and verified that they resolve to the same multi-architecture
   digest. Record a failed publication separately from application validation,
   and check the registry or the last successful tag verification before
   assuming which version is available.
7. Once publication is verified, connect to scareypit over SSH with the
   dedicated acceptance key described in [AGENTS.md](../AGENTS.md). Back up
   `/home/faaaaq/configs/reelscarey/compose.yml` and the configuration/database,
   update the Compose image to the versioned Docker tag, pull and recreate the
   service, then verify its running version and readiness. Record deployment
   separately. If SSH is unavailable, record that the live upgrade is pending.

Public distribution and the project license remain owner decisions. Keep
visibility, billing and credentials as configured.

## Publish triggers and validation

| Trigger on the primary branch or its release commit | Docker tags produced |
| --- | --- |
| Ordinary primary-branch push | `sha-<full commit>` |
| Git tag beginning with `v` | Version from `web/package.json`, `latest`, and the commit tag |
| Primary-branch push with exact subject `Release <version>` | Version from `web/package.json`, `latest`, and the commit tag |
| Manual dispatch from primary | `latest` and the commit tag |

Use the versioned tag or digest for repeatable Compose installs and rollback.
The example Compose file defaults to `latest`. The owner deployment uses a
versioned tag. Manual dispatch promotes the selected primary revision to
`latest` without adding a versioned tag.

The publisher builds natively on Linux amd64 and arm64. Both builds run the
frontend gates. The amd64 candidate runs startup, Compose, backup/restore,
feature, automatic-library and storage-recovery smoke tests before its build is
pushed by digest. It then pulls that digest and verifies the revision label and
backup/restore. The arm64 candidate verifies startup, version, readiness,
persistent config and clean restart, then pushes by digest. The manifest job
combines the digests, checks for both platforms, and pulls each final tag from
the registry.

`Dockerfile` accepts `RUN_GO_TESTS` (default `1`), which runs `go test ./...`
and `go vet ./...` in the backend stage before the binary is built. On primary
pushes and dispatch, these checks run in the amd64 leg. On Git tag pushes, they
run in the arm64 leg. `ci.yml` does not run on tag pushes.

BuildKit adds provenance and an SBOM to each architecture image. The manifest
job signs the merged digest with the GitHub attestation action when the
repository is public or the owner explicitly enables private attestations with
`ENABLE_PRIVATE_ATTESTATIONS=true` after confirming Enterprise Cloud
eligibility. Verify the signing step before claiming a signed release.

## Runner selection

Linux application checks, Windows smoke and browser tests run in parallel.
After Linux checks pass on a scheduled or manually dispatched run, the full Go
race suite and the live client, generated-media and container acceptance jobs
run independently. The `acceptance` result requires both jobs to pass.

| Repository variable | Jobs | Default runner |
| --- | --- | --- |
| `REELSCAREY_RUNNER` | Trusted Linux application checks and the full Go race suite | `reelscarey-ci` |
| `REELSCAREY_ACCEPTANCE_RUNNER` | Live acceptance on schedules and dispatches | `ubuntu-latest` |
| `REELSCAREY_PUBLISH_RUNNER` | AMD64 Docker publishing | `ubuntu-latest` |

Use one label per override under Settings → Secrets and variables → Actions →
Variables. Pull request checks, Windows smoke, browser tests, ARM64 publishing
and manifest assembly use GitHub-hosted runners. A self-hosted acceptance or
publish runner needs Linux x64, Docker Engine, Compose and Buildx. Live
acceptance also uses passwordless sudo to install disposable test tools. Give
concurrent Docker jobs isolated daemons and network namespaces because smoke
scripts use fixed container names and localhost ports.

BuildKit keeps separate GitHub Actions cache scopes for the amd64 publisher
(`reelscarey-publish`), the arm64 publisher (`reelscarey-publish-arm64`), the
acceptance image (`reelscarey-ci`) and the Transmission fixture
(`reelscarey-transmission`). Cache failures fall back to a cold build. GitHub
scopes cache access by ref, so caches from one tag may be unavailable to
another. Within a publish job both builds reuse the same BuildKit worker.

Use [LIVE_TESTING.md](LIVE_TESTING.md) for acceptance. The storage recovery
fixture uses a [Docker volume subpath](https://docs.docker.com/engine/storage/volumes/#mount-a-volume-subdirectory)
to avoid direct access to Docker's private host directories.

