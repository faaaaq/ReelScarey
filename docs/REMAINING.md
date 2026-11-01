# Remaining project work

The current milestone queue is summarized in [MILESTONES.md](../MILESTONES.md).

This checklist consolidates the open work after version 0.26's additional providers
and reference-subtitle alignment.
Earlier milestone sections in [ROADMAP.md](../ROADMAP.md) also record historical
scope limits. Implemented profile/monitoring bulk edits, explicit Anime numbering,
configured-collection discovery and registered-file rename/reorganization are no
longer missing features. Current acceptance evidence belongs in
[VALIDATION.md](VALIDATION.md); implementation and owner-service testing are
separate.

Docker is the sole supported deployment target. Historical native and Windows
results are evidence of past work, not ongoing acceptance requirements. The
owner's newly requested features are tracked in [ROADMAP.md](../ROADMAP.md).

OCR, translation and audio-based alignment were cancelled and removed on
2026-10-31. The active scope is completion and verification of existing workflows.
Optional integrations below are not an implementation queue without a new request.

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

- [x] Safely update embedded movie and episode titles during reviewed organization,
  preserving streams, chapters, attachments, sidecars, seeding sources and rollback.
  Reviewed MP4/M4V, Matroska, MOV, AVI and WebM updates preserve a rollback copy and can be queued
  automatically after selected-file and whole-folder moves, with one rollback
  copy per registered video. Rewrites that change stream or packet facts are
  rejected; TS/M2TS container title tags remain unsupported.

- [x] Connect manually selected movie/TV upgrades and YouTube videos to reviewed
  versioned replacement. Source provenance and canonical mappings survive rollback;
  selected VTT subtitles are copied without replacing existing sidecars.
  Automatic upgrade searches remain suggestions.
- [x] Bulk season and episode monitoring. Select up to 100 seasons per show or
  100 loaded episodes per season for an atomic, revision-checked update.
- [x] Reviewed catalog removal, keeping files or moving registered media and
  recognized subtitles to retained trash. Permanent trash purging is not provided.
- [x] Filesystem-health inspection of registered files. Organize library can
  check selected files on one page, reporting missing, changed and unsafe paths.
- [x] Guided offline main-root migration with reviewed full-tree inventory,
  verified safety backup, resumable copy, retained originals and mount activation.
- [x] Reviewed whole-title folder moves across filesystems, including NFO,
  artwork, attachments, nested extras and empty directories. Large reviews are
  bounded and an uncertain stage may require manual review.
- [x] Reviewed path rollback for completed organization moves, including whole
  folders and extras. Current future-import preferences are preserved. Changed
  versions must be restored first; failed jobs still require verified recovery.
- [x] Bulk metadata suggestions and atomic reviewed registration within configured
  collections. Suggestions never choose identities automatically.
- [x] Read-only browsing of additional explicitly mounted roots. Discovery and
  registration stay within configured library collections; see [Storage](STORAGE.md).

## Anime, metadata and media-server coverage

- [ ] Live acceptance of Emby activity with the owner's server. Opt-in polling
  records observed playback sessions; short plays and monitoring gaps can be
  missed and are not represented as complete server-side event history.

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
- [x] Explicit reviewed refresh of already-linked AniList facts, preserving saved
  episode mappings and canonical identities.
- [x] Kitsu identity search, links and reviewed fact refresh, with provider-scoped
  IDs and existing canonical order review. See [Anime providers](ANIME_PROVIDER_ORDERS.md).
- [ ] AniDB and other provider-specific integrations beyond AniList/Kitsu.
- [ ] One-file cross-season Anime mappings, broader extras and unsupported release
  grammars. Current multi-season support applies to separate reviewed files,
  not arbitrary franchise flattening or inferred canonical season offsets.
- [ ] Broader release grammar support where conservative matching currently
  requires manual search/review. No arbitrary filename or season-order inference.
- [x] Standard TV 1x02, spaced S01 E02 and Season 1 Episode 2 labels, with exact
  title/year/season/episode checks and no automatic range/pack interpretation.
- [x] Explicit S01EP02, S01 Episode 02 and Season 1 Ep 2 labels plus bounded
  consecutive long-form/spaced range/chained filename suggestions. Mapping still
  requires review; ordinary automation accepts only a single canonical episode.
- [ ] Acceptance of naming presets with real Plex, Emby and Jellyfin scans on
  representative libraries. Documented naming conventions are implemented; they
  are not proof of every server/version's identification behavior.
- [x] Paused-by-default metadata refresh schedules and richer local calendar facts.
  Calendar shows cached runtime, series status and metadata freshness.

## Quality, subtitles and additional clients

- [x] Reviewed YouTube source inspection and individual-video acquisition for
  existing movies/episodes and custom collections, with selected VTT subtitles.
  See [YouTube sourcing](YOUTUBE.md) and [validation](VALIDATION.md) for limits.
- [x] Paused channel/playlist subscriptions and reviewed bulk video acquisition,
  with explicit subtitle options and final import review.
- [x] Ordered source/codec scoring and preferred release terms.
- [ ] Language verification beyond embedded tags.
  Imported files expose audio stream tags; older files can be explicitly inspected.
  Tags may be absent or inaccurate, and release text still supplies selection evidence.
- [x] Per-show subtitle overrides and reviewed sidecar replacement, timing shifts
  and retained-version rollback for SRT/VTT/ASS/SSA.
- [x] Styled ASS/SSA and VTT provider downloads with explicit original-format
  preference, bounded cue validation, format-preserving recovery and reviewed
  edits/rollback. SRT remains the default. See [Subtitles](SUBTITLES.md).
- [x] SubDL alternative provider, explicit file review, original formats and
  provider-preserving interrupted imports. See [Subtitles](SUBTITLES.md).
- [x] Reference-subtitle alignment for offset and small uniform drift, with
  confidence checks, explicit review and retained-version rollback.
Further subtitle providers are optional extensions, outside the current completion scope.
- [x] Usenet retention and propagation filters with explicit publication evidence.
- [x] Multiple named qBittorrent/SABnzbd clients and NZBGet support, explicit
  per-protocol defaults and immutable per-job folder mappings. See
  [Download clients](DOWNLOAD_CLIENTS.md). Owner-service acceptance remains separate.
- [x] Transmission 4 named torrent clients with atomic ownership labels, uncertain
  submission reconciliation, reviewed TV imports and retained seeding sources.
- [ ] Additional client protocols or arbitrary durable job types if required by
  later workflows; downloads/searches already use durable leases and recovery.

## Requests and Discord

- [x] Private TMDb/Trakt synchronization with encrypted per-user access tokens.
- [ ] Direct continuous/private IMDb synchronization requires a supported list
  feed. The multi-step export has no stable export URL. IMDb via MDBList syncs
  automatically and CSV imports remain manual snapshots. See [REQUESTS.md](REQUESTS.md).
- [ ] Live acceptance with owner-selected provider lists and a Discord webhook.
  Existing validation uses isolated provider/Discord fixtures only.
- [ ] Live Discord companion bot acceptance with an owner-configured application,
  public HTTPS endpoint, linked accounts and download services. Signed commands,
  interactive selections and administrator notifications have fixture coverage.

## Deployment and public release

- [ ] Independently validate franchise associations and timeline behavior on a
  representative owner library. Automatic discovery uses community-maintained
  Wikidata statements and may be incomplete. The timeline backfills saved facts
  but cannot reconstruct intermediate transitions that were never recorded.

- [x] Remove standalone package and artifact upload steps from CI; Docker is the
  supported deployment target.
- [ ] Independent security review and broader accessibility acceptance.
- [ ] Representative NAS/shared-storage filesystem and arm64 runtime acceptance.
  Cross-compiling an arm64 package does not establish runtime compatibility.
- [ ] Representative production workload/resource sizing. The repeatable bounded
  read/write scenario in [WORKLOAD.md](WORKLOAD.md) does not establish long-term
  capacity on owner hardware, shares or large media operations.
- [x] Five-minute constrained container read/write baseline, latency/memory/process
  budgets and post-workload restart/integrity checks; see [validation](VALIDATION.md).
- [ ] Signed container release provenance and a public distribution process.
- [ ] Owner license selection before public distribution.

Keep graphite/blue and the functional interface style during later milestones.
Private tracker testing should remain deferred until the owner chooses to resume
it; other independent project work can continue.
