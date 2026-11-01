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
verify backup/restore, and inspect the actual workflow result. Hosted runner
billing, quota or permissions failures must be reported separately from local
checks. Public distribution still requires the owner's license choice. Do not
silently select a license, enable a paid plan or change visibility to clear a gate.

The Ubuntu x86-64 live-testing release builds a local candidate and runs backup,
existing-workflow and storage-recovery acceptance before publishing. After login,
it pushes the image, pulls the exact registry digest and verifies its source label
and backup/restore behavior. Existing repository visibility and credentials stay
unchanged. Use [LIVE_TESTING.md](LIVE_TESTING.md) and compose.server.yaml to install.

The storage acceptance mounts a named-volume subdirectory through Docker's
[documented volume-subpath option](https://docs.docker.com/engine/storage/volumes/#mount-a-volume-subdirectory),
so the calling user never needs access to Docker's private host directories.
