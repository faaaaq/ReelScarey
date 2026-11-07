# Scheduled searches and quality profiles

Open Automation in the sidebar. New installations and upgrades start with the
schedule paused, search-only mode, a six-hour interval, ten targets per run and
the HD balanced profile. Saving preferences does not connect any integration.
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

Searches only include effectively monitored targets with known calendar release/
air dates on or before today's UTC date. TV shows, seasons and episodes must all
be monitored; load season metadata explicitly in the TV library first. Pausing a
parent retains child selections. Movie monitoring can be changed alongside its
automation profile. Future and unknown dates wait for metadata. The scheduler
does not refresh TMDb, discover unloaded episodes or scan existing media folders.

Movie matches require the canonical normalized title immediately followed by its
year. TV matches require the canonical show title (optionally its year) and one
exact SxxEyy token. Pack/range/ambiguous names are excluded from automatic
selection; manual episode/season searching and mapping remain available.
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
not silently blocked by these automation-only rules.

## Title searches from library pages

**Search** buttons on a movie, show or episode (and in the Activity review
flows) trigger one focused automatic search for that title via
`POST /automation/search-item`. The run is the ordinary scheduled pipeline with
its lease, retries, batching and mode: download mode queues the best matching
release, search-only mode records candidates for review. A show-scoped search
continues across runs until its missing monitored episodes are done. The
latest recorded outcome per title is readable at
`GET /automation/item-results`.

## Modes and file safety

Search mode records decisions without queuing downloads. Download mode queues
the best matching **missing** target through the existing transfer service. A
completed TV download waits for episode/file review in Activity unless it is
unambiguous (one video, one reserved episode, exact filename evidence) and then
imports automatically with the same journaling; see TV_DOWNLOADS.md. Existing
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
