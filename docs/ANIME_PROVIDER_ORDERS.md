# Anime identities and reviewed episode orders

Version 0.18 adds AniList identity links, TMDb episode-group proposals and saved,
owner-reviewed release labels. It also supports explicitly selected Anime
specials and packs whose separate files span canonical seasons. TMDb remains the
canonical catalog: provider links do not replace media or episode IDs, move
episodes between seasons, change monitoring, or overwrite existing absolute
number aliases.

## Using the editor

1. Open a title from **Anime**, then choose **Edit Anime identity & mapping**.
   Search AniList and select the identities that belong to that title. Separate
   cours or specials can have separate links.
2. For a show, load the relevant seasons in its episode list. Enter a release
   title and label beside each episode, or load a TMDb episode order and review
   one subgroup's proposed labels before merging them into the draft.
3. Choose **Review Anime mapping changes**, check the canonical episode targets,
   then confirm and save. Canceling leaves the saved mapping unchanged.
4. To search a pack or special, select its saved mapped episodes in **Manual pack
   and special searches**. Review the target list, search, choose a release and
   confirm it. Activity keeps the mapping used for that download.
5. Once the download completes, review its files in Activity before importing.
   Saving an order never starts a search or changes files by itself.

Anime films support identity links; episode-order and pack controls apply to
shows. AniList needs no application key. TMDb order proposals use the existing
TMDb connection; release searches need an enabled indexer.

## Identity and order are separate decisions

AniList search uses the public GraphQL service without an owner API key. A saved
identity includes its provider ID, titles, format, available year/episode count
and canonical AniList link. Anime shows and films can link identities. Films do
not gain synthetic episodes or an episode-order mapping.

A title may retain up to twenty distinct AniList links. A review can resolve at
most eight new links, with one shared request deadline. Already-linked identity
facts are cached and reused. To fetch refreshed facts, remove the link and its
dependent rows, review and apply that change, then relink it in another review.
Changing a form without applying it does not remove or refresh the saved link.
Provider failures leave existing saved links available.

An identity is not evidence of an episode order. AniList episode counts, formats,
titles and relationships do not authorize offsets, sequel joins, special
classification or a TMDb episode match. Each saved order row explicitly joins
one loaded canonical episode to a release title and label. It may refer to a
linked AniList ID, or use provider ID zero for an owner-defined release order.

Supported labels are positive numeric labels, `SxxExx` (including season zero),
and positive `SP`, `OVA`, `ONA` or `OAD` labels. Leading zeroes and letter case
normalize, so `001` and `1` are the same label; `SP1` and `1` remain distinct.
Ranges are not single labels. Each canonical episode appears at most once.
Normalized release title plus normalized typed label must be unique across all
provider links: a filename cannot disambiguate two identical labels by an
invisible provider ID. Release titles are at most 180 UTF-8 bytes; a reviewed
order contains at most 5,000 loaded episodes.

## TMDb proposals

The order editor can list episode groups attached to the saved TMDb show. The
provider adapter verifies group membership before fetching its details and
rejects episodes belonging to another show.

Choose one subgroup, a release title, a label prefix and a starting number.
The prefixes are `episode` (a plain number), `SP`, `OVA`, `ONA` and `OAD`. Starting
numbers range from 1 through 100,000, and the resulting labels must fit that
range. The chosen subgroup must have distinct, contiguous zero-based positions.
The app never flattens every subgroup into one inferred franchise order.

Proposals join exact TMDb episode IDs to loaded canonical episodes and verify
their season and episode numbers. An unknown ID or conflicting canonical
position is returned as unmatched, rather than guessed from a title or number.
Unmatched positions retain their place in the proposed numbering; later rows
are not shifted to hide gaps. With no subgroup selected, the response lists
subgroups and produces no mapping rows.

A proposal is read-only. Review its rows before creating and applying an order
review. The saved order records the owner's chosen labels and identity facts;
it is not a cached, independently verified copy of the complete TMDb group or
proof that a release follows that provider order. Provider corrections do not
silently rewrite saved rows.

## Review, revisions and persistence

Preview compares the current order, category and canonical-numbering revisions
before contacting identity providers. It validates all selected IDs and local
rows first. Successful preview persists a bounded review for thirty minutes;
it does not apply links, change aliases or monitoring, enqueue work, or touch
media files. Applying its opaque review ID is a separate action.

Apply is atomic, bound to the same title and revisions, and single use. Expired,
already-applied, foreign-title and stale reviews are rejected. An actual change
increments the order revision, the series metadata revision when applicable,
and the item automation revision. A no-op consumes its review without bumping
those revisions. Reviews and applied orders survive database reopen.

These fences prevent an older editor or a pending queue decision from adopting
new mappings silently. They do not reinterpret downloads that were already
queued: those downloads retain their exact canonical targets, provider facts,
release labels and organization snapshot.

## Specials and multi-season packs

Manual pack search takes 1–100 explicitly selected, loaded, missing, unreserved
canonical episodes with saved order rows and the current order revision.
Selections can include specials in season zero and episodes from more than one
season. A blank query uses the first selected row's release title; a supplied
query is explicit owner search text. Searching does not reserve episodes or
enqueue downloads. Results still require a separate release choice.

Queueing rechecks the saved order and canonical revisions inside the reservation
transaction after fetching release metainfo. It persists the exact order with
the download. A later order edit does not rewrite those queued targets or labels.
The manual pack path is rejected by automatic queue entry points.

Completed downloads always wait for file-to-episode review. Filename hints use
an exact saved release title and one unambiguous typed label; unknown or
conflicting labels remain unassigned. Hints do not authorize imports. Each
selected target must be assigned exactly once or explicitly skipped. One file
may cover at most twenty consecutive canonical episodes from one season; a
single file spanning multiple canonical seasons remains unsupported. Different
files in the same pack can belong to different seasons.

Imports retain the existing containment, verification, no-overwrite and durable
per-file journal rules. Canonical organization uses each mapped file's own
season. Torrent source bytes and seeding links remain preserved. Existing files
are not replaced by this workflow.

The version 0.17 per-show season/absolute automatic Anime search remains: explicit
opt-in, one regular non-special episode, and the existing season/absolute modes.
Provider-order matching, specials and pack searches do not become automatic
through saving an identity or order. There is no new inference from external
episode counts or franchise relationships.

## API contract

All routes require an application session. Mutations also require the existing
CSRF token, same-origin checks and JSON content type. Unknown fields, null
objects and trailing JSON are rejected. Only order preview accepts a larger
512 KiB body; the other JSON mutations retain the standard 16 KiB bound.

| Method and route | Request and response |
| --- | --- |
| `GET /api/v1/metadata/anime?query=...` | One literal title query, at most 200 UTF-8 bytes. Returns `items` with at most ten AniList identities. |
| `GET /api/v1/library/items/{id}/anime-order` | Returns `media_id`, `revision`, `category_revision`, `numbering_revision`, `identities`, `entries` and loaded canonical `episodes`. An unsaved order has revision zero. |
| `POST /api/v1/library/items/{id}/anime-order/preview` | Accepts the three revisions, `provider_ids` and `entries`. Each entry has `episode_id`, `provider_id`, `release_title`, `label`. Provider facts cannot be supplied by the browser. Returns `review_id`, `settings`, `expires_at` (Unix seconds). |
| `PUT /api/v1/library/items/{id}/anime-order` | Accepts only `review_id`; returns applied settings. |
| `GET /api/v1/library/items/{id}/anime-order/sources` | Returns `items` containing TMDb episode groups for the saved show; requires configured TMDb access. |
| `POST /api/v1/library/items/{id}/anime-order/propose` | Accepts `group_id`, `subgroup_id`, `provider_id`, `release_title`, `label_prefix`, `start_number`. Returns `groups`, `entries`, `unmatched`, `source_name`; does not save. |
| `POST /api/v1/releases/anime-pack` | Accepts `series_id`, `episode_ids`, `query`, `order_revision`. Returns `items` and indexer `warnings`; requires a configured indexer. |

The existing `POST /api/v1/downloads` queues a chosen release. Download details
include its saved `anime_order` evidence. The existing
`POST /api/v1/downloads/{id}/mapping` accepts `files` with `file_index` and
`episode_ids`, plus `skip_episode_ids`, after the download reaches mapping review.
No route accepts a caller-supplied destination path or trusted provider identity.

Invalid local input returns 400, stale order/category/metadata or invalid review
returns 409, oversized bodies return 413, and incorrect MIME returns 415.
Provider failures use safe upstream error summaries. AniList cooldowns return
429 and retain saved mappings; redirects and unbounded provider responses are
not followed or accepted.

## Acceptance status and limits

Independent disposable tests cover identity transport and response validation,
local bounds before provider calls, API security, exact canonical proposal joins,
ambiguous labels, review expiry/replay/staleness, snapshot immutability, reopen
and schema 13-to-14 preservation. Library, metadata, database and HTTP package
tests passed with native local-fixture permissions during implementation.

The temporary Windows Go test executable could not open a public AniList
connection because of an OS socket restriction. The installed Windows app did
successfully search AniList through its normal authenticated API with a disposable
account/configuration. The real Go adapter also passed public identity and search
acceptance on Linux. A disposable real
qBittorrent downloaded three generated files for S00E01, S01E01 and S02E01;
saved mappings survived edits/reopen, import required review and each verified
source remained intact. Desktop/phone browser checks, all 114 interface tests,
native/package recovery and the backup-protected preview update also passed.
See [VALIDATION.md](VALIDATION.md) for the complete runs and environment limits.

Owner IPTorrents and XSpeeds account tests remain deferred. Neither private
accounts nor owner media are automated test fixtures. AniDB integration,
and broader one-file/cross-season behavior
remain future work in [REMAINING.md](REMAINING.md).

## Kitsu identities (0.26)

The editor now offers **AniList** or **Kitsu**. Kitsu uses its public JSON:API
without an owner key. Search, linking and explicit fact refresh use the selected
provider, bounded fixed-host requests and rate-limit cooldowns. Identity IDs are
scoped to their provider: Kitsu 1 is never reused as AniList 1.

Each title's saved identity set uses one provider. To switch, remove existing
links and change dependent mapping rows to Manual before selecting the other
provider, then review the complete change. A title without saved identities has
no persisted provider selection and defaults to AniList on reopening. At most
20 links can be saved, with eight newly resolved links per review. **Refresh
linked facts** creates a review; it preserves all canonical targets and labels.
Neither provider's title similarity, counts nor relationships infer an order.

Existing AniList data remains readable without migration. Queued downloads keep
their immutable provider/order snapshots. Kitsu facts do not replace TMDb catalog
identities, automatically enable Anime searches or waive file-mapping review.
