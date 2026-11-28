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

Pending mandatory release-branch checks, Docker publication and live upgrade.
