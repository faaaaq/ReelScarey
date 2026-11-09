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
organization and tag rewrites with rollback, fingerprints unresolved files
through AcoustID/Chromaprint, searches audio releases, queues them through the
selected download client, reconciles them with a leased worker, imports the
completed folder, and runs wanted-album searches on a schedule.

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
  explicit reviewed action. It uses a tagged release-group ID, a tagged album
  artist/artist MBID, or one exact normalized artist name and album title,
  creates only the matching artist/album, loads the album tracks and re-matches
  those files. Ambiguous or missing evidence stays unresolved.
  - Exact-name searches are bounded to five pages of 25 results so a common
    artist name is not lost behind the first page.
  - If a release-group search is incomplete or misses an exact title, discovery
    compares the artist's bounded discography instead. This is still an exact
    normalized-title comparison, never title similarity.
  - An album-artist MBID (`MUSICBRAINZ_ALBUMARTISTID`) is preferred over the
    track-artist MBID. The track-artist MBID is used only when the album artist
    and track artist tags name the same artist, so a compilation track is never
    attached to one featured artist.
  - Common Unicode equivalents, accents, typographic quotes/dashes, punctuation
    and repeated whitespace are folded for comparison. A leading English "The"
    and the `Various`/`Various Artists` compilation aliases are comparison-only;
    the stored catalog name always comes from MusicBrainz.
- Match selected unresolved files by audio fingerprint through AcoustID. It is
  a separate reviewed action that requires a saved application API key and
  fpcalc (Chromaprint) on the server. A request runs
  `fpcalc -json -length 120`, sends only the fingerprint and whole-file duration,
  requires a score of at least 0.85 and exactly one distinct release group, then
  adds or loads that exact MusicBrainz release group and matches the track by
  AcoustID recording ID before existing tag/track evidence. Missing or ambiguous
  matches stay unresolved. Audio is never uploaded. This is native
  AcoustID/Chromaprint matching, not MusicBrainz Picard.
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

## AcoustID and Chromaprint matching

This is the provider-native AcoustID/Chromaprint path. It is not MusicBrainz
Picard, it does not read Picard's database, and it does not use acoustic
similarity beyond the Chromaprint fingerprint.

- The Docker image installs Debian `libchromaprint-tools`, which provides
  `fpcalc`. `REELSCAREY_FPCALC_PATH` may point at a different executable; the
  default command is `fpcalc`. The child process runs without a shell, with a
  bounded timeout and bounded output. The media path is rechecked for
  media-root containment, regular-file status and symlink escape immediately
  before it runs.
- The administrator must save an AcoustID **application** key. Create one at
  <https://acoustid.org/new-application>. The key is stored through
  `securestore` as the encrypted integration `acoustid`; it is covered by the
  existing `secrets.key` backup validation and is never returned by the API or
  written to logs. AcoustID is free for non-commercial use and its availability
  is not guaranteed.
- Only the Chromaprint fingerprint string and the whole-file duration are sent
  to AcoustID. Audio bytes, file paths, tags, library contents and account data
  are never uploaded.
- Confidence policy: only candidates with score `>= 0.85` count. The accepted
  candidates must name exactly one distinct release-group ID. Zero or multiple
  release groups leave the file unresolved with a specific message; the code
  never guesses from a low score or between albums. The highest-score recording
  for that release group is used first for track matching. Equal-score
  recordings for the same release group fall back to existing tag/track
  evidence instead of picking one arbitrarily.
- Fingerprints are cached in `music_fingerprints` by relative path together
  with the file size, mtime, duration and fingerprint. The cache is reused only
  while size and mtime still match; a changed file is fingerprinted again and
  replaces the row. A moved or renamed file is a new path and is fingerprinted
  again.
- The reviewed `POST /scans/{id}/fingerprint` action processes at most 20
  unresolved files and starts MusicBrainz work for at most 10 album groups per
  request. Excess files are deferred and reported. It stops starting work
  before the server write deadline, keeps sequential AcoustID lookups at least
  350 ms apart, and never moves, renames or edits a file.
- Re-running is safe: matched files are no longer unresolved, cached
  fingerprints are reported as fingerprinted again without running `fpcalc`,
  and catalog rows are found instead of copied.

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
- Fixed AcoustID provider: `https://api.acoustid.org/v2/lookup`. Requests send
  `client` (the saved application key), `duration`, `fingerprint` and
  `meta=recordings+releasegroups` and request JSON. Arbitrary URLs and
  redirects are refused. Responses and parsed lists are bounded (2 MiB, 100
  results/recordings/release groups per result). Sequential lookups are spaced
  at least 350 ms apart to stay below the published three-per-second limit.
  `429` becomes a rate limit with `Retry-After`; `400`/`401`/`403` become an
  invalid-key error; transport and server failures become unavailable. Nothing
  is silently retried.

## Scan and registration

Open **Music → Library Import** (also linked from the Music toolbar and empty
state). Music roots are configured in **Settings → Media Management → Library
roots**, alongside the video collections. Music defaults to the exact `Music`
folder below `REELSCAREY_MEDIA_DIR`; if only a lowercase `music` folder exists,
that existing directory is shown and used. A missing folder is reported as
unavailable rather than guessed. Add more music roots to scan several folders in
one reviewed scan.
- Every scan root must be a relative folder below `REELSCAREY_MEDIA_DIR`.
  Absolute paths, `..`, symbolic links and overlapping roots are rejected.
- **Scan folder** offers the configured roots and starts one scan across the
  selected directories. Scan results record the root that supplied each file;
  registration keeps the file in place and stores its own root.
- Matching order:
  - album: MusicBrainz release-group ID, then a trusted album-artist MBID plus
    exact normalized album title, then exact normalized album title plus
    artist/album-artist name;
  - track: MusicBrainz track ID, recording ID, disc/number, then exact
    normalized title. The same order is used again after discovery.
- A missing disc tag is not a reason to skip the number match. A single-disc
  album matches its only disc; a multi-disc album uses the title to choose
  between equal track numbers instead of assuming disc 1.
- For track titles only, a trailing `(feat. ...)` or `[ft. ...]` credit is also
  compared with its base title. If both the base and credited titles exist in
  the album, the file stays unresolved instead of picking one.
- Album-artist MBIDs are read from `MUSICBRAINZ_ALBUMARTISTID` and stored in the
  scan's tag document (`tag_json`). The document carries a parser version; an
  unchanged file whose saved document predates the current parser is probed once
  more while its saved SHA-256 is reused. Older scans therefore pick up the new
  field on the next scan without reading the media again.
- Unmatched files stay `unresolved` with their tags and an explanation. Select
  those files and choose **Discover missing catalog** to review a bounded
  MusicBrainz search. Discovery only acts on strong evidence:
  - a release-group MusicBrainz ID in the file tag; or
  - a trusted album-artist/artist MusicBrainz ID in the file tag; or
  - exactly one artist whose normalized name equals the tagged album artist (or
    artist), and exactly one release group whose normalized title equals the
    tagged album. The year is used only to disambiguate multiple exact title
    matches.
- Discovery adds only the missing artist and release group, loads the album's
  tracks, and re-matches the selected files. It never moves, tags or registers a
  file, and a file it cannot prove stays unresolved with a clear message:
  missing artist or album tag, no exact artist, ambiguous artist, no exact
  release group, ambiguous release group, or album in the catalog with no
  matching track.
- Adding an artist or album is idempotent. A release group is stored once even
  if two artists list it, and a rerun finds the same catalog rows instead of
  copying them. A provider failure keeps the existing catalog, scan rows and
  registered files and reports an actionable warning.
- A request processes at most 10 album groups and 500 files, and stops starting
  new provider work before the server write deadline. The response reports
  matched files, added artists/albums, unresolved files, deferred files and
  unresolved files remaining in the scan. Select the remaining files and run it
  again to continue.
- Audio fingerprint matching is a second, separate action for selected
  unresolved files. Select them and choose **Match by audio fingerprint**. It
  requires explicit review, a saved AcoustID application API key and fpcalc
  (Chromaprint) on the server; the button stays disabled when either is
  missing. Fingerprinting computes Chromaprint evidence for the selected files,
  looks it up through AcoustID, adds only the matched artist and album, loads
  the album tracks and re-matches those files. A request processes at most 20
  files and 10 album groups and stops early within its time budget. It adds only
  the exact release group AcoustID names with a score of at least 0.85 and
  exactly one distinct release group. Audio with no confident match, an
  ambiguous result or a provider warning stays unresolved; nothing is moved,
  tagged or registered. The response reports `fingerprinted` files,
  `provider_calls`, matched files, added artists/albums, unresolved files,
  deferred files, unresolved files remaining in the scan and warnings.
- The AcoustID status panel reports whether an application key is configured,
  whether fpcalc is available and the server's message. **Configure AcoustID**
  saves the key through the encrypted secure store, links to
  https://acoustid.org/new-application and can remove the saved key. The API key
  is never returned to the browser or displayed by the page. Missing keys,
  missing fpcalc, rejected keys, rate limits, ambiguous matches and no-match
  results produce an actionable warning instead of a success message.
- A scan is a reviewed job: start it, poll its state, discover catalog entries,
  fingerprint unresolved files or register selected matched files with
  `reviewed: true`. Restart marks an interrupted scan failed.

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

- AcoustID matching requires the bundled `fpcalc` and an owner-supplied
  application key. It is not Picard, does not write MusicBrainz tags on its own,
  and does not guarantee a match. A provider outage leaves files unresolved.
- Only Chromaprint fingerprint and duration are sent. No audio is uploaded.
  A release group is added only from one unique result with score `>= 0.85`;
  low scores, multiple release groups and missing release groups require better
  tags or manual review instead of a guess.
- Fingerprint work is bounded to 20 files and 10 album groups per request.
  Select the remaining unresolved files and run the action again to continue.
  Cached evidence is local to this media path and is replaced when size or mtime
  changes.
- The worker imports a completed folder automatically only when every track has
  one matching disc/track file. Missing or ambiguous tags leave the job
  `blocked`; its files can be mapped explicitly from the Wanted page.
- No resolution/format upgrade suggestions and no per-album profile overrides.
- Search mode stores candidates for review; it does not queue.
- No playlists, lyrics, ratings, scrobbling or MusicBrainz editing.
- Catalog discovery requires exact normalized artist and album-title evidence,
  a trusted tagged MBID, or a release-group ID. It is not a fuzzy matcher, does
  not use title similarity as identity and does not add a complete artist
  discography. The discography fallback compares exact normalized titles and
  stores only the one match.
- Audio fingerprint matching is not a replacement for tag discovery and does
  not identify tracks without a usable AcoustID result. It needs the AcoustID
  application key and fpcalc; the page disables the action when either is
  missing and never displays the saved key.
- A compilation whose album-artist tag is missing is not guessed: per-track
  artist searches can fail, and the file stays unresolved until the album
  artist or a release-group ID is tagged. Featured-credit fallback applies only
  when the album artist and track artist are the same tagged name.
- `MUSICBRAINZ_ALBUMARTISTID` is persisted in the scan's tag document only;
  there is no dedicated scan column or migration. A scan created before this
  field existed needs a new scan to use it; the new scan re-probes unchanged
  files once as described above.
- A release MBID (`MUSICBRAINZ_ALBUMID`) is stored but not used for discovery:
  the catalog is keyed by release group. Files that carry only a release MBID
  fall back to the tagged artist and album title.
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
| GET/POST | `/api/v1/library/music/scans` | List scans or start `{roots:[...]}` (legacy `{root}` accepted); 202 |
| GET | `/api/v1/library/music/scans/{id}?page=` | Scan state and 100 files per page |
| POST | `/api/v1/library/music/scans/{id}/register` | `{file_ids,reviewed:true}` |
| POST | `/api/v1/library/music/scans/{id}/discover` | `{file_ids,reviewed:true}` or `{all:true,reviewed:true}`; bounded catalog discovery |
| POST | `/api/v1/library/music/scans/{id}/fingerprint` | `{file_ids,reviewed:true}` or `{all:true,reviewed:true}`; reviewed Chromaprint/AcoustID matching |
| GET/PUT/DELETE | `/api/v1/library/music/acoustid` | Status; PUT `{api_key}` saves or replaces the encrypted key, DELETE removes it. The key is never returned |
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
`albums_added`, `unresolved`, `remaining`, `deferred` and `warnings`. The
fingerprint response adds `fingerprinted` and `provider_calls` to the same
shape. AcoustID status reports `configured`, `updated_at`,
`fingerprint_available` and `message`; it never includes the saved key.
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
- `072_music_fingerprints.sql` — `music_fingerprints` cache and the encrypted
  `acoustid` integration name.

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
- `internal/music/normalize_test.go` — case, punctuation, typographic quotes and
  dashes, non-breaking spaces, composed/decomposed accents, leading articles,
  Various Artists aliases and featured-credit splitting.
- `internal/music/tags_test.go` — format tags, album-artist MBID, OGG stream
  tags, invalid bodies.
- `internal/music/store_test.go` — idempotent artist saves, release-group reuse
  across artists, track replacement, invalid paging.
- `internal/music/scan_test.go` — bounded scan, matching, in-place registration,
  availability, escaping roots, track evidence order, missing disc handling,
  ambiguous titles, article/Unicode album lookup.
- `internal/music/discovery_test.go` — release-group ID creation, tagged artist
  MBID use, later artist-search pages, discography fallback (including a later
  browse page), missing disc tags, multi-disc titles, compilation album-artist
  MBIDs, Various Artists and featured-artist aliases, exact artist/album
  creation, ambiguous and missing evidence retention, unknown tagged MBIDs,
  album added without a matching track, duplicate artist reuse, provider failure
  retention, scan count refresh, rerun idempotence and re-match.
- `internal/music/acoustid_test.go` — fake fingerprinter and fake AcoustID HTTP
  server: strong unique release-group matching, recording-ID-first track match,
  score `>= 0.85`, multiple/no release groups, no match, invalid key, rate limit,
  missing key/fpcalc, 20-file deferral, cached fingerprint reuse, cache
  invalidation on size/mtime change, status without key disclosure and
  `fpcalc` output validation.
- `internal/music/edits_test.go` — move/retag/no-op plans, move apply/rollback,
  FFmpeg tag write, retained backup and rollback.
- `internal/httpapi/music_test.go` — authentication, CSRF, catalog, scan,
  discovery, AcoustID key status/save/delete without key disclosure, fingerprint
  route validation and error mapping, edit routes, error mapping, SPA routes.
- `internal/database/music_upgrade_test.go` — additive catalog migration.
- `web/src/test/Music.test.tsx`, `MusicImport.test.tsx`,
  `MusicOrganize.test.tsx` — catalog, import, tag discovery, AcoustID status,
  key save/remove without echoing the key, disabled fingerprint controls and
  honest fingerprint result/error summaries, and organize pages.
- `scripts/music-container-smoke.sh` — disposable Docker acceptance: add an
  artist, load an album, generate a tagged FLAC, scan, register, preview/apply
  organization and tags, verify the MusicBrainz tag and retained backup, roll
  back, then generate a complete tagged album folder, import it, and verify the
  imported files and untouched sources. This passed against the isolated
  `reelscarey:music-acq` image on the acceptance server.

Catalog discovery was validated locally with targeted Go tests
(`go test ./internal/music ./internal/httpapi`), the frontend format/type checks
and the `MusicImport` Vitest suite. The 0.46 discovery hardening added the
normalization, tagged-MBID, bounded-page, discography-fallback, compilation and
track-evidence cases above. It was not run against the live acceptance server or
a real MusicBrainz library; provider behavior is covered by local fake
MusicBrainz responses. The AcoustID status, key save/remove and fingerprint
result UI were validated with fake API responses in the `MusicImport` suite; no
real AcoustID account, fpcalc run or audio file was used. Backend
AcoustID/Chromaprint matching was validated locally with generated media and
fake `fpcalc`/AcoustID/MusicBrainz responses. It was not run against a real
AcoustID account or owner media; the service is free for non-commercial use but
its availability is not guaranteed. The Docker runtime installs
`libchromaprint-tools`; `scripts/music-container-smoke.sh` checks
`fpcalc -version` in the disposable container.
