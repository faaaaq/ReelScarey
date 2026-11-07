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
  again. The daemon's lifecycle status - running, starting, stopped or failed
  with its message - is shown on the page and never invented.
- **Automatic seeding limit.** The Torrents page saves a ratio and/or
  seeding-time limit with an action (pause, remove the torrent entry, or remove
  and delete files). A supervised pass runs at most once a minute while the
  client is enabled and reachable. It only acts on completed torrents, skips
  torrents bound to unfinished ReelScarey jobs, and skips ReelScarey torrents
  unless they are explicitly included. Their files are never deleted: an
  include plus "remove and delete" only removes the torrent entry. The pass
  records one History entry per torrent hash and action and retries errors on
  the next tick. See [Torrents](TORRENTS.md#automatic-seeding-limit).
- Generic client editing and deletion refuse the built-in connection; use its
  own controls. It can still be tested from the client list.

## Built-in SABnzbd (headless)

The Docker image also ships Debian's `sabnzbdplus` and `par2`, and the
application supervises one private Usenet daemon the same way it supervises
qBittorrent. This is the **bundled downloader**; external SABnzbd 4/5 and NZBGet
connections remain fully supported and are not affected by it.

- **Never automatic.** A new installation has no Usenet default and no bundled
  Usenet row. Saving a news server on the **Usenet** page creates a paused
  supervised Usenet connection (fixed id
  `6f1c2b3a4d5e4f60718293a4b5c6d7e8`); the owner must turn it on explicitly.
  The supervisor never enables a client the owner did not turn on, and never
  starts one without a news server.
- **Private loopback daemon.** SABnzbd listens only on `127.0.0.1:8086` with a
  generated API key. Its profile is `config/sabnzbd`; incomplete data is
  `downloads/builtin-usenet/incomplete`; completed output is
  `downloads/builtin-usenet/ReelScarey/reelscarey-<job>`. The generated category
  is `reelscarey`, folder `ReelScarey`, no script, and folder renaming stays on.
  On a hardened installation set `DOWNLOADS_READ_ONLY=false` and prepare
  `downloads/builtin-usenet` with
  `install -d -o <uid> -g <gid> "$DOWNLOADS_PATH/builtin-usenet"`.
- **News server settings.** Host, port, TLS flag, username, password and 1–64
  connections are encrypted through securestore. The API never returns the
  password; changing the host or username requires re-entering it. Test
  connection is a short TCP reachability probe only, not an account check.
- **Routing.** The row is an ordinary Usenet client of kind `sabnzbd`. Turning
  it on does **not** make it the Usenet default; choose it under **Usenet
  default** here. Existing jobs keep the client and folder mapping they were
  reserved with. Turning it off clears its Usenet default and refuses while its
  jobs are active.
- **Queue page.** The `Usenet` page lists the bounded SABnzbd queue, supports
  multi-select pause/resume/delete, uploads only `.nzb` files (no URL fetch), and
  sets an absolute speed limit. Deleting a job still tracked by an unfinished
  ReelScarey download is refused. See [Usenet](USENET.md#bundled-sabnzbd).
- Generic client editing and deletion refuse the built-in connection; use the
  Usenet page. The saved connection can still be tested through
  `POST /api/v1/download-clients/{id}/test`.
- **Provider and indexer limits are unchanged.** ReelScarey does not provide
  NNTP access, quota, retention scoring or an indexer; those stay with your
  provider and Newznab/Torznab indexers. Repair and unpacking use SABnzbd's
  installed `par2`, `unrar` and `7z` tools.

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

The application now ships an optional supervised SABnzbd daemon and Usenet
queue in the Docker image, described above. External SABnzbd and NZBGet
connections remain supported for owners who need a separate daemon, NAS or
shared mount. The bundled instance is a downloader only: your NNTP provider,
indexer, quota and retention remain external, and ReelScarey does not implement
article download, repair or unpacking itself.

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
