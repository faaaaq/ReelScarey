# ReelScarey milestones

This is the current work queue, updated 2026-10-28. Docker is the supported
deployment. Checked boxes mean implemented; live acceptance is tracked separately.
See [ROADMAP.md](ROADMAP.md) for the broader roadmap, [remaining work](docs/REMAINING.md)
for limits, and [validation](docs/VALIDATION.md) for evidence.

## Before feature-complete live testing

- [ ] Connect downloaded movie and TV upgrades to reviewed replacement and
  rollback, preserving download/seeding sources and provenance.
- [ ] Connect newly acquired YouTube videos and selected subtitles to reviewed
  replacement and rollback, preserving both versions and source provenance.
- [x] Support embedded-title updates for MOV, AVI and WebM in addition to MP4,
  M4V and Matroska, with reviewed rollback. Every rewrite must preserve packets,
  timing, streams, chapters and non-title metadata; incompatible combinations
  are rejected. Transport streams cannot store these container title tags.
- [ ] Complete direct continuous IMDb list synchronization and private IMDb,
  TMDb and Trakt lists. Public TMDb/Trakt lists work; automatic IMDb sync uses
  MDBList and IMDb CSV is a one-time import.
- [ ] Add reviewed catalog/media removal with explicit file-deletion choices.
- [x] Add reviewed path rollback for completed file/title/folder organization
  moves, preserving current preferences for future imports. Modified media must
  first return to its previous version; occupied destinations are never replaced.
- [ ] Test the completed workflows with generated media and isolated services,
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
- [ ] Guided main-root migration; arbitrary-root browsing and bulk matching.
- [ ] More Anime providers and order refresh, broader release grammars and
  single-file cross-season mappings.
- [ ] Automatic metadata refresh, richer calendar data, YouTube subscriptions
  and bulk acquisition, additional subtitle providers and resynchronization.
- [ ] Richer quality/language scoring, more download clients and Usenet retention.
- [ ] NAS/shared-storage and ARM runtime acceptance, sustained load and broader
  accessibility/security review. Public distribution also needs signed releases
  and an owner-selected license.

The app currently runs locally with empty project media and download folders;
configure mounts before testing against an actual library. The checklist does not
claim owner-service acceptance or completion of unimplemented features.
