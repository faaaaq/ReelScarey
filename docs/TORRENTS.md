# Torrents

**Torrents** is a direct front-end for the supervised built-in qBittorrent
client (qBittorrent 4.5.x, Web API 2.8.x). It lists every torrent in that client
with progress, download/upload speed, ETA, seeds/peers, ratio, queue position,
limits, share limits and state, and refreshes while the page is open.

The page is laid out like the qBittorrent Web UI: a toolbar (add links/files,
resume, pause, force resume, delete, queue moves, recheck, reannounce, a seeding
policy dialog, a text filter and a column chooser), a collapsible filter sidebar
(status, source, category, tag and tracker, with counts), a sortable multi-select
torrent table with a right-click menu, a resizable properties panel (General,
Trackers, Peers, HTTP Sources, Content) for a single selected torrent, and a
status bar with connection state, DHT nodes, the alternative speed toggle, the
global download/upload limits and the saved seeding limit. Column, sort, sidebar
and panel choices are kept in the browser.

- **Add** a magnet link or upload a `.torrent` file. Manual torrents are saved
  under `builtin/ReelScarey/manual` and tagged `reelscarey-manual`; the server
  never fetches a remote torrent URL for this page.
- **Origin.** Every torrent is labelled from its tags: `job` for torrents
  ReelScarey submitted for a download job (`reelscarey-<job id>`, with the job
  ID), `manual` for torrents added on this page, and `external` for anything
  else in the client.
- **Single and bulk actions** (up to 500 selected torrents): pause, resume,
  recheck, reannounce, force start, queue top/bottom/up/down, per-torrent
  download/upload limits, share limits (ratio and seeding time, or the global
  setting), sequential download, first/last piece priority and super seeding.
  qBittorrent only toggles sequential and first/last piece modes, so ReelScarey
  reads the current state and toggles only the torrents that differ from the
  requested state. Queue actions report that queueing is off when it is
  disabled in the client.
- **Remove** always requires the review dialog. Deleting downloaded data is a
  separate checkbox. Torrents added by ReelScarey carry a warning because
  deleting their data can break an import or a seeding source.
- **Details** shows general properties (save path, sizes, dates, piece size,
  connections, averages, comment), every file with size, progress, priority and
  availability, trackers with their status and messages, connected peers (at
  most 2000, busiest first), web seeds and a piece map of at most 512 buckets.
- **File priorities** (do not download, normal, high, maximum) can be changed
  for manual and external torrents.
- **Global speed limits**: switch the alternative speed mode and set the global
  download/upload limits. The limits apply to whichever mode is active, which
  is how qBittorrent itself applies them. The overview also shows whether
  queueing is enabled and the alternative limits.

## Automatic seeding limit

qBittorrent pauses a torrent when its own share limit is reached and never
removes torrents or data. ReelScarey therefore enforces the owner's seeding
limit itself: **Seeding policy** in the toolbar opens a dialog with an enabled
switch, the action to apply, a maximum ratio, a maximum seeding time (hours and
minutes) and an option to include ReelScarey downloads. The status bar shows the
saved summary, for example `Seeding limit: ratio 2.0 · pause`.

- The ratio rule accepts 0.1–1000 and the time rule 0–525600 minutes. `0`
  disables an individual rule, and an enabled limit needs at least one rule.
- `Pause` stops the torrent, `Remove torrent` removes its entry without data,
  and `Remove torrent and delete files` also deletes the downloaded data.
- A supervised pass runs at most once a minute, and only while the built-in
  client is enabled and reachable. It evaluates every torrent whose reported
  `progress` is 1 and `amount_left` is 0; active downloads are never touched.
- Torrents bound to a ReelScarey download job that has not finished are skipped
  entirely. Torrents submitted by ReelScarey are skipped unless **Include
  ReelScarey downloads** is on, and their files are **never deleted**: for those
  torrents `Remove torrent and delete files` degrades to removing the torrent
  entry only, so the reviewed import and the seeding source survive. The dialog
  states this explicitly.
- Already paused torrents are left alone, even if they exceed the limit.
- Every applied action writes one History entry (`kind=download`, object is the
  torrent hash, state `seeding limit reached: <action>`). A hash/action pair is
  recorded once; the durable entry also survives a restart, so a resumed torrent
  is not acted on again for the same action.
- Client errors are logged and retried by the next pass; a failing pass never
  stops the application. **Run now** evaluates the saved policy immediately and
  reports how many torrents were checked and acted on.

The policy is stored in SQLite with a revision, so two editors cannot silently
overwrite each other. It applies only to the supervised built-in client; named
external clients keep their own share limits.

The page only manages the built-in client. External qBittorrent, Transmission,
SABnzbd and NZBGet connections keep their existing job-level controls and are
not exposed as a general client console. The built-in client is included and
enabled by default; turning it off in Download clients makes this page explain
how to turn it back on.

## Intentionally excluded

- **No native Web UI proxy.** qBittorrent's own Web UI and preferences can run
  programs on completion and write to arbitrary paths, so ReelScarey exposes a
  restricted, administrator-only API instead. Only the queueing switch and the
  alternative speed limits are read from the client preferences; nothing else
  is read or forwarded, and preferences cannot be changed from this page.
- **No set location, rename, category, tag or tracker edits.** ReelScarey's
  imports and seeding sources depend on the save paths and tags it assigned.
- **Job file selection is protected.** Changing which files a ReelScarey
  download fetches can break its reviewed import, so file priority changes are
  refused for `job` torrents; change those downloads from Activity instead.
- **Tracker passkeys are never shown.** Private tracker announce URLs carry
  passkeys in the user information, path or query. Every tracker and web seed
  address is reduced to `scheme://host[:port]` (DHT/PeX/LSD entries are shown
  as-is, unrecognisable values as `(hidden)`), URLs in tracker messages are
  reduced the same way, and the torrent's magnet link is never returned.

Stopping or removing a torrent on this page does not change a ReelScarey
download record. Active imports still reconcile against the client and will
report a failure if their torrent disappears. Manual torrents are independent
downloads and are not imported into the library by this page.

See [API](API.md#torrents-page-api) for request and response shapes.

Archive-only releases are unpacked before import; see
[Archive extraction](ARCHIVES.md) for formats, limits and cleanup.
