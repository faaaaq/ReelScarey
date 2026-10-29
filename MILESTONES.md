# ReelScarey milestones

This is the current work queue, updated 2026-10-29. Docker is the supported
deployment. Checked boxes mean implemented; live acceptance is tracked separately.
See [ROADMAP.md](ROADMAP.md) for the broader roadmap, [remaining work](docs/REMAINING.md)
for limits, and [validation](docs/VALIDATION.md) for evidence.

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
- [ ] More Anime providers, broader release grammars and
  single-file cross-season mappings.
- [ ] Additional subtitle providers, OCR, translation and automatic alignment.
- [x] Named qBittorrent/SABnzbd/NZBGet clients, per-protocol defaults and saved
  per-job folder mappings. See [Download clients](docs/DOWNLOAD_CLIENTS.md).
- [ ] Language verification beyond stream tags and additional client protocols.
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
