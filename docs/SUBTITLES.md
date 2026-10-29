# Subtitle setup and operations

Open **Subtitles** in ReelScarey. The first provider is OpenSubtitles.com; Torznab
and qBittorrent are not needed to configure or use subtitles. Subtitle activity
appears for media registered by completed ReelScarey movie/TV imports. This
milestone does not scan arbitrary existing library folders.

## Connection and preferences

Enter your OpenSubtitles.com username, password, application API key and its
registered application name/version directly in the form. Save & test signs in
before replacing the saved connection. The key/password are encrypted using the
existing config key and never returned to the browser. Blank secret fields retain
saved values. Back up the entire config directory, including `secrets.key`, with
the process stopped. Missing/wrong keys fail closed. A test does not request a
subtitle download, although account/API access restrictions still apply.

Default choices match the owner's request:

| Language | Track | New sidecar name |
| --- | --- | --- |
| English | Full dialogue | `<video>.en.srt` |
| English | Signs / songs | `<video>.en.signs.srt` |
| English | Forced / foreign parts | `<video>.en.forced.srt` |

These are three independently requested tracks. Providers do not have a universal
signs/songs flag: ReelScarey requires an explicit signs label in release/file names.
Forced tracks require the foreign-parts provider flag or an explicit forced label.
A signs label takes precedence when both occur; one provider file is assigned one
variant and is never downloaded twice as signs and forced. Availability depends
on what the provider actually supplies. Provider-labeled AI/machine translations
and multi-disc subtitles are excluded.

You can select up to three supported languages/nine unique track combinations,
allow/exclude/prefer hearing-impaired (SDH) subtitles, require file-hash matching
only, and change the 60–10080-minute interval. The default is six hours and
monitored items only. Automatic downloads start **paused**. Saving preferences
does not require a provider; enabled work waits until connected. Language rules
are global in this milestone, with no per-show override or verified audio-language
rule. Signs/forced variants can be requested for any registered movie/show; they
are not conditional on an inferred spoken language.

## Detection and timing

Each check opens only a registered video under the media root. The first check
verifies its registered SHA-256 and uses FFprobe for subtitle stream language,
title and forced/SDH dispositions. The inspection cache reuses results when the
registered SHA, size and nanosecond mtime agree. This is a fingerprint cache,
not a full rehash on every poll; edits that deliberately preserve size and mtime
are outside that detection guarantee. Publication also verifies that the opened
video and its visible location have not changed.

Embedded streams and same-stem SRT, VTT, ASS and SSA sidecars are inspected before
provider calls. Language aliases such as `eng` are recognized, with `forced`,
`signs`/`songs` and `sdh`/`hi`/`cc` suffixes. Existing ASS/SSA recognition checks
basic text/event structure, not a complete parser. Invalid matching sidecars
remain unverified and are preserved. An exact same-stem sidecar without a
language label pauses automatic downloads for review. Unsupported naming patterns
are preserved but may not satisfy a track. No sidecar or embedded stream is removed.

Searches use the catalog's canonical TMDb movie or parent-show/season/episode
identity, requested languages and the OpenSubtitles video hash when available.
That hash requires at least 128 KiB. Automatic selection requires provider-confirmed
catalog identity plus either a matching file hash or an exact normalized original
download release name. Canonical renamed library filenames are not used as release
evidence. A shared multi-episode video requires a hash; matching only its first
episode/release is insufficient for automatic selection. Provider matches are
evidence, not a guarantee of perfect synchronization or correct content labels.

**Search subtitles** queues a search for review without downloading. Candidates
show their language/type and timing reasons. Confirm the review checkbox before
downloading an eligible choice. Weaker timing matches can be chosen manually;
unconfirmed catalog identity cannot. Choices expire after 30 minutes and become
invalid when preferences, provider generation or registered video SHA change.

## Automatic work, quotas and recovery

A worker in the same server discovers at most 50 registered files per sweep using
a persisted rotating cursor. It handles each file with a random 60-second lease,
renewed every 15 seconds, and an eight-minute work limit. Temporary failures retry
up to five attempts with bounded backoff. Blocked work needs manual retry. A
scheduled check following a completed manual request returns to automatic mode;
it does not reuse an expired manual selection. Manual requests can inspect
unmonitored files while automatic work follows monitoring preferences.

OpenSubtitles API calls are serialized with one-second spacing. JWTs stay in
memory and can refresh once after rejection. Rejected account/API access waits
for an explicit connection test/update. Reported quota/rate cooldowns are stored
across restarts and shared by all files; reported remaining downloads appear in
the connection panel. A zero allowance after one subtitle retains that successful
file and waits before the next. The provider charges its `/download` request;
failures after that request can still consume allowance. There is no promise of
exactly-once provider charging across an interruption before bytes are journaled.

New downloads default to SRT. **Download format → Keep original SRT, VTT, ASS
or SSA** preserves a supported provider file's format, styling and positioning.
This applies to both manual and automatic selections and does not replace
existing tracks. Unsupported original extensions require SRT conversion or a
different selection. The candidate shows its output format before review.

Files remain bounded to 4 MiB of UTF-8 text. BOM/CRLF are normalized; cue text,
timestamps, ASS/SSA style/event fields and VTT style/comment blocks are retained.
Timed cues are checked against video duration with a three-minute tolerance.
HTML/error pages, format mismatches, malformed timestamps, missing event fields
and empty cues are rejected. Keeping original styled files omits the provider's
conversion flag; a converted/mislabeled response is rejected rather than saved
under the wrong extension. External font files are not downloaded. The protocol
follows the [official download contract](https://ai.opensubtitles.com/api-docs).
Quota resets accept numeric epochs or the documented human-readable reset plus
UTC timestamp. Account/CDN failures can still consume provider allowance.

Changing the format preference invalidates unconsumed candidate reviews. An
already journaled import retains its original extension/content and recovers
without consuming quota again. Backups preserve this preference and all pending
journals. Imported ASS/SSA/VTT can use the existing reviewed timing, replacement
and retained-version rollback tools. SRT conversion can discard styling; choose
original format when those details matter.

Verified content is journaled before a private staging file is written and synced
in the video's folder. Exclusive same-folder hardlink publication refuses overwrite;
the media filesystem must support hardlinks even if video imports use copies.
Registration commits after verification; journaled content is then cleared.
Interrupted planned/staged/published work resumes from saved bytes without another
provider request. A partial stage is preserved while a fresh stage is allocated;
conflicting complete stages remain for review. Only a journaled committed stage
proven to be the same verified file as its destination is cleaned. Unknown and
unreferenced files are never swept. Media/config roots must be separate and paths
must not contain symlinks. The video is opened read-only throughout.

Preferences, monitoring, connection generation and worker lease are checked before
and after fetch and under the publication transaction. Pausing/disconnecting stops
subsequent publication; already committed subtitles stay present. Existing videos
and subtitles are never replaced. There is no subtitle deletion, replacement,
automatic resynchronization, translation, OCR or provider fallback in this milestone.

## Protocol references and validation

The adapter follows the provider's [REST API reference](https://ai.opensubtitles.com/pt-br/api-docs)
and [official file-hash specification](https://opensubtitles.github.io/oshash/).
The hash is implemented from the documented unsigned 64-bit formula; no provider
reference implementation or other media manager's source was copied. Fixed API
hosts are `api.opensubtitles.com` and `vip-api.opensubtitles.com`. API redirects are
not followed. Signed file links/redirects allow HTTPS/default port only on
`dl.opensubtitles.com`, `www.opensubtitles.com` and `dl.opensubtitles.org`; account,
API-key and cookie headers are not forwarded to those file hosts. Other locations
fail closed and may need a future allowlist update if the provider changes.

Contract tests exercise the real adapter using owned HTTP protocol responses;
workflow tests use disposable media roots. Opt-in generated-video acceptance uses
`REELSCAREY_LIVE_SUBTITLES=1 go test ./internal/subtitle -run TestRealSubtitleMedia -v`
with FFmpeg/FFprobe installed. It tests actual embedded track detection and three
sidecar publications with a fixture provider. This does not claim a live owner
OpenSubtitles media-download acceptance test. The owner now reports a privately
saved connection; a real sidecar check still needs imported media. See
VALIDATION.md for executed results.
