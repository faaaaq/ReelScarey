# Music catalog, import and organization


## Recovery and scan performance

Music submissions reserve a job before calling the download client. If a
response is lost or ReelScarey restarts, Retry tracks the same torrent hash
and job tag or the saved Usenet identity. Stop tracking releases the
reservation without changing the client's queue.

Imports save the staged file checksum before publishing, then commit each
registration with its import progress. On restart the worker can finish
registering its own staged output. A path conflict keeps the downloaded source
and staged copy intact for review.

Rollback verifies the edited file, the retained original and the catalog
entry. It refuses an occupied original path and can continue an interrupted
rollback from its saved phase. Edits created before 0.39.9 do not have the
required checksums and must be restored manually from verified originals.

The music schedule skips albums with unresolved download jobs, rotates its
batch through the wanted catalog, and rechecks the schedule, monitoring and
quality preferences when reserving each download. A rescan reuses tags and
hashes only when the saved file observation still matches.
Status: **implemented end to end.** The Music tab catalogs MusicBrainz artists,
albums and tracks, scans and registers existing files, applies reviewed
organization and tag rewrites with rollback, searches audio releases, queues
them through the selected download client, reconciles them with a leased worker,
imports the completed folder, and runs wanted-album searches on a schedule.

## What works

- Search MusicBrainz artists and add one idempotently.
- Browse saved artists, release groups (albums) and on-demand album tracks.
- Scan a folder below the media root:
  - bounded walk (20,000 audio files, 24 folder levels, no symlinks);
  - FFprobe tag reading for MP3, FLAC, M4A/MP4, OGG/Opus, WAV, AIFF and WMA;
  - SHA-256 evidence (up to 512 MiB per file);
  - matching against the local catalog by MusicBrainz release-group ID or by
    album title plus artist name;
  - review of matched and unresolved files.
- Discover missing catalog entries for selected unresolved files through an
  explicit reviewed action. It uses a release-group ID, or one exact normalized
  artist name and album title, creates only the matching artist/album, loads the
  album tracks and re-matches those files. Ambiguous or missing evidence stays
  unresolved.
- Register matched files in place. Registration verifies the file size and
  modification time and records `music_files`; it never moves a file.
- Search configured indexers with the audio category, parse release names and
  rank them by album/artist match, format and seeders. Results link to the
  tracker; automatic queueing is not implemented.
- Import a completed album folder below the downloads root: probe each file,
  map a complete one-to-one set to the catalog tracks, copy and tag into the
  music root, publish without overwrite and register `music_files`. Sources are
  never modified.
- Queue a searched release through the configured client; a leased worker
  reconciles torrent/Usenet progress, imports the completed folder, and marks
  the download completed or blocked.
- A blocked download exposes its completed files and accepts an explicit
  file-to-track mapping, then resumes the normal verified import.
- Audio preferences (`allow_lossless`, minimum bitrate, preferred formats)
  filter automatic choices.
- Wanted albums (monitored with no files) and an interval schedule in search or
  download mode.
- Preview and apply reviewed organization:
  - destination `root/Artist/Album/NN Title.ext` (or `D-NN` for multi-disc
    albums);
  - actions `move`, `retag` or `move_retag`;
  - stale plans return `409` and must be previewed again.
- Write catalog tags with FFmpeg without re-encoding:
  - artist, album artist, album, title, track/total, disc/total, year;
  - MusicBrainz artist, release-group, track and recording IDs;
  - a previous copy is retained before a tag rewrite;
  - the written tags are verified with FFprobe before publication;
  - publication refuses an existing destination.
- Roll back a completed or failed edit, restoring the file and its previous
  tag record.

## Provider and bounds

- Fixed provider: `https://musicbrainz.org/ws/2`. No credentials are stored.
- Requests send a descriptive User-Agent and are spaced at least one second
  apart. A `503`/`429` response becomes a rate limit with `Retry-After`; it is
  never silently retried.
- Responses are bounded (2 MiB search/artist, 4 MiB release browse, 8 MiB release
  tracks). Redirects are refused.
- Discographies are browsed rather than read from the 25-item lookup include.
  At most 1,000 release groups are stored per artist.
- Album tracks come from the earliest **Official** release in the group. Edition
  selection is not implemented.
- Cover art uses `https://coverartarchive.org/release-group/{mbid}/front-500`;
  the browser follows the archive.org redirect.

## Scan and registration

Open **Music → Library Import** (also linked from the Music toolbar and empty
state). The music folder must be mounted below `REELSCAREY_MEDIA_DIR`; enter its
relative name exactly, including case, in the scan dialog.
- The scan root must be a relative folder below `REELSCAREY_MEDIA_DIR`. Absolute
  paths and `..` are rejected.
- Matching order:
  - album: MusicBrainz release-group ID, then exact (case-insensitive) album
    title plus artist/album-artist name;
  - track: MusicBrainz track ID, recording ID, disc/number, then exact title.
- Unmatched files stay `unresolved` with their tags and an explanation. Select
  those files and choose **Discover missing catalog** to review a bounded
  MusicBrainz search. Discovery only acts on strong evidence:
  - a release-group MusicBrainz ID in the file tag; or
  - exactly one artist whose normalized name equals the tagged album artist (or
    artist), and exactly one release group whose normalized title equals the
    tagged album. The year is used only to disambiguate multiple exact title
    matches.
- Discovery adds only the missing artist and release group, loads the album's
  tracks, and re-matches the selected files. It never moves, tags or registers a
  file, and a file it cannot prove stays unresolved with a clear message.
- A request processes at most 10 album groups and 500 files, and stops starting
  new provider work before the server write deadline. The response reports
  matched files, added artists/albums, unresolved files, deferred files and
  unresolved files remaining in the scan. Select the remaining files and run it
  again to continue.
- A scan is a reviewed job: start it, poll its state, discover catalog entries
  or register selected matched files with `reviewed: true`. Restart marks an
  interrupted scan failed.

## Organization and tagging

- All edits are previewed first. The preview returns a `plan_hash`; applying a
  plan whose hash no longer matches is refused.
- A file is staged in its destination folder. Moves use a hardlink where
  possible and a verified copy otherwise. Publication uses an exclusive
  create/link, so an existing destination is never overwritten.
- A retag copies the source to `<source>.reelscarey-backup`, writes tags with
  FFmpeg `-c copy` to a staged file, verifies the staged tags with FFprobe, then
  publishes. The backup blocks a second retag until it is reviewed.
- MP3 writes use ID3v2.3; M4A uses `use_metadata_tags` so custom keys persist.
  OGG keeps comments on the stream. OGG embedded artwork is not supported.
- Rollback restores the retained backup or moves the destination back, restores
  the previous catalog path and the previous tag record, and marks the job
  `rolled_back`.

## Limits

- The worker imports a completed folder automatically only when every track has
  one matching disc/track file. Missing or ambiguous tags leave the job
  `blocked`; its files can be mapped explicitly from the Wanted page.
- No resolution/format upgrade suggestions and no per-album profile overrides.
- Search mode stores candidates for review; it does not queue.
- No playlists, lyrics, ratings, scrobbling or MusicBrainz editing.
- Catalog discovery requires exact normalized artist and album-title evidence,
  or a release-group ID. It is not a fuzzy matcher, does not use title
  similarity as identity and does not add a complete artist discography.
- No embedded cover-art writing and no CUE/multi-track single-file albums.
- Edits only touch registered files below the media root. Symlinks are skipped.
- A cross-device move falls back to a verified copy; it is not an atomic rename.
- Every artist and album is stored with `status: missing` until a file is
  registered, then `available`. Partial albums are not yet distinguished.
- The UI labels the catalog/import/organize pages by their actual scope; do not
  describe acquisition as available.

## API

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/api/v1/metadata/music/artists?query=&page=` | MusicBrainz artist search |
| GET | `/api/v1/library/music/artists?query=&page=` | Saved artists |
| POST | `/api/v1/library/music/artists` | `{musicbrainz_id,monitored?}`; 201 created or 200 existing |
| GET | `/api/v1/library/music/artists/{id}` | Artist and albums |
| GET | `/api/v1/library/music/albums/{id}` | Album and tracks; first read loads tracks |
| GET/POST | `/api/v1/library/music/scans` | List scans or start `{root}`; 202 |
| GET | `/api/v1/library/music/scans/{id}?page=` | Scan state and 100 files per page |
| POST | `/api/v1/library/music/scans/{id}/register` | `{file_ids,reviewed:true}` |
| POST | `/api/v1/library/music/scans/{id}/discover` | `{file_ids,reviewed:true}` or `{all:true,reviewed:true}`; bounded catalog discovery |
| POST | `/api/v1/library/music/scans/{id}/cancel` | `{}` |
| POST | `/api/v1/library/music/scans/{id}/skip` | `{file_id}` |
| GET | `/api/v1/library/music/files?page=` | Registered files |
| GET | `/api/v1/library/music/edits?page=` | Recent edit jobs |
| POST | `/api/v1/library/music/edits/preview` | `{file_ids}`; returns the plan and hash |
| POST | `/api/v1/library/music/edits/apply` | `{file_ids,plan_hash,reviewed:true}`; 202 |
| GET | `/api/v1/library/music/edits/{id}` | Job state and files |
| POST | `/api/v1/library/music/edits/{id}/rollback` | `{reviewed:true}` |
| POST | `/api/v1/library/music/albums/{id}/releases` | `{query?}`; audio-indexer search, no queue |
| POST | `/api/v1/library/music/albums/{id}/import` | `{source_path,root}`; reviewed folder import; 202 |
| GET | `/api/v1/library/music/imports?page=` | Import jobs |
| GET | `/api/v1/library/music/imports/{id}` | Import job and files |
| POST | `/api/v1/library/music/albums/{id}/download` | `{release_id,root}`; queue through the selected client; 202 |
| GET | `/api/v1/library/music/downloads?page=` | Music download jobs |
| GET | `/api/v1/library/music/downloads/{id}` | One job |
| POST | `/api/v1/library/music/downloads/{id}/cancel` | `{}` |
| POST | `/api/v1/library/music/downloads/{id}/retry` | `{}` |
| GET | `/api/v1/library/music/downloads/{id}/files` | Completed files with tags for mapping |
| POST | `/api/v1/library/music/downloads/{id}/mapping` | `{mappings:[{file_index,track_id}]}`; resume the import |
| GET/PUT | `/api/v1/library/music/preferences` | Audio preferences |
| GET | `/api/v1/library/music/wanted?page=` | Monitored albums with no files |
| GET/PUT | `/api/v1/library/music/schedule` | Wanted-album schedule |
| POST | `/api/v1/library/music/schedule/run` | `{}`; one bounded run |
| GET/PUT/DELETE | `/api/v1/library/music/albums/{id}/preferences` | Per-album override or reset to global |
| GET | `/api/v1/library/music/upgrades` | Albums below their effective preference |

The discovery response reports `selected`, `matched`, `artists_added`,
`albums_added`, `unresolved`, `remaining`, `deferred` and `warnings`.
All routes are administrator-only. Mutations require CSRF and same-origin
validation. Request accounts cannot read or write the music catalog.

## Data model

- `059_music.sql` — `music_artists`, `music_albums`, `music_tracks`.
- `060_music_files.sql` — `music_scans`, `music_scan_files`, `music_files`.
- `061_music_edits.sql` — `music_files.root`, `music_edit_jobs`,
  `music_edit_files`.
- `062_music_acquisition.sql` — `music_albums.revision`,
  `music_release_candidates`, `music_import_jobs`, `music_import_files`.
- `063_music_downloads.sql` — `music_downloads`, `music_preferences`,
  `music_schedule`.
- `064_music_album_preferences.sql` — per-album `allow_lossless`,
  `min_bitrate` and `preferred_formats` overrides.

The migrations are additive except for the `music_files.root` column. Existing
movie/TV rows, foreign keys and consumers are untouched.

## Next phase

The core Lidarr-style path — catalog, want, search, download, reconcile, map
blocked files, import, organize and tag — is implemented, including upgrade
suggestions and per-album preference overrides. Optional future work: automatic
upgrade replacement and richer multi-profile assignment.

## Validation

- `internal/music/client_test.go` — normalization, pagination, rate limits,
  oversized bodies, redirect refusal, exact release-group search and lookup.
- `internal/music/tags_test.go` — format tags, OGG stream tags, invalid bodies.
- `internal/music/store_test.go` — idempotent artist saves, track replacement,
  invalid paging.
- `internal/music/scan_test.go` — bounded scan, matching, in-place registration,
  availability and escaping roots.
- `internal/music/discovery_test.go` — release-group ID creation, exact
  artist/album creation, ambiguous and missing evidence retention, duplicate
  artist reuse, provider failure retention, scan count refresh and re-match.
- `internal/music/edits_test.go` — move/retag/no-op plans, move apply/rollback,
  FFmpeg tag write, retained backup and rollback.
- `internal/httpapi/music_test.go` — authentication, CSRF, catalog, scan,
  discovery and edit routes, error mapping, SPA routes.
- `internal/database/music_upgrade_test.go` — additive catalog migration.
- `web/src/test/Music.test.tsx`, `MusicImport.test.tsx`,
  `MusicOrganize.test.tsx` — catalog, import, discovery and organize pages.
- `scripts/music-container-smoke.sh` — disposable Docker acceptance: add an
  artist, load an album, generate a tagged FLAC, scan, register, preview/apply
  organization and tags, verify the MusicBrainz tag and retained backup, roll
  back, then generate a complete tagged album folder, import it, and verify the
  imported files and untouched sources. This passed against the isolated
  `reelscarey:music-acq` image on the acceptance server.

Catalog discovery was validated locally with targeted Go tests
(`go test ./internal/music ./internal/httpapi`), the frontend format/type checks
and the `MusicImport` Vitest suite. It was not run against the live acceptance
server or a real MusicBrainz library; provider behavior is covered by local
fake MusicBrainz responses.
