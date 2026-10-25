# Automatic Anime episode searches (0.17 development)

## Enable a show explicitly

Open an episodic title in the Anime section, then **Edit automatic searches**.
Every show starts with automatic Anime searching **Off**. Choose a numbering
mode, enter any alternate release titles, review the changes, then save. This
changes only the show's search policy; it does not start a search, enable the
global schedule, change monitoring or alter existing downloads/files.

| Mode | Episode identity used for searching and matching |
| --- | --- |
| Off | The Anime show is excluded from automatic episode searches. |
| Season and episode | The saved canonical TMDb season/episode, such as S02E01. |
| Absolute episode number | The explicit number saved for that canonical episode in Library section. No number is inferred from season order. |

Global Automation settings still determine scheduled versus requested one-off
runs and search-only versus download mode. Parent show, season and episode
monitoring, loaded metadata, valid released dates, active reservations and quality
profiles remain applicable. Specials (season zero) are excluded from automatic
Anime searches. Anime movies continue through the ordinary movie workflow.

Otherwise eligible absolute-mode episodes without a saved number produce
**needs_numbering** in the run results, without contacting indexers or queueing
that episode. Other eligible
episodes can continue. The editor's loaded/ready/missing counts describe loaded
non-special episodes and saved numbering coverage. They are not counts of
monitored, released, missing or currently downloadable episodes.

## Alternate titles and provider queries

The canonical catalog title remains unchanged. Enter up to eight explicit
alternate release titles, one per line, with 1–200 UTF-8 bytes per title. Names
must contain letters or numbers, have no control characters, and remain distinct
from the canonical name and each other after case/punctuation normalization.
Choose the canonical name or one of those names as the query title. An empty
saved `search_title` follows the current canonical title. When searching is
enabled, the chosen query title must fit 180 bytes so an episode suffix fits the
200-byte query limit.

Only the chosen name is sent as the title portion of provider queries. Alternate
names are accepted matching evidence, not an unbounded provider fan-out:

- Season mode uses one query, such as `Chosen Title S02E01`.
- Absolute mode uses up to three distinct numeric forms. For absolute 5 these
  are `Chosen Title 5`, `Chosen Title 05` and `Chosen Title 005`; equivalent
  padded forms are deduplicated.

The numeric variants share a ten-second search budget. Responses are interleaved,
source selections are deduplicated, and at most 200 candidates are retained for
the episode. Searches use the existing enabled indexers and TV category. No
provider identity, language, genre or filename automatically creates an alias.
AniDB/AniList identity and external alternate-order lookup are not introduced.

## Conservative single-episode matching

Automatic selection requires an exact normalized canonical or explicit alternate
title followed by one identity in the selected mode. A matching saved show year
may appear between the name and episode label. One bounded leading release-group
tag is allowed; it cannot masquerade as an episode, resolution or pack label.

Examples for canonical S02E01 with explicit absolute 25:

| Release name | Season mode | Absolute mode |
| --- | --- | --- |
| `[Group] Title S02E01v2 [1080p][Japanese]` | Can match | Rejected |
| `[Group] Title - 025v2 [1080p][Japanese]` | Rejected | Can match |
| `Unlisted name - 025 [1080p]` | Rejected | Rejected |
| `Title S02E01-E02 [1080p]` | Rejected | Rejected |
| `Title - 025-026 [1080p]` | Rejected | Rejected |
| `Title - 025 Batch [1080p]` | Rejected | Rejected |

“Can match” means identity evidence only: the quality profile must also accept
the resolution, explicit language label, size, required/rejected terms and torrent
seed count where applicable. Language/resolution words inside a show title or
release-group tag do not establish technical evidence. Version suffixes v1–v99
do not change episode identity. A bare year or common resolution number requires
an explicit dash when used as an absolute episode label.

Ranges, repeated episode labels, extra unlabelled numbers, malformed brackets,
packs and special-content qualifiers such as OVA, OAD, specials, recap or extras
are refused for automatic selection. Recognized technical bit-depth/audio-channel
labels and complete bracketed checksums do not become extra episode numbers.
The parser deliberately rejects unfamiliar or ambiguous release grammars. Manual
release search and explicit mapping remain available for those cases; this is
not a universal Anime filename parser.

## Review, queue fences and immutable history

Saving requires the exact search-policy, category and numbering/metadata
revisions that were read. The UI offers a read-only review, disables editing
while saving and requires a fresh read after conflict or an uncertain response.
An unchanged policy is a no-op. Changes bump the policy, show and assignment
revisions, invalidating older editors and automatic queue decisions.

After metainfo is fetched, the queue transaction rechecks the run lease and
schedule, effective monitoring, quality rules, canonical target, category,
numbering, selected query title and complete alias policy. A preference changed
away and back cannot reuse an older decision. Target reservations and duplicate
release/hash handling remain shared with ordinary TV downloads.

A queued Anime job stores its search mode, chosen/canonical/alternate titles,
policy/category/numbering revisions, generated queries and absolute number as
audit evidence. Canonical episode IDs and explicit absolute aliases are also
stored in immutable `download_targets`. Activity displays the saved Anime policy.
Later policy or alias edits do not reinterpret queued targets, naming snapshots
or completed file mapping. Import identity comes from those canonical targets,
not from searching again or reading a newer alias list.

Automatic download queueing does **not** authorize a file import. Completed
episodes still wait for reviewed file-to-episode mappings in Activity. Sources
used for seeding, existing registered files and import no-overwrite protections
remain intact. Existing-file resolution upgrades remain suggestions; this
milestone does not introduce replacement imports.

## API and persistence

GET/PUT `/api/v1/library/items/{id}/anime-search` use the existing authenticated
application API. PUT requires CSRF, same origin and strict JSON within the ordinary
16 KiB limit. The handler has a five-second request budget.

GET returns:

```text
media_id, canonical_title, category, revision, category_revision,
numbering_revision, mode, search_title, alternate_titles,
loaded_count, ready_count, missing_number_count
```

An absent per-show policy reads as revision 0, disabled, with no alternate titles.
PUT accepts only:

```text
revision, category_revision, numbering_revision,
mode, search_title, alternate_titles
```

Invalid fields return 400, unavailable non-Anime/non-series writes return 409,
stale revisions return 409 and missing titles return 404. Existing series policy
reads may remain available after reclassification; writes require the Anime
section. Classification and absolute numbering retain their separate editor/API.

Additive migration 013 creates `anime_search_settings` and adds the download audit
snapshot. Schema becomes 13. It does not modify previous migration files, rename
media, replace credentials or opt existing shows into searches. Graphite/blue and
plain functional copy are retained. IPTorrents/XSpeeds account testing remains
deferred.

## Acceptance status

This document describes the implemented contracts and limits. The current
milestone's integrated browser, package, native and Linux acceptance must be
recorded separately in [VALIDATION.md](VALIDATION.md); source or test presence is
not a claim that every check has run. See [remaining work](REMAINING.md) for
deferred owner-service acceptance and product/release gaps.
