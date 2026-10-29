# Usenet downloads (0.11 development)

Newznab sources and SABnzbd share the existing library, release search, quality
profiles, Activity, reviewed TV mappings and verified importer. Prowlarr is not
required. SABnzbd and an NNTP provider remain external services, not bundled apps.

## Setup

1. Configure your NNTP service in SABnzbd and choose its completed downloads folder.
2. Create category **reelscarey**, Folder/Path **ReelScarey** (relative to completed
   downloads), no script, and disable sorting for this category. Retain completed
   history until imports finish. Save/test checks category existence; output
   containment is checked before importing.
3. Mount that completed folder at ReelScarey's configured Downloads root. The API
   does not grant remote filesystem access. Both download clients, if connected,
   must map their configured remote root to this shared local Downloads root.
4. Settings → SABnzbd: enter its Web UI base URL (including a proxy prefix if
   needed, without `/api`), **full API key** from Config → General (not the
   NZB-only key), and completed folder as SABnzbd sees it. Save & test verifies
   version/category. Enter credentials privately in the app.
5. Indexers → Add → **Newznab (Usenet)**: enter the complete API endpoint, usually
   ending in `/api`, and API key. Save/test verifies `caps` text search. Queries
   use generic `search` with `q`, movie/TV categories 2000/5000.

Secrets, NZBs and private URLs are encrypted and omitted from responses/logs.
Blank secret fields retain saved keys; a changed URL requires a key. Active jobs
prevent changing/disconnecting their own client. A torrent job does not prevent
changing an idle SABnzbd connection, or vice versa.

## Download and import

Find releases for a movie or episode/season. Results show source and protocol.
Torrents use qBittorrent; NZBs use SABnzbd, without conversion/fallback. Usenet has
no peer/seeder counts, and torrent minimum-seeder rules do not apply. Identity,
resolution, language labels, size and term rules still apply. Automation starts
paused and download mode only chooses releases with a connected matching client.

NZBs must be bounded XML article descriptors (four MiB maximum). The app uploads
the descriptor, not its private indexer URL. Submission explicitly uses category
reelscarey, no script, repair/unpack without requesting source archive deletion,
and unique job name `reelscarey-<job id>`. Original releases/sources remain in
ReelScarey's history and subtitle evidence. Client ID/progress persist; SABnzbd 4
prefixed IDs and SABnzbd 5 UUIDs are supported.

Import requires history Completed and no loaded post-processing. 100% progress,
verification, repair, extraction, moving or failed history cannot authorize it.
Raw client error details are not echoed because they can include secrets.
Final storage must map to exact folder `ReelScarey/reelscarey-<job id>` under the
completed root; SABnzbd may report that folder or a single video inside it.
Changed/sorted/renamed output blocks import until restored. Discovery refuses
links, bounds nesting to 12 separators, 500 directories/20,000 entries/500 videos,
and excludes hidden paths, samples, trailers, archives, empty/unsupported files.
File choices freeze before review, preventing later files from shifting indexes.

One movie video may proceed automatically; multiple videos require a file choice.
TV always needs explicit episode mapping, including automatic downloads. The
shared importer verifies containment, size, FFprobe and hashes, journals
publication, refuses overwrite and resumes across restart. Completed sources
remain in place. ReelScarey never deletes client jobs, history or source files;
keep client-side cleanup from removing them before import.

## Recovery and limits

The encrypted NZB's SHA-256 with `nzb:` prefix is stored in legacy field
`info_hash`. This identifies the descriptor, not a torrent or video hash.
Identical descriptors deduplicate; differently serialized equivalent articles
may not. Shared target reservations independently prevent concurrent duplicates.

Submission intent is saved before upload. Lost replies reconcile by exact job
name/category; a known client ID must still match. Duplicate/changed identities
block tracking. Missing/uncertain submissions are never automatically resubmitted,
including after retry/restart. Restore original name/category/history, then retry
tracking. Listings are bounded to 1,000 matching queue/history records and four
MiB. Definitively rejected submissions can retry after correcting the client.
Repair/retry failed downloads in SABnzbd, retain job/history, then retry tracking.
Stopping tracking leaves the client/files unchanged. Confirmed journaled imports
can recover without contacting the client again.

Not included: NNTP account administration, arbitrary caller-supplied NZBs/URLs,
multiple SABnzbd clients, NZBGet, article-level retry, retention scoring, source
cleanup or replacement imports. Connected APIs are separate from owner shared-
mount acceptance. IPTorrents/XSpeeds account tests remain deferred.

References: [Newznab API](https://newznab.readthedocs.io/en/latest/misc/api.html),
[SABnzbd API](https://sabnzbd.org/wiki/configuration/4.5/api), and
[SABnzbd 5.1.3](https://github.com/sabnzbd/sabnzbd/releases/tag/5.1.3).
The pinned/checksummed SABnzbd release is a disposable test runtime only, not an
application dependency or a private package component.
