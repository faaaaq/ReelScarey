# Scheduled searches and quality profiles

Open Automation in the sidebar. New installations and upgrades start with the
missing-title search schedule paused, search-only mode, a six-hour interval, ten
targets per run and the HD balanced profile. The separate release feed check
starts **enabled every 15 minutes** (see Release feed sync below). Saving
preferences does not connect any integration.
Torznab setup may be deferred; enabled schedules record a waiting state and make
no search/download requests until an indexer exists. Search now is available
after connection and requests one run using the saved mode, even while paused.
It does not enable the recurring schedule.

## Selection and profiles

Profiles allow ordered resolutions 480p, 720p, 1080p and 2160p. Choose a resolution
cutoff among allowed values, a release-language preference, minimum seed count,
maximum release size in GiB, and comma-separated required/excluded terms. Every
required term must appear; any excluded term rejects the candidate. Terms match
normalized whole words/phrases, ignoring case and punctuation. At most 20 profiles
are supported. Profiles are edited in place with revision checks; deletion is not
exposed. Item assignments choose a profile or follow the schedule default.
Assignment and Wanted/Calendar alphabetical ordering ignore a leading
case-insensitive `The ` while displaying the saved title unchanged.

Searches include every effectively monitored target with no registered file and
no active job. A movie is searched whatever its saved release date, so a release
that lands earlier than its scheduled date is not missed. TV episodes still need
a known air date on or before today's date in the server's configured timezone,
and the show, season and episode
must all be monitored; load season metadata explicitly in the TV library first.
Pausing a parent retains child selections. Movie monitoring can be changed
alongside its automation profile. The scheduler does not refresh TMDb, discover
unloaded episodes or scan existing media folders.

The show-level **Monitor all future episodes** action (`monitor-all`) applies a
different, explicit owner rule: it turns the show on, monitors only regular
episodes whose `air_date` is a valid full YYYY-MM-DD date on or after today's
date in the server timezone, and explicitly unmonitors regular episodes and
seasons with no such
episode. A regular season is monitored only when it contains such an episode or
its own `air_date` is valid and today-or-later. Past, undated and invalid regular
episodes are therefore off; specials keep their explicit choices. The
library-wide bulk variant uses the same rule for every show in scope.

A download-mode run needs a usable client before targets are checked.
`client_connected` and the run's readiness include an enabled explicit
per-protocol default, the legacy settings connection, the enabled built-in
qBittorrent torrent fallback, and the enabled built-in SABnzbd client when a news
server is saved. A disabled or unconfigured client still produces
`waiting_client`; readiness never submits a job by itself.

Movie matches require the canonical normalized title immediately followed by its
year. TV matches require the canonical show title (optionally its year) and one
exact SxxEyy token; a single presenter or host name may lead the release title as
long as the canonical show title closes the episode prefix as a whole word (for
example "Jimmy Carrs Am I The Asshole S02E02" for "Am I the Asshole?"), so
run-together or reversed prefixes stay rejected. Pack/range/ambiguous names are
excluded from automatic selection; manual episode/season searching and mapping
remain available.
Resolution must be explicitly labeled. Camera recordings, screeners, samples,
trailers, conflicting labels, unknown/nonpositive size and unknown seed counts
are rejected, even with a zero seed minimum. Required language must appear in
the release suffix as a supported full language name (English also accepts ENG).
MULTI alone proves no language. These are conservative title claims, not verified
audio tracks. Exact matching intentionally favors false negatives over unrelated
automatic downloads. Review history when legitimate releases are excluded.

Eligible releases rank by the cutoff first, then lower allowed resolutions from
highest to lowest, then above-cutoff resolutions from lowest to highest. Within
one resolution, the known torrent seed count dominates (doubled, capped at
4000 seeds), so the best-seeded available release wins when the search runs;
source, codec and preferred-term bonuses rank just below it. Usenet releases
have no seed bonus. Title order breaks remaining ties. The indexer supplies at
most 50 candidates; the ten highest ranked decisions
and their rejection reasons are retained per target. Manual release choices are
not silently blocked by these automation-only rules. A show-scoped automatic
search tries one eligible season pack first for each season with multiple
missing episodes; if no pack qualifies, it searches the individual episodes.
Pack titles are still excluded from single-episode identity matching.

## Title searches from library pages

**Search** buttons on a movie, show or episode (and in the Activity review
flows) trigger one focused automatic search for that title via
`POST /automation/search-item`. The run is the ordinary scheduled pipeline with
its lease, retries, batching and mode: download mode queues the best matching
release, search-only mode records candidates for review. A title or Calendar
bulk search is one bounded pass: it continues across batches until every
selected target has been checked once, then clears the request even when some
targets stay missing. Missing targets are never re-searched by the same request;
the ordinary scheduled cycle picks them up later. An explicit title or bulk
search also covers a monitored Anime show whose per-show automatic Anime search
is off, using standard season/episode identity for that request; the saved Anime
policy is unchanged. The latest recorded outcome per title is readable at
`GET /automation/item-results`.

Adding a monitored movie or show requests the same focused search once
(search-on-add). It follows the saved automatic-search mode; a newly added show
still needs its season episodes loaded (or a feed entry for an episode) before
the search can see them.

## Release feed sync (Sonarr/Radarr-style)

Freely adapted from the way Sonarr and Radarr watch indexer feeds: a separate
release-check schedule watches every enabled indexer's newest releases instead
of waiting for an hourly search of the whole library.

Owner decision in 0.40: every installation starts with the release check
**enabled every 15 minutes**. This deliberately overrides the older "new
schedules start paused" rule for this check only. The missing-title search
schedule above still starts paused and search-only.

Every cycle asks each enabled source for its newest releases without a title
query: `t=search` with no `q` for Torznab and Newznab, and the provider's RSS
endpoint or first listing page for the direct RSS, NYAA, IPTorrents and XSpeeds
adapters. Each unseen feed entry is matched against monitored, missing movies and
loaded episodes using the same exact title/year, single `SxxEyy`,
quality, language, size and seed rules as automatic searches. A matching entry
is grabbed immediately through the normal encrypted selection, reservation,
client submission and journaled import path. It is grabbed even while the
missing-title schedule is paused or in search-only mode; manual release searches
still return results for review.

Feed checking **never replaces registered files**. Movie versions and episodes
that already have a file are skipped, and upgrades remain suggestions. Packs,
ranges, ambiguous labels, anime absolute/mapped number forms without an exact
standard `SxxEyy` token, and titles without an exact year stay with the scheduled
or manual flows. Each `(indexer, feed identity)` pair is processed once; seen
keys are retained for 180 days and the newest 2,000 result rows are kept.

Indexer errors are per source: one failing feed does not stop the others and the
next cycle retries it. A temporary client or storage failure leaves the entry
unseen for the next check; a profile rejection is recorded once. The interval
supports 5–1440 minutes, each pass has an eight-minute budget, one leased worker
and a 20,000-target matching index, and settings changes release the lease. Results and run history contain
safe release metadata, never download URLs or credentials. Most HTML tracker
pages expose only their newest listing page, so a release that never appears
there is still found by the scheduled missing-title search.

## Modes and file safety

Search mode records decisions without queuing downloads. Download mode queues
the best matching **missing** target through the existing transfer service. A
completed TV download maps itself when every reserved episode has exactly one
matching file (single episodes and labeled packs); replacements and incomplete or
ambiguous mappings wait for episode/file review in Activity. The same journaling,
ffprobe verification and no-overwrite import run either way; see TV_DOWNLOADS.md.
Existing
hash/target reservations, encrypted payloads, authenticated client submission,
source retention, collision refusal and journaled imports are shared with manual
downloads. A queue policy check runs transactionally after metainfo retrieval and
before reservation, rejecting changed monitoring, profiles, assignments, schedule
revision or an expired lease. Pausing does not cancel already queued downloads.

Existing registered files use stored FFprobe dimensions to determine a resolution
tier, including typical cropped widescreen sizes. At/above-cutoff files require
no indexer request. Unknown inspected dimensions are skipped. Below-cutoff files
may produce an upgrade suggestion for an eligible higher resolution. **Existing
files are never replaced, removed, renamed or overwritten**, and upgrade
suggestions do not queue replacement downloads. Safe versioned replacement
imports, audio-language verification and source/codec scoring are future work.

## Persistence, failures and limits

The singleton schedule supports 15–10080 minute intervals and 1–25 targets per
run. A persisted cursor rotates across eligible movie and episode IDs rather
than repeatedly searching only the first titles. Each run has an eight-minute
budget. Claims use random owners with 60-second leases renewed every 15 seconds;
expired runs are marked interrupted and reclaimed. Shutdown releases the lease
and preserves schedule/cursor state. This is at-least-once execution with guarded
idempotent effects, not an exactly-once guarantee.

Provider/storage failures retry at 1, 2, 4 and 8 minutes, up to five attempts in a
retry sequence. Exhausted one-off requests stop; enabled recurring schedules
remain enabled for their next interval. Settings changes reset retry intent.
Missing integrations wait until the next interval or a new one-off request,
without repeated network calls. Only one run can be requested/running at a time.
Manual and scheduled searches may invalidate old opaque release IDs; fresh review
links perform a new search rather than queueing an expired history selection.

Transfer independently retries temporary download/import failures and exposes
blocked/failed jobs in Activity. Those jobs retain reservations and prevent a
second automatic download for the target until resolved or stopped tracking.
Automatic failure handling never deletes a torrent/source or silently submits a
different release. Retry/resume and stop-tracking retain their existing safeguards.

Run history retains 100 runs, cascading result cleanup; the UI lists the latest
30. Each run retains at most 25 target results and ten decisions per target.
Records contain safe release metadata and opaque IDs, not credentials or download
URLs. All settings, assignments, requests and profile mutations require login,
same-origin validation and CSRF. Schedule/profile/assignment edits use optimistic
revisions; reload saved values after a conflict.

Migration 006 only adds tables; prior migrations, sessions, credentials, media
registrations and download/import journals are preserved. Back up the stopped
config directory (including its encryption key) before upgrading. Downgrading
the database in place is not supported. See VALIDATION.md for executed checks.
