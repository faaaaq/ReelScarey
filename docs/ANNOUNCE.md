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

Open **Announce feeds**, choose a tracker and enter:

- **Source name** — any label.
- **IRC nick** — the bot nick.
- **NickServ account/password** — when the tracker requires it.
- **Invite command** — optional; for BeyondHD/AnimeBytes paste the command from
  the tracker's IRC page (for example `Millie announce YOURKEY`).

Server, port, TLS and channel are fixed from the tracker definition. Enable the
source to connect. The badge says **Enabled**, not "connected"; any connection
error appears under the source.

## Security and bounds

- The IRC password is encrypted with the application key. It is never returned
  by the API or written to logs.
- The raw IRC line is not stored. Events keep parsed fields and a SHA-256
  dedupe hash, so tracker links or credentials cannot leak from the list.
- History keeps 30 days and at most 5000 events.
- TLS with system roots, NickServ `IDENTIFY`, PING/PONG, reconnect with
  exponential backoff. SASL is not implemented.

## API

- `GET /api/v1/announce-sources` — sources plus the six definitions.
- `POST /api/v1/announce-sources` — create and test-free save; the source
  starts paused unless `enabled` is true.
- `PUT /api/v1/announce-sources/{id}` — revision-checked save; blank password
  retains the saved one.
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
