# Download clients

Version 0.24 adds **Settings → Manage download clients** (`/download-clients`).
Save up to 32 named qBittorrent, SABnzbd and NZBGet connections. Each connection
is tested before saving and starts paused. Enable it explicitly, then choose it
as the torrent or Usenet default. Existing Settings connections remain available
through the explicit **Settings connection** choice. No existing job is rerouted
when a default changes, and a failed named client never silently falls back.

Each job retains its client ID/type/name, remote completed root and local folder
mapping. Credentials are encrypted with the existing configuration key and never
returned by the API. Leaving a secret blank retains it; changing the address or
username requires entering the secret again. Updates and defaults compare saved
revisions. Queue reservations also compare the selected client, default revision
and encrypted credentials after fetching release contents.

## Folder mapping

The client's completed folder is its own absolute path, for example `/completed`.
The optional local subfolder maps that root inside ReelScarey's download mount.
For example, with a download mount at `/downloads`, `server-a` maps the client's
`/completed/ReelScarey/...` to `/downloads/server-a/ReelScarey/...`. Mount that
completed tree read-only into the application container. Blank uses the mount
root. Relative traversal, symlink escapes and foreign job folders are refused.
Changing an address or mapping is blocked while unfinished jobs depend on it.
Stopped jobs retain their binding for retry. Credential rotation and display-name
changes preserve the queued identity and folder snapshots.

Choose another default before pausing a default connection. Pausing affects new
jobs; existing jobs keep tracking. Removing a connection requires a current
revision, explicit review, no default assignment and no unfinished jobs. Completed
history remains visible. Removing a saved connection never deletes jobs or files
from the external client.

## Usenet setup

SABnzbd requires category `reelscarey`, relative folder `ReelScarey`, no script
and no sorting. Use its full API key. NZBGet uses its username/password and the
[official JSON-RPC API](https://nzbget.com/documentation/api/); the server address
may include its proxy base path or end in `/jsonrpc`. Configure category
`reelscarey` with destination `<completed root>/ReelScarey`. Use a dedicated
NZBGet profile with no global/category extensions or renaming scripts.

Both clients use a unique job name and dedicated output folder. Uncertain
submissions reconcile against the queue/history and never automatically submit
the NZB again. Retain original job names, categories and history. Duplicate,
renamed, manually marked successful, incomplete or changed-output NZBGet jobs
are held for correction. Only successful post-processing can supply candidates.
TV downloads require the existing explicit episode/file review. Import copies
and verifies files through the shared journaled importer, preserving sources.

Configuration/catalog backups include all named clients, encrypted credentials,
defaults and immutable job bindings. The encryption key must accompany the
database; the existing backup/restore interface handles this. Media bytes remain
separate. Restore retains the existing recovery review before workers resume.

The application still deploys as one Docker container. External download clients
and their NNTP/indexer accounts are configured independently. Generated-media
acceptance uses isolated qBittorrent, SABnzbd 5.1.3 and NZBGet 26.3; it does not
establish acceptance of an owner's accounts, NAS, proxy or shared mounts.
