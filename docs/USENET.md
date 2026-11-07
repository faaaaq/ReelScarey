# Usenet downloads

Newznab sources and Usenet share the existing library, release search, quality
profiles, Activity, reviewed TV mappings and verified importer. Prowlarr is not
required.

The Docker image ships and supervises a headless SABnzbd daemon, and external
SABnzbd and NZBGet connections remain supported. An NNTP provider and an indexer
stay owner-supplied: ReelScarey is a downloader, not a Usenet provider, quota
manager or indexer. See [Bundled SABnzbd](#bundled-sabnzbd) for the supervised
client and [Setup](#setup) for external clients.

## Bundled SABnzbd

The runtime image installs Debian's `sabnzbdplus` and `par2`. The application
supervises one private SABnzbd instance and never uses an external SABnzbd for
these controls:

- **Process and address.** The daemon listens only on `127.0.0.1:8086` with a
  generated 32-character API key. The application is its only client; the port
  is not published. State is reported honestly as `stopped`, `starting`,
  `running` or `failed` with the process message. Restarts use bounded backoff,
  and disabling stops the process. A missing binary fails with an explanation
  instead of being presented as working.
- **Files.** Its profile lives in `ConfigDir/sabnzbd` (mode 0700, `sabnzbd.ini`
  mode 0600). Incomplete data is `DownloadsDir/builtin-usenet/incomplete`.
  Completed output is `DownloadsDir/builtin-usenet/ReelScarey/reelscarey-<job>`.
  The generated category is `reelscarey`, folder `ReelScarey`, no script,
  post-processing option 2 and folder renaming on. The daemon writes nowhere
  else. A hardened deployment that mounts downloads read-only must set
  `DOWNLOADS_READ_ONLY=false` and prepare `downloads/builtin-usenet` for the
  container user.
- **News server.** Host, port, TLS flag, username, password and connection count
  (1–64) are stored encrypted through the existing securestore. The API returns
  only configured, host, username, port, TLS and connections, never the
  password. Leaving the password blank keeps it; changing the host or username
  requires entering it again. Saving does not contact the provider, so a
  temporarily offline but valid provider can be saved. **Test connection** is a
  TCP reachability check with a short timeout; it does not authenticate the
  account or fetch an article, and it never crashes on a missing server. Saving
  changed provider settings restarts the daemon briefly; SABnzbd resumes its own
  queue from its profile, and existing ReelScarey jobs keep their binding.
- **Explicit opt-in.** New installations do not create or enable this client.
  Saving the first news server creates a paused supervised Usenet connection;
  the owner must turn it on on the Usenet page. The supervisor never starts a
  client without a news server and never re-enables one the owner turned off.
- **Routing.** The bundled connection is an ordinary selectable Usenet client
  (kind `sabnzbd`). It becomes usable for new releases only after the owner
  chooses it as the **Usenet default** in Download clients. Turning it on does
  not change that default. If it is disabled, its Usenet default is cleared and
  the existing "connect SABnzbd/NZBGet" prompts remain accurate. Existing jobs
  keep the client, URL and folder mapping they were reserved with, including
  after a default or provider change.
- **Queue.** The Usenet page shows a bounded queue with name, size, remaining,
  progress, status, speed, ETA and category. SABnzbd downloads one job at a time,
  so only the active job carries the global rate. Pause, resume and reviewed
  delete accept 1–500 job IDs. Delete with "also delete downloaded files" sends
  SABnzbd's `del_files=1`; ReelScarey never deletes completed library files.
  Deleting a job still tracked by an unfinished ReelScarey download is refused;
  stop that download from Activity first. The speed limit is an absolute
  byte-per-second value applied to the running daemon (0 removes it);
  SABnzbd's own saved percentage is left intact and applies again after a
  restart.
- **Add NZB.** The page accepts only bounded uploaded `.nzb` files (4 MiB,
  `application/x-nzb`). The server never fetches an NZB URL. Manual NZBs use the
  same dedicated category and are not imported into the library.
- **Secrets and logs.** The API key and NNTP password are never returned or
  logged. SABnzbd writes its own log inside the profile directory and receives
  the generated configuration on loopback only. Database backups include the
  encrypted provider settings and the client row; SABnzbd's own queue, history
  and logs are runtime state in its profile and are not part of that database
  backup.

The bundled daemon does not add an NNTP account editor, indexer, quota or
retention scoring. Repair and unpacking use the installed `par2`, `unrar` and
`7z` tools through SABnzbd. External SABnzbd/NZBGet connections remain fully
supported and keep their own job-level controls.

## Setup

Use either the bundled client above (save the news server on the Usenet page,
turn it on, then choose it as the Usenet default) or an external client below.

1. Configure your NNTP service in SABnzbd and choose its completed downloads folder.
2. Create category **reelscarey**, Folder/Path **ReelScarey** (relative to completed
   downloads), no script, and disable sorting for this category. Retain completed
   history until imports finish. Save/test checks category existence; output
   containment is checked before importing.
3. Mount that completed folder at ReelScarey's configured Downloads root. The API
   does not grant remote filesystem access. External clients, if connected, must
   map their configured remote root to this shared local Downloads root; the
   bundled client's mapping is fixed to its dedicated folder.
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
Torrents use qBittorrent; NZBs use the configured Usenet default (the bundled
SABnzbd or an external SABnzbd/NZBGet connection), without
conversion/fallback. Usenet has no peer/seeder counts, and torrent
minimum-seeder rules do not apply. Identity, resolution, language labels, size
and term rules still apply. Automation starts paused and download mode only
chooses releases with a connected matching client.

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

Not included: NNTP account administration, an indexer, quota or retention
scoring, article-level retry or repair outside SABnzbd/par2, arbitrary
caller-supplied NZB URLs, more than one bundled instance, or automatic library
import of manually uploaded NZBs. External SABnzbd 4/5 and NZBGet remain
supported for owners who prefer their own deployment. The bundled instance is
one supervised daemon, not an NNTP provider. IPTorrents/XSpeeds account tests
remain deferred.

References: [Newznab API](https://newznab.readthedocs.io/en/latest/misc/api.html),
[SABnzbd API](https://sabnzbd.org/wiki/configuration/4.5/api), and
[SABnzbd 5.1.3](https://github.com/sabnzbd/sabnzbd/releases/tag/5.1.3).
The pinned/checksummed SABnzbd release and Debian's `sabnzbdplus` package are
download clients only; generation tests never use an owner account or media.
