# ReelScarey milestones

This is the current work queue, updated 2026-10-31. Docker is the supported
deployment. Checked boxes mean implemented; live acceptance is tracked separately.
See [ROADMAP.md](ROADMAP.md) for the broader roadmap, [remaining work](docs/REMAINING.md)
for limits, and [validation](docs/VALIDATION.md) for evidence.

## Current completion scope

The existing implementation milestones are complete except direct/private IMDb
synchronization, which lacks the supported feed needed by the existing adapter.
Owner-service acceptance, external review, representative hardware testing and
release/license requirements remain open as listed below. Optional extensions
are not authorized by a request to finish the existing milestones.

The experimental OCR, translation and audio-alignment branch work was removed at
the owner's request on 2026-10-31; none was deployed. The existing reference-text
subtitle alignment and manual timing editor remain unchanged.

- [x] Repair existing navigation titles and keyboard focus: Escape returns focus
  from a closing mobile menu, route changes focus the main content, and existing
  management pages have descriptive browser titles. Remove the stale version label.
  This targeted verification does not establish a full accessibility audit.

## Before feature-complete live testing

- [x] Administration backup/restore manager for configuration and the catalog:
  live snapshots, schedules, retention, ZIP download/upload, safety backups,
  controlled restart and interrupted-restore rollback. Media backups stay separate.

- [x] Connect downloaded movie and TV upgrades to reviewed replacement and
  rollback, preserving download/seeding sources and provenance.
- [x] Connect newly acquired YouTube videos and selected subtitles to reviewed
  replacement and rollback, preserving both versions and source provenance.
- [x] Support embedded-title updates for MOV, AVI and WebM in addition to MP4,
  M4V and Matroska, with reviewed rollback. Every rewrite must preserve packets,
  timing, streams, chapters and non-title metadata; incompatible combinations
  are rejected. Transport streams cannot store these container title tags.
- [x] Private TMDb/Trakt list synchronization with encrypted, account-owned user
  tokens. Expired tokens require replacement by the owner.
- [ ] Direct continuous/private IMDb lists require a supported list feed.
  IMDb's multi-step exports have no stable export URL. Automatic MDBList sync
  and one-time IMDb CSV imports remain available.
- [x] Reviewed catalog removal with keep-files or retained-trash choices.
  Dependent catalog history is removed; client sources and backups are retained.
- [x] Add reviewed path rollback for completed file/title/folder organization
  moves, preserving current preferences for future imports. Modified media must
  first return to its previous version; occupied destinations are never replaced.
- [x] Test the completed workflows with generated media and isolated services,
  including interruption, collision, recovery and Docker restart behavior.

## Already implemented; requires owner-service live acceptance

- [x] User request accounts, approvals, public list imports, Discord webhook
  notifications and companion-bot interaction flows.
- [x] Unmatched-file review, per-series release preferences, franchise discovery,
  destination-folder creation and the event timeline.
- [x] Reviewed whole-folder moves with extras and cross-filesystem copying;
  verified embedded-title updates for MP4/Matroska with rollback copies.
- [x] Individual YouTube acquisition for existing and custom items, including
  selected VTT subtitles.
- [ ] Test real configured indexers, download clients, shared mounts, library
  registration, subtitles, lists and Discord webhook/bot using owner-selected
  test data. Private-tracker testing remains deferred until the owner chooses it.
- [ ] Validate franchise/timeline behavior and naming with representative media
  server libraries. Existing fixture checks do not establish real-server results.

## Later and optional scope

- [ ] Live Emby reporting/acceptance, explicitly deferred by the owner.
- [x] Reviewed bulk matching, metadata refresh/calendar enrichment, AniList fact
  refresh, per-show subtitles, reviewed subtitle edits/rollback, YouTube
  subscriptions/bulk acquisition and source/codec/Usenet age rules. See below.
- [x] Guided offline main-root migration with verified backup/copy, resumable
  publication, retained originals, mount activation and recovery review.
- [x] Read-only browsing of media/downloads and explicitly mounted extra roots.
- [x] Additional explicit standard-TV release labels: 1x02, spaced S01 E02 and
  Season 1 Episode 2, preserving single-episode and ambiguity checks.
- [x] Five-minute constrained container read/write workload and restart budget:
  1,000 synthetic titles, eight readers, settings writes and sampled resources.
- [x] Kitsu as an additional Anime identity provider, with reviewed links/refresh.
- [ ] AniDB, broader release grammars and single-file cross-season mappings.
- [x] SubDL subtitles and reviewed reference-based timing alignment.
- [x] Original ASS/SSA/VTT provider subtitles, style-preserving recovery and
  reviewed edits/rollback. SRT remains the default.
- [x] More explicit single-episode TV labels and bounded consecutive filename
  range/chain suggestions, sharing one parser and retaining mandatory review.
- [x] Named qBittorrent/SABnzbd/NZBGet clients, per-protocol defaults and saved
  per-job folder mappings. See [Download clients](docs/DOWNLOAD_CLIENTS.md).
- [x] Transmission 4 named torrent clients, with reviewed imports and recovery.
- [ ] Language verification beyond stream tags and further client protocols.
- [ ] NAS/shared-storage and ARM runtime acceptance and broader
  accessibility/security review. Public distribution also needs signed releases
  and an owner-selected license.

The app currently runs locally with empty project media and download folders;
configure mounts before testing against an actual library. The checklist does not
claim owner-service acceptance or completion of unimplemented features.

## Version 0.22 follow-through

All seven follow-through features are implemented: scheduled metadata refresh
and richer calendar facts, reviewed refresh of linked AniList identities,
per-show subtitle preferences, reviewed subtitle timing/replacement/rollback,
YouTube subscriptions and bulk acquisition, source/codec scoring with Usenet age
filters, and reviewed bulk metadata matching. See [feature behavior and bounds](docs/SEVEN_FEATURES.md).
Provider credentials, private-service acceptance and the license still require
owner decisions or access where listed in the remaining-work record.

## Version 0.23 storage and reliability follow-through

See [Storage](docs/STORAGE.md) for the read-only folder browser and guided offline
disk migration. The latter retains the container media path, original disk and
catalog IDs. [Workload checks](docs/WORKLOAD.md) define a repeatable constrained
container scenario; representative hardware and long-term capacity remain separate.

## Version 0.26 completed scope

Transmission 4 named torrent clients, Kitsu Anime identity links/refresh, SubDL
subtitle downloads and reviewed reference-text timing alignment are implemented.
The release workflow is prepared for versioned image tags, SBOM/provenance and
conditional signing. Actual signed publication remains outstanding until its
repository/account requirements and owner license decision are satisfied; see
[release process](docs/RELEASING.md). Optional extensions are not part of the current completion scope.

## Version 0.28 movie versions

- [x] Attach multiple existing movie files without duplicate-title rejection.
- [x] Opt-in HD and 4K targets with independent quality profiles and downloads.
- [x] Explicit additional-edition acquisition, distinct filenames and file status.
- [x] Per-file replacement/rollback and version-aware organization preserve other copies.

See [movie versions](docs/MOVIE_VERSIONS.md) and [executed validation](docs/VALIDATION.md).

## Version 0.29 UI, discovery and matching

- [x] Emby-inspired light/dark shell, collapsed navigation and command palette.
- [x] Reviewed existing-library discovery with automatic matching and explicit exceptions.
- [x] Folder-first identity, title-only episode fallback and guarded season refresh.
- [x] Optional registration of unverified videos with honest labels (schema 045).

See [existing-library behavior](docs/EXISTING_LIBRARY.md) and [UI](docs/UI.md).

## Version 0.30 inspected quality and title records

- [x] Filenames carry inspected resolution plus HDR/SDR transfer tags.
- [x] FFprobe records HDR10/HLG transfer; migration 046 projects `video_hdr`.
- [x] Multiversion suffixes combine quality and version without duplication.
- [x] Embedded-title updates replace the original in place and record the
  previous title, sizes and checksums instead of retaining a duplicate video.

See [existing-library behavior](docs/EXISTING_LIBRARY.md) and [API](docs/API.md).

## Version 0.31 built-in torrent client

- [x] The Docker image ships qbittorrent-nox and the application supervises it.
- [x] The built-in connection starts paused; the torrent default routes jobs and
  existing jobs never change clients.
- [x] The daemon writes only inside `downloads/builtin`; its API stays on the
  container loopback. The peer port publishes as `REELSCAREY_QBT_PORT`.
- [x] Hardened installs opt into a writable download folder with
  `DOWNLOADS_READ_ONLY=false`; enabling without it fails honestly.

See [download clients](docs/DOWNLOAD_CLIENTS.md).

## Version 0.32 manual torrent uploads

- [x] Attach an uploaded .torrent file to a movie or series/season as the
  selected release, queued through the normal verified pipeline.
- [x] Duplicated torrents refused by infohash; v2-only metadata refused honestly.
- [x] Season packs bind the season's selected missing episodes; explicit
  episode numbers bind exactly those; import mapping review unchanged.

See the release endpoints in [API](docs/API.md).

## Version 0.34 IRC announce feeds

- [x] Encrypted IRC sources for IPTorrents, TorrentLeech, FileList, BeyondHD,
  AnimeBytes and MyAnonamouse using their real announce formats.
- [x] Bounded parsed history with tracker links; raw lines and passwords are
  never stored in the event list.
- [x] Sources can be paused, edited and removed; a failing connection shows
  its error.
- [ ] Automatic grabbing and download-URL fetching are the next step.

See [announce feeds](docs/ANNOUNCE.md).

## Version 0.33 Mullvad VPN kill switch

- [x] Encrypted Mullvad WireGuard configuration with a route-suppressed tunnel.
- [x] The built-in torrent client binds to the tunnel interface and stops
  whenever the tunnel is down; metadata and the UI keep the normal network.
- [x] NET_ADMIN retention is opt-in; the private key is encrypted, never
  returned, and redacted from tool output.
- [x] A clear kill-switch toggle stops the built-in client when the tunnel is
  down; turning it off allows unbound torrent traffic with an explicit warning.
