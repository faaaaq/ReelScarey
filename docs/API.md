# REST API — movie catalog

All routes are same-origin. JSON uses snake_case. All `/api/v1` routes require
an administrator session except the three public auth routes below. Mutations
also require `X-CSRF-Token` from session status and an allowed browser Origin.
Cookies are HttpOnly/SameSite=Strict; there is no Bearer app API token yet.
The API is versioned under `/api/v1`. Changes within v1 should be additive unless
explicitly documented as breaking before the first stable release.

| Method | Path | Behavior |
| --- | --- | --- |
| GET | `/healthz` | Process liveness; `{"status":"ok"}` |
| GET | `/readyz` | Database ping, 2-second deadline; 200 ready or 503 |
| GET | `/api/v1/system` | Version, schema, uptime, configured paths, explicit capabilities |
| GET | `/api/v1/dashboard` | Library name, real SQL movie/series counts, settings revision/time |
| GET | `/api/v1/settings` | Current library preferences and revision |
| PUT | `/api/v1/settings` | Validate and atomically update preferences if revision matches |
| GET | `/api/v1/library?kind=movie&page=1` | Paginated movies and saved metadata |
| GET | `/api/v1/library?kind=series&page=1` | Paginated series read model; adding shows is upcoming |
| GET | `/api/v1/auth/session` | Public setup/auth status; authenticated response includes username, csrf_token, expires_at |
| POST | `/api/v1/auth/setup` | Public, rate-limited first admin creation; username, password, setup_token; returns 201 + cookie |
| POST | `/api/v1/auth/login` | Public, rate-limited username/password login; returns 200 + cookie |
| POST | `/api/v1/auth/logout` | Revoke current session and clear cookie; requires CSRF |
| GET | `/api/v1/providers/tmdb` | configured + updated_at; never the token |
| PUT | `/api/v1/providers/tmdb` | `{ "token": "..." }`; validate remotely, encrypt and save; failure preserves previous token |
| DELETE | `/api/v1/providers/tmdb` | Remove saved credential; retain all library metadata |
| POST | `/api/v1/providers/tmdb/test` | Test the saved credential against real TMDb |
| GET | `/api/v1/metadata/movies?query=title&page=1` | Real TMDb movie search; per-result in_library flag |
| GET | `/api/v1/metadata/movies/{tmdb_id}` | Canonical movie details from TMDb |
| POST | `/api/v1/library/movies` | Add canonical metadata by TMDb ID; 201 created or 200 existing |
| GET | `/api/v1/library/movies/{id}` | Saved movie by catalog ID, without a TMDb request |
| GET/PUT/DELETE | `/api/v1/integrations/torznab` | Safe status; test/encrypt configuration; disconnect |
| GET/PUT/DELETE | `/api/v1/integrations/qbittorrent` | Safe status; test/encrypt configuration; disconnect when no active jobs |
| POST | `/api/v1/integrations/{name}/test` | Test saved Torznab/qBittorrent connection |
| GET | `/api/v1/releases?movie_id=1&query=title` | Torznab search; materialize encrypted short-lived selections |
| GET | `/api/v1/downloads?page=1` | Activity: items, page, total, limit 24 |
| POST | `/api/v1/downloads` | `{ "release_id": "..." }`; 202 queued or 200 existing; item + created |
| GET | `/api/v1/downloads/{id}` | One durable job with progress/error fields |
| POST | `/api/v1/downloads/{id}/retry` | `{}`; retry/resume blocked, failed or stopped tracking |
| POST | `/api/v1/downloads/{id}/stop` | `{}`; stop blocked/failed local tracking, leaving client/files untouched |
| POST | `/api/v1/downloads/{id}/file` | `{ "index": 3 }`; select a file from a choose_file job |

Library responses contain `items`, `page`, `total`, and `limit: 24`. Page bounds
are 1–100000; ordering is case-insensitive title then ID. Search responses contain
`items`, `page`, `total_pages` (capped at 500), and `total_results`. Queries accept
1–200 characters without control characters; search pages accept 1–500.

Add movie body: `{ "tmdb_id": 11, "monitored": true }` (`monitored` defaults
true). The server fetches metadata; caller-supplied titles, artwork or paths are
rejected as unknown fields. Response is `{ "created": true, "item": {...} }`.
The `(kind,tmdb_id)` uniqueness constraint prevents duplicate rows. Existing adds
return the original item without calling TMDb or changing monitoring preferences.
New movie adds save status `missing`; selecting a release is a separate action.
Queueing sets `downloading`; a verified import transaction sets `available`.

Torznab PUT body is `{ "url": "https://indexer.example/api", "api_key": "..." }`.
qBittorrent PUT body is `{ "url": "https://qbt.example", "username": "admin",
"password": "...", "download_path": "/downloads" }`. Omitted credential fields
retain a saved credential; changed URL/username requires explicit re-entry.
Settings responses contain configured/url and, for qBittorrent, username and
download_path. Tests and saves call real services; failed saves preserve old data.

Release responses contain items with id (opaque), movie_id, title, size_bytes,
seeders/peers (nullable) and published_at. Selection expiry is 30 minutes;
searches/settings changes invalidate unqueued older selections. No download URLs
or magnets appear in responses. Queue payloads accept a release ID only.
Activity exposes state, progress, bytes, client_state, attempts, safe error fields,
file choices and timestamps; encrypted submission payloads/credentials are omitted.
States are queued, submitting, downloading, importing, blocked, failed, completed
and cancelled. See MOVIE_WORKFLOW.md for leases, retry and import semantics.

Movie DTOs include tmdb_id, title, overview, year (nullable), release_date,
poster_path, poster_url, and runtime_minutes (nullable). Saved library items
add id, kind, status and monitored. Poster URLs are restricted to TMDb's official
image host; no upstream credential is sent to the browser.

Authentication bodies and credential PUTs use the same bounded, strict JSON
decoder as settings. Passwords require 12+ Unicode characters and at most 256
bytes. Usernames require 3–40 ASCII letters, digits, underscores or hyphens.
Sessions last 24 hours and survive restart. See AUTHENTICATION.md for recovery,
setup-code handling, deployment origin and key backup requirements.

Example settings response:

```json
{
  "library_name": "My library",
  "movie_folder": "Movies",
  "series_folder": "TV Shows",
  "revision": 1,
  "updated_at": "2026-10-22T00:00:00.000Z"
}
```

PUT sends the first four fields; `updated_at` is read-only. Requires
`Content-Type: application/json`, at most 16 KiB, exactly one JSON object, and no
unknown fields. A supplied browser Origin must match scheme and host exactly;
cross-site Fetch Metadata is rejected. No permissive CORS headers are sent.
The Vite development proxy preserves origin/host. For a reverse proxy, configure
REELSCAREY_PUBLIC_URL and preserve the original Host; forwarded headers are ignored.

Library names accept 1–80 Unicode characters without controls. Folder names
accept 1–80 ASCII letters, digits, spaces, underscores, and hyphens, start with a
letter/digit, reject reserved Windows device names, and must differ ignoring
case. Surrounding whitespace is removed. Folders are preferences only: saving
does not create, scan, move, or rename any file. Media/download roots are
process settings configured through environment variables. Imports require
writable media and readable downloads.

Errors use `{"error":{"code":"...","message":"..."}}`:
- 400 invalid input, kind, or JSON
- 401 application login required or invalid login credentials
- 403 cross-origin mutation
- 404 unknown API endpoint (never the frontend HTML)
- 405 unsupported method, with `Allow`
- 409 stale settings revision; reload before retrying
- 413 body too large
- 415 unsupported content type
- 429 authentication throttled or TMDb rate-limited; Retry-After accompanies it
- 500 unexpected server failure; details in local logs
- 502 upstream authorization, response, connectivity or availability failure;
  TMDb's own 401 does not log the administrator out
- 503 readiness or frontend unavailable

Missing TMDb configuration returns 409 `tmdb_not_configured`; stale settings use
409 `revision_conflict`. Provider errors are safe summaries, never upstream body
text, credentials, request headers or URLs containing secrets.

Planned route groups (not registered): television/library editing,
multi-indexer/client CRUD, generic `/jobs`, `/subtitles`,
and `/profiles`. Design each group's concrete request/response types with its
first complete vertical slice; do not expose placeholder 200 responses.
