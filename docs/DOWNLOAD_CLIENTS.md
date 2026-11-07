# Download clients

Version 0.24 adds **Settings → Manage download clients** (`/download-clients`).
Save up to 32 named qBittorrent, Transmission 4, SABnzbd and NZBGet connections. Each connection
is tested before saving and starts paused. Enable it explicitly, then choose it
as the torrent or Usenet default. Existing Settings connections remain available
through the explicit **Settings connection** choice. No existing job is rerouted
when a default changes, and a failed named client never silently falls back.

## Built-in qBittorrent (0.31, included by default since 0.38.4)

The Docker image ships `qbittorrent-nox`, and the application supervises it
directly. The supervision loop creates the saved connection, enables it and
makes it the torrent default automatically whenever the binary is present and
the owner has not disabled it before, so a new installation can download
without any setup. The daemon's API listens only on the container's loopback
interface, so the application is the only client and nothing is exposed beyond
the peer port.

- **Included by default (0.38.4, owner decision):** when no torrent default is
  chosen and the qBittorrent Settings connection is not configured, release
  downloads use the built-in client. Disabling it on the Download clients page
  clears its torrent default and stops the daemon; the supervision loop never
  re-enables a client the owner turned off. Turning it on again restores the
  same behaviour. The old explicit **Set up built-in client** flow remains for
  deployments where the daemon could not be provisioned.
- Existing jobs never change their client: they keep the client and folder
  mapping they were bound to, including after a default change.
- The daemon writes only inside `downloads/builtin` under the download mount.
  On a hardened installation the download mount is read-only by default; set
  `DOWNLOADS_READ_ONLY=false` in `server.env` and prepare the folder with
  `install -d -o <uid> -g <gid> "$DOWNLOADS_PATH/builtin"` (the container user
  is `10001:10001` in the shipped compose file). Disabling the client refuses
  while jobs are active.
- The peer port defaults to 6881 and can be changed on the page; the compose
  file publishes it as `REELSCAREY_QBT_PORT` (TCP and UDP). Changing it
  restarts the daemon briefly; completed state resumes from fastresume.
- Disabling stops the daemon; retained seeding resumes when it is enabled
  again. The daemon's lifecycle status — running, starting, stopped or failed
  with its message — is shown on the page and never invented.
- Generic client editing and deletion refuse the built-in connection; use its
  own controls. It can still be tested from the client list.

## Mullvad VPN kill switch (0.33)

The Download clients page can save an encrypted Mullvad WireGuard
configuration and bring up a `reelscarey0` tunnel **without installing any
routes**. The built-in torrent client binds directly to that interface, so:

- torrent traffic can only leave through the tunnel — if the interface
  disappears, the client has no egress and cannot fall back to the bare
  network;
- the supervisor additionally stops the built-in client while the tunnel is
  down and starts it again when the tunnel returns;
- the rest of the application (metadata, search, the web UI) keeps using the
  normal connection, and DNS lookups still use the container's resolver —
  tracker hostnames remain visible to that resolver, which the settings page
  states plainly.

Enabling requires the container to run with `cap_add: [NET_ADMIN]` and
`REELSCAREY_KEEP_NET_ADMIN=1`, which retains that capability for the
application process through its privilege drop; the default deployment stays
capability-free. The private key is stored encrypted, never returned by the
API, and redacted from any tool output that reaches logs or status messages.
A failed connection reverts to disabled with the failure visible.

Upload the `.conf` exported by Mullvad and the app fills every field and
connects; the tunnel address may list both IPv4 and IPv6 entries. The tunnel is
created with `wg` and `ip` directly rather than `wg-quick`, because `wg-quick`
re-execs through `sudo` and the application intentionally runs unprivileged
with only `NET_ADMIN`. Sockets bound to `reelscarey0` use a dedicated routing
table; everything else keeps the main table, so application and web traffic
never enter the tunnel. IPv6 is optional when the container has no IPv6
support.

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

### Add an external SABnzbd or NZBGet connection

Open **Settings → Download clients → Add client**, choose **SABnzbd** or
**NZBGet**, and fill in:

- **Name:** a unique label, for example `NAS SABnzbd`.
- **Server URL:** the client's own address as the ReelScarey container sees it,
  such as `http://sabnzbd:8080` or `http://nzbget:6789`. NZBGet may include a
  reverse-proxy base path or end in `/jsonrpc`; it is stored without the suffix
  and appended when called.
- **SABnzbd API key:** the full API key, not the NZB-only key.
- **NZBGet username/password:** a dedicated account with API access.
- **Completed folder:** the client's absolute completed root, for example
  `/downloads` or `/completed`.
- **Local subfolder:** optional relative path under ReelScarey's download mount
  when that completed tree is mounted elsewhere.

Saving tests the connection and stores credentials encrypted; the new client
starts paused. Enable it explicitly, then choose it under **Usenet default**.
Each protocol has its own default: a SABnzbd or NZBGet connection cannot be the
torrent default, and a torrent client cannot be the Usenet default. NZB-type
releases route through the enabled Usenet default; Torrent releases never do.
Existing jobs keep the client, address and folder mapping they were reserved
with, even after a default change. Test connection re-checks the saved client
without saving changes. Removing or pausing a client requires another default
and no unfinished jobs.

The application still does not bundle a Usenet download daemon, NNTP client,
PAR2 repair, unpacker or Usenet queue. Building that would duplicate SABnzbd and
NZBGet (article download, connection pooling, repair/unpack, history, quota,
failover and security) inside the single container; it is a large separate
project and is intentionally not part of the named-client work. External clients
remain the supported path.

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

## Transmission 4 (0.26)

Choose **Transmission** and enter its RPC address, username/password and completed
folder mapping. A bare server URL uses `/transmission/rpc`; an explicit reverse
proxy path is preserved. Use Transmission 4 with legacy RPC 17 compatibility and
all torrent-added, completed and seeding-completed scripts disabled. The profile
is external to ReelScarey's single application container.

A submission attaches its ownership label and dedicated output folder atomically.
Tracking uses the info hash, which survives a client restart, rather than its
numeric torrent ID. A lost response is reconciled before any retry; a preexisting
foreign torrent is left untouched. Retain the application's labels and folders.
Only checked, fully downloaded bytes can enter the existing importer. TV still
requires explicit file-to-episode review, and seeding sources remain in place.
The torrent default can use Transmission; the Usenet default cannot.

The disposable test image in `scripts/transmission-test.Dockerfile` runs private,
authenticated Transmission movie and TV flows with generated web-seeded media.
It is a test tool, not part of the deployed runtime.
