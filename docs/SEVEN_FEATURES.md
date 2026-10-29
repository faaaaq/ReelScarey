# Metadata, subtitles, source subscriptions and matching

Version 0.22 adds six append-only migrations (32–37). The application remains
one Go process with embedded web assets and one SQLite database in one Docker
container. Back up `/config` before upgrading. The backup manager includes the
new settings and catalog tables and pauses the new schedules after restoration.

## Metadata refresh and calendar

Open **Metadata refresh** to request a pass or enable a schedule. New schedules
start paused. Choose 1–720 hours, 1–25 titles per batch, and whether to limit
refreshes to monitored titles. Batches rotate through saved titles. TV refreshes
rotate through at most five loaded or monitored upcoming seasons per title per
pass. Metadata requires the existing TMDb connection; an absent provider is a
visible waiting state. Changes during a fetch invalidate its write permission.

Canonical IDs, monitoring decisions and registered media are preserved. Removed
provider episodes are retained. Calendar reads remain local and now show saved
runtime, series status and metadata freshness; refreshing is an explicit worker
action. Scheduling cannot infer missing air dates or guarantee provider accuracy.

On an Anime title's order page, **Review refreshed AniList details** fetches only
the already-linked identities and creates an immutable review. Applying that
review updates cached facts while preserving canonical TMDb IDs and explicit
episode mappings. Title similarity and provider counts never choose ordering.

## Subtitle preferences and reviewed edits

Each series has an expandable subtitle preference editor. Inherit global tracks
or save language, full/signs/forced tracks, hearing-impaired and match policies
for that show. The global scheduler remains the master switch. Changes fence
pending selections and do not themselves authorize a provider download.

Expand **Edit existing subtitles** on a registered file to review a timing shift,
upload a replacement, or restore a retained version. These actions support UTF-8
SRT, VTT, ASS and SSA sidecars up to 4 MiB, beside their registered video. Timing
shifts are bounded to ten minutes in either direction; invalid or negative cue
times are rejected. Embedded subtitles are outside this editor's scope.

Review and consent are required before a durable worker changes the sidecar.
The worker verifies the media and subtitle identities, stages and syncs the new
content, retains a verified original under `.reelscarey-subtitle-history` at the
media root, and journals publication and catalog changes. Interrupted switches
resume; foreign destination files are preserved and require attention. Retry is
available after resolving a blocked operation. Rollback itself is a fresh review.
Video bytes are never edited by this workflow. Original sidecar versions are
media files: include them in a media backup; configuration ZIPs exclude them.

## YouTube subscriptions and bulk acquisition

Open **YouTube subscriptions** to save a public channel or playlist into a custom
collection. Subscriptions start paused in review mode. The first successful check
records a baseline of up to the latest 100 visible videos without downloading a
backlog. Intervals range from 1–168 hours. Review mode discovers new entries for
explicit selection. After initialization, an explicit acquisition consent can
enable queued acquisition of subsequently discovered videos, at most 25 per pass.
Enabling acquisition does not enqueue the already-discovered backlog. Duplicate
videos use the existing acquisition reservations. Removing a subscription keeps
its acquired files and existing jobs.

Playlist/channel results and subscription entries support selection of up to 25
videos, destination/subtitle review, and an atomic queue operation. Stale reviews
reject the entire batch. The existing final import review is still required
before downloaded files publish to the library. Public source availability,
cookies, geographic restrictions and yt-dlp support remain external constraints.

## Release scoring

Quality profiles accept ordered source preferences (`remux`, `bluray`, `webdl`,
`webrip`, `hdtv`, `dvd`), ordered codec preferences (`av1`, `h265`, `h264`) and
comma-separated preferred terms. Ranking explains the observed labels and bonus
points. These soft preferences cannot override required identity or eligibility
checks. Existing per-series codec/group rules remain explicit hard requirements.

Usenet profiles additionally support a maximum age of 0–10000 days and minimum
propagation of 0–10080 minutes. Zero disables the corresponding filter. When an
age rule is active, unknown, invalid or future publication dates are rejected.
Torrent seed rules and dates are unaffected. The same rules and revision fences
apply to automatic movie, TV and Anime searches.

## Bulk matching

On an existing-library scan, select up to 25 unresolved files from the current
page and request suggestions. Saved catalog suggestions are local. Optional TMDb
lookups are bounded to five distinct queries per batch. Suggestions show their
evidence and never select a match automatically. Provider results can be added as
paused catalog titles, then reviewed against explicit loaded canonical episodes.
Ambiguous files can use the existing individual match editor.

Choose every title and episode, review the complete batch, and consent to queue
registration. The complete snapshot is rechecked atomically: changed sources,
catalog facts, episode revisions, scopes or reservations reject the batch. The
existing verification worker registers files in place and preserves original
bytes without inventing download history. Suggestions alone do not register,
rename, move or acquire files.
