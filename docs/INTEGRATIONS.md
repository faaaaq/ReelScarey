# Integration surface: API keys, compatibility, notifications and media servers

This page documents the integration additions in schema 81–87. The application
still has one administrator and one SQLite database; request accounts remain
separate and cannot use any endpoint described here.

## Scoped API keys

Administrator GET/POST `/api/v1/api-keys` lists or creates keys; DELETE
`/api/v1/api-keys/{id}` revokes one. A created key is returned once as
`{id,name,prefix,scopes,created_at,expires_at,token}`. Only a SHA-256 digest and
a short display prefix are stored. The token shape is `rs_` followed by 43
base64url characters.

Supported scopes are `compat-sonarr`, `compat-radarr` and `compat-prowlarr`.
A key can carry one or more. Keys expire only when an expiry time is supplied;
revocation is immediate. At most 20 active keys are allowed. An empty `secret`
on a notification channel means “keep the saved secret”; API keys have no edit
operation, only create and revoke.

## Sonarr / Radarr / Prowlarr compatibility

The application accepts these prefixes:

| Prefix | Scope |
| --- | --- |
| `/sonarr/api/v3` and `/sonarr` | `compat-sonarr` |
| `/radarr/api/v3` and `/radarr` | `compat-radarr` |
| `/prowlarr/api/v1` and `/prowlarr` | `compat-prowlarr` |
| `/api/v3` | scoped by the requested resource |

Authentication is `X-Api-Key: <token>`, `?apikey=<token>` or an
`Authorization: Bearer <token>` header. A local administrator session may inspect
the endpoints for diagnosis, but mutations still require CSRF. Request accounts
are always rejected.

Implemented reads: `/system/status`, `/qualityprofile`, `/rootfolder`, `/tag`,
`/languageprofile`, `/downloadclient`, `/movie`, `/movie/lookup`, `/series`,
`/series/lookup`, `/episode`, `/calendar`, `/queue`, `/history`, `/indexer`,
`/indexer/schema` and `/health`. `POST /command` accepts only:

- `MovieSearch` — queue a focused movie search.
- `RefreshMovie` — enqueue the existing metadata refresher.
- `EpisodeSearch` — queue a focused episode search.
- `RefreshSeries` — enqueue the existing metadata refresher.

Everything else, including arbitrary free-text Prowlarr search, single-item
reads, rescans and write endpoints, returns an explicit `404` or `501`. The
layer never fabricates download history, files or quality decisions. It is a
compatibility subset, not a claim of full Sonarr/Radarr/Prowlarr parity.

## Notification channels

Administrator `/api/v1/notifications` lists or creates channels. PUT
`/api/v1/notifications/{id}` saves with the current revision, DELETE removes it,
POST `/api/v1/notifications/{id}/test` sends a synchronous test, and
GET `/api/v1/notifications/outbox` plus POST
`/api/v1/notifications/outbox/{id}/retry` inspect and explicitly retry durable
deliveries.

Providers are `email` (SMTP), `ntfy`, `gotify`, `pushover`, `telegram`,
`discord` and `webhook`. Each channel selects one or more events:
`download.completed`, `download.failed`, `import.completed`, `request.approved`,
`request.declined`, `health.failed`, `subtitle.failed`, `backup.failed`,
`organization.failed` and `youtube.completed`.

The worker maps new `activity_events` rows to those events in the same
transaction that advances its cursor. Deliveries are `pending`, `sending`,
`sent`, `failed`, `uncertain` or `cancelled`. A network failure after the
request may have reached the provider is marked `uncertain` and is **never**
retried automatically; the owner may retry explicitly, which can duplicate the
message. HTTP `429` and `5xx` responses retry with bounded backoff; `4xx` is
terminal. SMTP results that may have reached the server are also uncertain.

Provider secrets are encrypted with the instance key and are never returned by
list or test responses. URLs must be HTTP/HTTPS without embedded credentials or
fragments; Discord webhooks are restricted to Discord hosts. Response bodies are
drained with a small bound and never logged.

## Cardigann definitions

Indexers may use `kind: "cardigann"` with a bounded YAML definition and the
tracker homepage URL. The implementation is a conservative subset of Cardigann:
CSS/HTML search rows, common text filters (`trim`, `tolower`, `toupper`,
`replace`, `regexp`, `split`, `dateparse`, URL/base64/HTML decoding and
prefix/suffix helpers), cookie and user-agent headers, and torrent or magnet
download links. The search path must stay on the configured host; redirects are
not followed and download URLs are never fetched from arbitrary hosts.

Definitions are untrusted configuration. Existing definitions are not returned by
the indexer list; re-paste a definition when editing. Form-login definitions
require the session cookie to be entered explicitly. Cardigann indexers are
torrent-only in this implementation; NZB definitions are not represented as
working.

## Jellyfin and Plex

Administrator GET/PUT/DELETE `/api/v1/jellyfin` and `/api/v1/plex` read and save
encrypted connections. GET `/activity` returns a bounded recent-play snapshot;
POST `/refresh` calls the server's own refresh endpoint. Plex additionally
supports GET `/sections` to list section IDs. An omitted API key/token on PUT
retains the saved secret only for the same URL.

These adapters read server state and request a refresh. They never change
playback state, mark items watched, or write media files. Plex `recentlyAdded`
is a recently added/played listing, not a complete event history. Jellyfin
returns the server's own `UserData`, which can be absent for shared or restricted
accounts.

## Boundaries

- API keys reach only the compatibility prefixes; they do not grant the
  administrator REST API.
- Request accounts are denied compatibility, notification, media-server and API
  key routes.
- Notifications and media-server credentials use the existing secure store.
- Backups include the key and therefore the encrypted API-key table, channel
  secrets and connection tokens. Restore still pauses schedules and holds
  workers for explicit review.
