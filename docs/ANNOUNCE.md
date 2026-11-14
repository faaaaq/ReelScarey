# IRC announce feeds

ReelScarey can watch tracker IRC announce channels and keep a bounded history
of new releases. Capture is the default and the only behavior for most
trackers: their sources record and display announces but never download.
IPTorrents has one explicit, per-source, **off by default** automatic-grab
opt-in that turns a captured announce into an ordinary download. Everything
else stays capture-only and is reviewed by hand with the tracker link or a
manually attached `.torrent`.

## Supported trackers

The definitions come from the open-source Autobrr definitions and are covered
by parser tests using their published sample lines.

| Tracker | Network | Channel | Account required |
|---|---|---|---|
| IPTorrents | irc.iptorrents.com:6697 (TLS) | `#ipt.announce` | no |
| TorrentLeech | irc.torrentleech.org:7021 (TLS) | `#tlannounces` | no |
| FileList | irc.filelist.io:6697 (TLS) | `#announce` | no |
| BeyondHD | irc.beyond-hd.me:6697 (TLS) | `#bhd_announce` | yes, plus invite command |
| AnimeBytes | irc.animefriends.moe:7000 (TLS) | `#announce` | no, invite command may be required |
| MyAnonamouse | irc.myanonamouse.net:6697 (TLS) | `#announce` | yes |

All six trackers capture announces. Automatic grabbing is implemented only for
**IPTorrents**; the other five remain capture-only (see **Automatic grabbing**).

## Setup

Open **Announce feeds** and choose **Add Source**. The dialog shows a
**How to get the IRC values** panel with the same steps. Server, port, TLS and
channel are fixed from the tracker definition and cannot be edited.

### IPTorrents walkthrough

IPTorrents only needs a bot nick. There is **no NickServ account requirement and
no invite command**, so the fields in step 5 stay blank.

1. **Tracker**: `IPTorrents`.
2. **Source name**: any label, for example `IPTorrents`.
3. **IRC nick**: a bot nick that is unique to ReelScarey. It must differ from
   the nick any other IRC client or announce tool (for example Autobrr) is
   connected with — two clients on the same nick disconnect each other. A
   grouped nick such as `yourname|reelscarey` is easiest to keep free.
4. **NickServ account** and **NickServ password**: leave both **blank**.
   IPTorrents does not require authentication to read `#ipt.announce`. If you do
   register and group the nick (recommended so nobody else can take it), fill
   both so the client sends `IDENTIFY account password`.
5. **Passkey**: paste the passkey from your IPTorrents **details page**. It is
   encrypted, never returned by the API and unused unless automatic grabbing is
   enabled, so it can be left blank for capture-only. It lives in the field
   labelled by the tracker definition (here **Passkey**).
6. **Invite command**: leave **blank**.
7. **Skip TLS certificate verification**: leave this **off** first. Only turn it
   on if saving the source shows a connection error such as `tls: failed to
   verify certificate: x509: certificate has expired or is not yet valid`.
   IPTorrents' IRC server has served an expired certificate; the option lets the
   reader connect anyway. It keeps the link encrypted but stops checking the
   server's identity, so use it only for a tracker you trust. This option only
   affects the IRC capture connection; it is never used for the tracker
   download (see **Automatic grabbing**).
8. Tick **Connect this source now**, then **Add source**.

The row then shows **Enabled**. That means the worker has a running source, not
that the socket is proven connected; a connection error appears under the source
and the first release appears in **Recent announces**. To confirm IPTorrents is
live, wait for one announce (the channel is busy, so this is usually under a
minute) or open **Edit** to re-check the saved values.

### Connected but no announces

The reader waits for the tracker's welcome (`001`) before identifying to
NickServ and joining the channel. Joining earlier is silently discarded by some
servers (IPTorrents answers `451 You have not registered`), which used to leave
a source looking connected while capturing nothing.

Failures are now reported instead of hidden:

- A refused join (`477`, `473`, `475` and similar) becomes the source error, for
  example `irc server refused #ipt.announce: ... You need to be identified with
  services`. Fix the account/password or nick rather than waiting.
- A server `ERROR` line is returned as-is, so throttling shows up as
  `Throttled: Reconnecting too fast`. Leave the source alone until the tracker
  lifts the throttle; reconnects back off exponentially up to five minutes.
- A server that never completes registration fails after 90 seconds instead of
  hanging.

A successful join logs `announce source joined the IRC channel`. Tracker color,
bold and other IRC formatting markers are removed before matching, while the
original announce is retained as evidence. If that line is present and no error
is shown, the source is simply waiting for the next matching announce.

### TLS certificate errors

Every source connects with TLS and verifies the tracker's certificate against
the system trust store. If the tracker presents an expired or otherwise
untrusted certificate the source reports a `failed to verify certificate`
error and captures nothing.

For a tracker you trust, tick **Skip TLS certificate verification** when adding
or editing the source. The connection stays encrypted, but the server is no
longer authenticated, so a man in the middle could read anything sent on it —
which is why the field defaults to off and a source that uses it logs a warning
at startup. Prefer leaving it off whenever you can; if the tracker later renews
its certificate, turn the option back off.

This setting only relaxes the **IRC capture** connection. The tracker download
used by automatic grabbing is fetched separately over fixed HTTPS with normal
certificate verification, so an expired IRC certificate never weakens the
download (see **Automatic grabbing**).

### Other trackers

- **NickServ account/password** — fill both when the tracker definition marks
  the account as required (BeyondHD, MyAnonamouse). The password is your
  tracker IRC account password and is sent only to NickServ over TLS; it is
  stored encrypted.
- **Invite command** — paste the command from the tracker's IRC page when the
  tracker uses one (BeyondHD/AnimeBytes), for example
  `Millie announce YOURKEY`. The nick/tracker key inside it is sent verbatim.

## Replacing another announce tool (Autobrr)

The IRC capture here covers what an announce tool such as Autobrr does for
trackers in the table above: it joins the announce channel and records every
release. To switch over:

1. Give ReelScarey its own bot nick (step 3 above) **or** disconnect the other
   tool's client before enabling the ReelScarey source. Never run both on the
   same nick — they will fight and disconnect each other.
2. Add and enable the source, then confirm releases appear in
   **Recent announces**.
3. When you are satisfied, stop the other tool so only ReelScarey holds the
   nick.

Capture is independent of automatic grabbing: the announce feed records every
release for review, and only an explicitly opted-in IPTorrents source also
queues the releases that match the library (see **Automatic grabbing**).

## Automatic grabbing (IPTorrents)

Automatic grabbing is an explicit, **per-source opt-in that defaults to off**.
Only IPTorrents supports it; every other tracker and every non-opted source
stays capture-only. It is a controlled extension of the scheduled search: a
captured announce is matched with the same identity and quality rules the
scheduled search uses, then handed to the ordinary reservation and import
workflow. It never carries a download URL or credential across a boundary and
never bypasses review or the importer.

### Enabling it

- Turn on **Automatic grabbing** in the source's add/edit dialog. The option is
  disabled in the UI for any tracker other than IPTorrents, and the backend
  rejects an opt-in for any other tracker.
- A **saved tracker passkey is required**. Enabling automatic grabbing without a
  stored passkey is refused with `Save a tracker passkey before enabling
  automatic grabbing.` The passkey is stored encrypted with the application
  key, is never returned by the API, and is used only to build the tracker
  download URL. Leaving the field blank on edit keeps the saved passkey.
- The opt-in is saved on the source. Adding or editing a source writes the
  opt-in and increments the source revision; the IRC reader reconnects at the
  new revision automatically. Toggling the source pause/resume does **not**
  change the opt-in.

### What it grabs

Only a captured announce that resolves to exactly one of the following is
grabbed:

- an **exact** match to a saved, **monitored**, **missing** movie, or
- a single **ordinary TV episode** (a positive season and episode number) for a
  saved, monitored, missing episode.

Everything else stays capture-only for manual review, including:

- **Season packs, multi-episode ranges and chained releases** — the matcher
  returns no target for them, so they are never auto-grabbed.
- **Specials** (season zero) and any non-positive season or episode.
- **Anime** — Anime targets are rejected outright; Anime packs and mapped
  orders keep their reviewed manual flows.
- **Titles that already have a registered file** — registered media and
  episode files are never replaced, and the guard re-checks this inside the
  reservation transaction.
- **Ambiguous titles** — if a release title resolves to more than one saved
  title it is not grabbed even when one candidate would score higher; it stays
  for manual review (outcome `ambiguous`).

### Quality profiles and unknown seeders

The match is scored with the target's **saved quality profile** and its grab
delay, exactly as a scheduled search would. Announce lines do not report
seeders, so the missing seed count **satisfies the profile's minimum seeder
count** rather than failing it:

- A profile with any minimum seeders (including one that requires seeders)
  accepts an announce whose seeder count is unknown.
- The minimum seeder count still applies to **scheduled searches**, which
  always report a real count: a release that reports fewer seeders than the
  profile requires is rejected.

Because an announce never carries a count, this is the only way the seeder rule
is relaxed, and no seed count is ever invented. A release that does not satisfy
its profile's quality, language, size or identity rules is not grabbed (outcome
`rejected`).

### Durable processing, retries and outcomes

Opted-in announces create durable work rather than being downloaded inline:

- A leased background worker claims pending announces in bounded batches under
  short-lived leases with a heartbeat. A crashed or restarted worker reclaims
  its leases, so a captured announce is not lost and is not grabbed twice.
- **Transient failures** (a temporary indexer, client or network error) are
  retried with bounded exponential backoff. After repeated failures, or once a
  capture is too old, it is surfaced for review instead of retrying forever
  (outcome `needs_attention`).
- **Already tracked**: if the same captured announce already has a download
  record, it is not downloaded again; it is marked `already_tracked` and can be
  reviewed in Activity.
- Every captured announce carries a status in **Recent announces**. Outcomes
  include:
  - `queued` — a matching announce was queued automatically.
  - `already_tracked` — the announce already has a download record.
  - `unmatched` — no monitored missing movie or ordinary episode matched.
  - `not_aired` — the release matches a monitored episode whose listed air
    date has not arrived (see **Not-yet-aired episodes** below).
  - `ambiguous` — the title matches more than one saved title.
  - `rejected` — no candidate satisfies the profile's rules.
  - `stale_policy` / `stale_source` — the saved target, profile, source or
    opt-in changed before the grab, so the stale decision was not applied.
  - `needs_attention` — repeated failures or an error that requires review.
  - `capture_only` — the tracker, the source, or the release was not eligible
    for automatic grabbing.


### Not-yet-aired episodes

TV episodes are grabbed on or after their listed air date (see
[automation](AUTOMATION.md)). A valid announce that arrives before that date is
therefore not a title mismatch. The processor records the explicit
`not_aired` outcome naming the listed date and defers the event to the
air-date local midnight (at most 30 days ahead), when the ordinary match runs
again and the same release is grabbed if it still satisfies the profile. A
deferral that outlives its attempt limit, or an air date more than 30 days
away, is completed as `not_aired` with an actionable message instead of being
parked forever.
### How the download URL is built

Automatic grabbing does **not** trust the announce link or any indexer
setting. It builds a download URL against a fixed HTTPS origin
(`https://www.iptorrents.com`) using the captured torrent id, the release name
and the saved passkey, then fetches it through the ordinary IPTorrents HTTPS
adapter with normal certificate verification, bounded size and no redirects.

The IRC **Skip TLS certificate verification** option is deliberately **not**
consulted for this download. Relaxing the IRC certificate never relaxes the
HTTPS download: an expired IRC certificate cannot let an untrusted server
inject a torrent.

### Instant grabs and zero-seeder stalls

An announce grab is submitted the moment the tracker advertises the release,
which is frequently before the swarm has a reachable seeder. To keep the
transfer from waiting on qBittorrent's own slow periodic re-announce,
ReelScarey polls freshly submitted torrents every eight seconds and, whenever
the client reports a swarm with no seeds while the download has not started,
sends an immediate qBittorrent `reannounce` for that infohash. Re-announces are
spaced at least twenty seconds apart and capped per job, then stop once the
download begins. This only accelerates peer discovery; it never deletes,
retries or replaces a release. See [download clients](DOWNLOAD_CLIENTS.md).

### No retroactive grabs, no restarts

Enabling automatic grabbing only affects announces captured **after** you save
it. Announces already recorded while the source was capture-only stay
capture-only; you do not get retroactive grabs. Similarly, changing the source
(including the passkey or the opt-in) writes a new revision and fences earlier
captured work, which then finishes as `stale_source`/`stale_policy` rather than
being applied.

No application restart or manual reconnect is needed. The durable worker runs
continuously, and a source whose settings change reconnects itself at the new
revision. Avoid deleting and re-adding a source to "refresh" it — editing the
saved source is enough, and a re-add starts a new capture history.

## Security and bounds

- The IRC password is encrypted with the application key. It is never returned
  by the API or written to logs.
- The tracker passkey (or other tracker download key) is encrypted with the
  application key, never returned by the API, and is used only to build the
  tracker download URL for an opted-in source.
- The raw IRC line is not stored. Events keep parsed fields and a SHA-256
  dedupe hash, so tracker links or credentials cannot leak from the list.
- History keeps 30 days and at most 5000 completed events. Pending, retry and
  in-flight automatic-grab work is retained until it reaches a terminal
  outcome, regardless of age or count.
- TLS with system roots, NickServ `IDENTIFY`, PING/PONG, reconnect with
  exponential backoff. SASL is not implemented.
- A per-source **Skip TLS certificate verification** option can connect to a
  tracker whose certificate is expired or untrusted (for example IPTorrents).
  It defaults to off, keeps the link encrypted but not authenticated, and logs
  a startup warning while it is enabled. It applies only to the IRC capture
  connection, not to the HTTPS download.

## API

- `GET /api/v1/announce-sources` — sources plus the six definitions.
- `POST /api/v1/announce-sources` — create and test-free save; the source
  starts paused unless `enabled` is true.
- `PUT /api/v1/announce-sources/{id}` — revision-checked save; blank password
  retains the saved one, a blank key retains the saved key, and omitting
  `skip_tls_verify` or `auto_grab` keeps the saved value.
- `DELETE /api/v1/announce-sources/{id}` — `{revision}`; deletes the encrypted
  password and captured events.
- `GET /api/v1/announces?page=1` — newest 50 parsed events, each with its
  status (`outcome`, `message`, `attempts`, `download_id`).

A source object reports `auto_grab`, `has_key`, `key_label` and
`skip_tls_verify` so the UI can show whether automatic grabbing and a download
key are saved. Enabling `auto_grab` for any tracker other than IPTorrents, or
without a saved key, is rejected.

## Limits

- **Automatic grabbing is implemented only for IPTorrents.** TorrentLeech,
  FileList, BeyondHD, AnimeBytes and MyAnonamouse remain capture-only, and an
  opt-in for them is refused.
- Automatic grabbing covers monitored missing exact movies and ordinary single
  TV episodes only. Season packs, ranges, specials, Anime, ambiguous titles and
  registered files always stay for manual review.
- Search for these private trackers is not part of this release; the existing
  IPTorrents/XSpeeds/Nyaa/RSS/Torznab/Newznab adapters remain the search paths.
- The automatic-grab path builds its own fixed-origin HTTPS download URL; it
  never fetches the announce-supplied link and never uses the IRC TLS setting
  for the download.

## Readiness and deferred live testing

New sources are saved **paused** by default. Saving stores the encrypted IRC
password and the tracker key without opening a connection; enable the source
only when you are ready. Use a **dedicated bot nick** that no other IRC client
or announce tool is using. Two clients connected with the same nick will
disconnect each other, so the previous announce tool must be disconnected (or
use a different nick) before live acceptance.

The IRC client, parser, storage, automatic-grab worker and UI are covered by
fixtures, a fake IRC server and the full image build. Live tracker acceptance
remains owner-scheduled, and automatic grabbing against a live tracker has not
been accepted in production.

## Tracker download key (0.34.4)

Each announce source can store the tracker credential used to build a direct
download URL when automatic grabbing is enabled:

- IPTorrents: **Passkey** from the details page.
- TorrentLeech and BeyondHD: **RSS key** from the profile.
- FileList and AnimeBytes: **Passkey** from the account page.
- MyAnonamouse: **session cookie (mam_id)**.

The key is encrypted with the application key, never returned by the API, and
is used only for an explicitly enabled source. Leaving the field blank on edit
keeps the saved key. A source can be saved paused with the key now.
Automatic grabbing currently consumes only the IPTorrents passkey; the other
trackers' keys are stored for their capture-only sources.
