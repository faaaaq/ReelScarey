# IRC announce feeds (0.34)

ReelScarey can watch tracker IRC announce channels and keep a bounded history
of new releases. This release captures and displays announces; it does **not**
start downloads automatically. Automatic grabbing is paused at the owner's
request. Use the tracker link or attach the `.torrent` manually.

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
   encrypted, never returned by the API and unused until automatic grabbing is
   enabled, so it can be left blank for capture-only. It lives in the field
   labelled by the tracker definition (here **Passkey**).
6. **Invite command**: leave **blank**.
7. **Skip TLS certificate verification**: leave this **off** first. Only turn it
   on if saving the source shows a connection error such as `tls: failed to
   verify certificate: x509: certificate has expired or is not yet valid`.
   IPTorrents' IRC server has served an expired certificate; the option lets the
   reader connect anyway. It keeps the link encrypted but stops checking the
   server's identity, so use it only for a tracker you trust.
8. Tick **Connect this source now**, then **Add source**.

The row then shows **Enabled**. That means the worker has a running source, not
that the socket is proven connected; a connection error appears under the source
and the first release appears in **Recent announces**. To confirm IPTorrents is
live, wait for one announce (the channel is busy, so this is usually under a
minute) or open **Edit** to re-check the saved values.

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

Search and automatic grabbing are still separate (see **Limits**): the announce
feed records releases for review, it does not download them.

## Security and bounds

- The IRC password is encrypted with the application key. It is never returned
  by the API or written to logs.
- The raw IRC line is not stored. Events keep parsed fields and a SHA-256
  dedupe hash, so tracker links or credentials cannot leak from the list.
- History keeps 30 days and at most 5000 events.
- TLS with system roots, NickServ `IDENTIFY`, PING/PONG, reconnect with
  exponential backoff. SASL is not implemented.
- A per-source **Skip TLS certificate verification** option can connect to a
  tracker whose certificate is expired or untrusted (for example IPTorrents).
  It defaults to off, keeps the link encrypted but not authenticated, and logs
  a startup warning while it is enabled.

## API

- `GET /api/v1/announce-sources` — sources plus the six definitions.
- `POST /api/v1/announce-sources` — create and test-free save; the source
  starts paused unless `enabled` is true.
- `PUT /api/v1/announce-sources/{id}` — revision-checked save; blank password
  retains the saved one, and omitting `skip_tls_verify` keeps the saved value.
- `DELETE /api/v1/announce-sources/{id}` — `{revision}`; deletes the encrypted
  password and captured events.
- `GET /api/v1/announces?page=1` — newest 50 parsed events.

## Limits

- Search for these private trackers is not part of this release; the existing
  IPTorrents/XSpeeds/Nyaa/RSS/Torznab/Newznab adapters remain the search paths.
- Automatic queueing and download-URL fetching are deferred at the owner's
  request. The saved key is ready for when that work resumes.
## Readiness and deferred live testing

New sources are saved **paused** by default. Saving stores the encrypted IRC
password without opening a connection; enable the source only when you are
ready. Use a **dedicated bot nick** that no other IRC client or announce tool is
using. Two clients connected with the same nick will disconnect each other, so
the previous announce tool must be disconnected (or use a different nick)
before live acceptance.

The IRC client, parser, storage and UI are covered by fixtures, a fake IRC
server and the full image build. Live tracker acceptance remains owner-scheduled
and is intentionally not run against a tracker while another client owns the
nick.
## Tracker download key (0.34.4)

Each announce source can store the tracker credential used to build a direct
download URL when automatic grabbing is enabled:

- IPTorrents: **Passkey** from the details page.
- TorrentLeech and BeyondHD: **RSS key** from the profile.
- FileList and AnimeBytes: **Passkey** from the account page.
- MyAnonamouse: **session cookie (mam_id)**.

The key is encrypted with the application key, never returned by the API, and
is not used until the automatic-grab step is enabled. Leaving the field blank
on edit keeps the saved key. A source can be saved paused with the key now.
