# Remaining project work

This checklist consolidates the open work after version 0.18's Anime provider
identity, reviewed order and manual special/multi-season pack implementation.
Earlier milestone sections in [ROADMAP.md](../ROADMAP.md) also record historical
scope limits. Implemented profile/monitoring bulk edits, explicit Anime numbering,
configured-collection discovery and registered-file rename/reorganization are no
longer missing features. Current acceptance evidence belongs in
[VALIDATION.md](VALIDATION.md); implementation and owner-service testing are
separate.

Docker is the sole supported deployment target. Historical native and Windows
results are evidence of past work, not ongoing acceptance requirements. The
owner's newly requested features are tracked in [ROADMAP.md](../ROADMAP.md).

## Current milestone acceptance

- [x] Complete and record integrated application validation for version 0.17: clean frontend,
  backend/race/static checks, browser behavior, native/package recovery, generated
  media/real disposable client regressions, container startup/restart and a
  verified-backup preview upgrade. Do not use owner media or private accounts as
  automated fixtures. All application gates passed; the workflow's final package
  upload failed only because GitHub artifact storage is full. See VALIDATION.md.
- [x] Complete and record integrated version 0.18 acceptance, including Linux
  native AniList access, generated real-client special/multi-season pack imports,
  restart/snapshot preservation, frontend, package/recovery and container gates.
  The installed Windows app also passed live AniList search through its API.
  All application gates passed; GitHub artifact storage quota alone blocked the
  final upload. See [ANIME_PROVIDER_ORDERS.md](ANIME_PROVIDER_ORDERS.md) and VALIDATION.md.

## Deferred owner-service acceptance

- [x] Verify native Windows AniList access through the installed application's
  authenticated API with a disposable account/configuration. Public search found
  Cowboy Bebop (AniList 1). The temporary Go test executable alone received an
  OS socket-permission error; no network/security control was changed or bypassed.
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
- [x] Reviewed whole-title folder moves across filesystems, including NFO,
  artwork, attachments, nested extras and empty directories. Large reviews are
  bounded and an uncertain stage may require manual review.
- [ ] Queued organization cancellation and rollback after apply. Failed saved
  jobs can be resumed with verification; there is no general batch rollback API.
- [ ] Arbitrary-root browsing and automatic bulk metadata matching, if added with
  explicit scope and identity review. Current discovery stays within configured
  collections and registration requires a reviewed match.

## Anime, metadata and media-server coverage

- [x] AniList identity links and owner-reviewed release-order labels, with
  read-only TMDb group/subgroup proposals joined to exact canonical episode IDs.
  Existing title/absolute aliases remain preserved. Provider identity alone
  never infers an episode order. Integration acceptance remains listed above.
- [x] Manual reviewed Anime specials/OVA and multi-season packs using explicitly
  selected canonical targets and separate files, immutable queued order evidence
  and mandatory file mapping. Each shared file remains bounded to at most twenty
  consecutive canonical episodes from one season.
- [x] Automatic provider-mapped release matching, including separately opted-in
  specials and packs. Saving an external identity or order alone does not enable
  automatic searches. Linux generated-media and container acceptance is recorded
  in [VALIDATION.md](VALIDATION.md).
- [ ] AniDB or additional Anime identity/order providers and an explicit refresh
  workflow for already-linked AniList facts. Current linked identity facts are
  cached; remove/apply then relink to fetch again.
- [ ] One-file cross-season Anime mappings, broader extras and unsupported release
  grammars. Current multi-season support applies to separate reviewed files,
  not arbitrary franchise flattening or inferred canonical season offsets.
- [ ] Broader release grammar support where conservative matching currently
  requires manual search/review. No arbitrary filename or season-order inference.
- [ ] Acceptance of naming presets with real Plex, Emby and Jellyfin scans on
  representative libraries. Documented naming conventions are implemented; they
  are not proof of every server/version's identification behavior.
- [ ] Automatic metadata refresh and richer calendar/broadcast information.
  Wanted/Calendar currently project saved metadata and registration facts.

## Quality, subtitles and additional clients

- [x] Reviewed YouTube source inspection and individual-video acquisition for
  existing movies/episodes and custom collections, with selected VTT subtitles.
  See [YouTube sourcing](YOUTUBE.md) and [validation](VALIDATION.md) for limits.
- [ ] Automatic channel/playlist subscriptions, bulk video acquisition and richer
  subtitle selection remain future work.
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

- [ ] Restore GitHub artifact retention capacity if hosted artifacts are needed.
  The approved oldest artifact was removed, but quota still blocked the latest
  uploads. See [VALIDATION.md](VALIDATION.md) for the recorded results.
- [ ] Independent security review and broader accessibility acceptance.
- [ ] Representative NAS/shared-storage filesystem and arm64 runtime acceptance.
  Cross-compiling an arm64 package does not establish runtime compatibility.
- [ ] Sustained workload/resource budgets. Container snapshots are point-in-time
  evidence rather than a long-running capacity measurement.
- [ ] Signed container release provenance and a public distribution process.
- [ ] Owner license selection before public distribution.

Keep graphite/blue and the functional interface style during later milestones.
Private tracker testing should remain deferred until the owner chooses to resume
it; other independent project work can continue.
