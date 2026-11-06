# Torrents

**Torrents** is a direct front-end for the supervised built-in qBittorrent
client. It lists every torrent in that client with progress, download/upload
speed, ETA, seeds/peers, ratio and state, and refreshes while the page is open.

- **Add** a magnet link or upload a `.torrent` file. Manual torrents are saved
  under `builtin/ReelScarey/manual` and tagged `reelscarey-manual`; the server
  never fetches a remote torrent URL for this page.
- **Pause**, **resume** and **recheck** act on the selected torrent.
- **Remove** always requires the review dialog. Deleting downloaded data is a
  separate checkbox. Torrents added by ReelScarey carry a warning because
  deleting their data can break an import or a seeding source.
- **Details** shows the save path and every file with size and progress.

The page only manages the built-in client. External qBittorrent, Transmission,
SABnzbd and NZBGet connections keep their existing job-level controls and are
not exposed as a general client console. The built-in client must be enabled in
Download clients; otherwise the page explains how to enable it.

Stopping or removing a torrent on this page does not change a ReelScarey
download record. Active imports still reconcile against the client and will
report a failure if their torrent disappears. Manual torrents are independent
downloads and are not imported into the library by this page.

Archive-only releases are unpacked before import; see
[Archive extraction](ARCHIVES.md) for formats, limits and cleanup.
