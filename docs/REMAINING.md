# Remaining project work

This checklist consolidates the open work after the Anime search implementation.
Earlier milestone sections in [ROADMAP.md](../ROADMAP.md) also record historical
scope limits. Implemented profile/monitoring bulk edits, explicit Anime numbering,
configured-collection discovery and registered-file rename/reorganization are no
longer missing features. Current acceptance evidence belongs in
[VALIDATION.md](VALIDATION.md); implementation and owner-service testing are
separate.

## Current milestone acceptance

- [x] Complete and record integrated application validation for version 0.17: clean frontend,
  backend/race/static checks, browser behavior, native/package recovery, generated
  media/real disposable client regressions, container startup/restart and a
  verified-backup preview upgrade. Do not use owner media or private accounts as
  automated fixtures. All application gates passed; the workflow's final package
  upload failed only because GitHub artifact storage is full. See VALIDATION.md.

## Deferred owner-service acceptance

- [ ] Test the owner's IPTorrents session directly in the app when the owner is
  ready. Keep cookies and matching user-agent values out of chat and Git.
- [ ] Test the owner's XSpeeds session on the same basis. Both private-account
  checks remain explicitly deferred; adapter and disposable tests do not replace
  them.
- [ ] Complete owner movie/TV/automation download-to-import acceptance with an
  enabled indexer and a correctly shared download mount. Torznab setup remains
  deferred and optional; direct trackers/RSS are supported without Prowlarr.
- [ ] Complete owner Newznab/SABnzbd/shared-mount acceptance after private setup.
- [ ] Complete owner existing-library registration/organization acceptance on
  explicitly chosen media. Generated-media checks remain separate evidence.
- [ ] Complete owner subtitle download acceptance once appropriate media is
  imported. The owner-reported OpenSubtitles connection is already recorded.

## Media management and organization

- [ ] Versioned replacement imports with reviewed recovery/rollback. Existing
  files currently remain protected; resolution upgrades are suggestions.
- [ ] Bulk season/episode preference edits beyond existing parent monitoring and
  quality-profile batches.
- [ ] Reviewed catalog/media removal with clear file-deletion choices.
- [ ] Filesystem-health inspection that distinguishes missing/changed files from
  saved registration state.
- [ ] A guided main-root migration. The main media directory is currently selected
  at deployment; changing it requires an offline complete-tree move.
- [ ] Whole-title folder moves including NFO/artwork/attachments and other extras.
  Current reorganization handles registered videos and recognized subtitle
  companions, preserving or blocking uncertain extras.
- [ ] Queued organization cancellation and rollback after apply. Failed saved
  jobs can be resumed with verification; there is no general batch rollback API.
- [ ] Arbitrary-root browsing and automatic bulk metadata matching, if added with
  explicit scope and identity review. Current discovery stays within configured
  collections and registration requires a reviewed match.

## Anime, metadata and media-server coverage

- [ ] AniDB/AniList or other Anime-specific provider identity and external
  alternate-order mapping. Current identities remain canonical TMDb plus explicit
  owner-entered title/absolute-number aliases.
- [ ] Broader reviewed Anime workflows for specials/OVA/extras and cross-season
  packs. Automatic searching accepts one exact non-special episode; manual
  shared-file imports remain bounded to consecutive canonical episodes from one
  season.
- [ ] Broader release grammar support where conservative matching currently
  requires manual search/review. No arbitrary filename or season-order inference.
- [ ] Acceptance of naming presets with real Plex, Emby and Jellyfin scans on
  representative libraries. Documented naming conventions are implemented; they
  are not proof of every server/version's identification behavior.
- [ ] Automatic metadata refresh and richer calendar/broadcast information.
  Wanted/Calendar currently project saved metadata and registration facts.

## Quality, subtitles and additional clients

- [ ] Verified audio-language inspection and richer source/codec scoring; release
  language text currently supplies selection evidence only.
- [ ] Additional subtitle providers, ASS-style provider downloads, subtitle
  replacement/resynchronization and per-show language overrides. Existing styled
  subtitle companions can be detected/preserved during organization; that is a
  different capability from provider download support.
- [ ] Usenet retention scoring, multiple SABnzbd clients and NZBGet support.
- [ ] More general multi-client management and arbitrary durable job types if
  required by later workflows; current downloads/searches already use durable
  leases and recovery.

## Deployment and public release

- [ ] Restore GitHub artifact retention capacity. Version 0.17 packages built
  and verified in CI, but account storage quota blocked upload; verified local
  packages remain available. No previous remote artifacts were removed.
- [ ] Independent security review and broader accessibility acceptance.
- [ ] Representative NAS/shared-storage filesystem and arm64 runtime acceptance.
  Cross-compiling an arm64 package does not establish runtime compatibility.
- [ ] Sustained workload/resource budgets. Container snapshots are point-in-time
  evidence rather than a long-running capacity measurement.
- [ ] Signed release provenance and a public distribution process. Existing
  private package checksums detect corruption but are not signatures.
- [ ] Owner license selection before public distribution.

Keep graphite/blue and the functional interface style during later milestones.
Private tracker testing should remain deferred until the owner chooses to resume
it; other independent project work can continue.
