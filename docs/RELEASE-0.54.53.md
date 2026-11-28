# ReelScarey 0.54.53

- Add in-place artist, album and track editing with revision-checked partial
  updates, bounded track batches, artist/album artwork search and JPEG/PNG
  upload, provider-artwork restore and explicit filename edits. Optional audio
  tag writes use the durable edit journal and existing recovery/rollback flow;
  catalog-only saves leave registered audio tags unchanged.
- Schema migration 117 retains editor choices and administrator-selected
  artwork. Automatic metadata updates preserve those choices, including
  catalog-only genre/comment edits. Populated-database upgrade coverage restores
  the older trigger set before replaying migrations. The requested local
  `728e426` change is included with its two prerequisite editor commits.
- Verify Deezer artist identities against reviewed aliases instead of accepting
  a differently named artist from a shared album title. In particular,
  `(hed) p.e.` can no longer resolve to Boyz II Men merely because both have an
  album named `Evolution`.
- Consolidate reviewed artist aliases (`(hed) p.e.` / `(hed) Planet Earth` and
  `ABK` / `Anybody Killa`) across scans, discovery and provider lookups.
  Startup consolidates existing duplicates before music workers resume,
  preserving catalog/file/scan/playback/upgrade references and owner overrides.
  Conflicting albums, competing MusicBrainz identities and active work remain
  untouched with a startup-log reason. No media file is moved or retagged by
  this consolidation, and conflicting tagged artist identities fail closed.

## Validation

Local release-content validation passed a clean `npm ci`, frontend format and
TypeScript checks, all 87 frontend test files / 532 tests and the production
build. The full Go test suite, Go vet, application build and `git diff --check`
also passed against the release tree with the built frontend embedded.

## Publication and live deployment

Required release-branch checks passed in GitHub Actions run `46119327931`.
The first attempt reached the job timeout during post-action cleanup after all
checks had passed; the rerun completed green. Docker publication run
`46125802509` passed AMD64 and ARM64 candidate builds, runtime acceptance,
manifest assembly, final-tag pulls and provenance signing.

The verified multi-architecture manifest digest for both `0.54.53` and `latest`
is `sha256:7c33f3cc80b5bce09f90695fe1e5f4755c766d186409757dc6d6a139259c3c0c`,
built from release commit `f958f0b21f81e4b720e0bb73f96728c6341fddd6`.

The live Compose service was upgraded to the versioned tag and reports
`0.54.53`, schema `117` and Docker `healthy`. Its stopped-container configuration
backup is `/home/faaaaq/configs/reelscarey/config-backup-20261128024309`, with
Compose backup `/home/faaaaq/configs/reelscarey/compose.yml.bak-20261128024309`;
the backup passed SQLite integrity verification. Startup consolidated artist 4
into artist 3. The two `(hed)` albums and all 13 registered files remain attached
to artist 3, and `PRAGMA foreign_key_check` returned no violations.
