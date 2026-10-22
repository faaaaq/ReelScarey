# REST API — foundation

All routes are same-origin. JSON uses snake_case. No authentication is present
yet; bind to loopback only. No API accepts provider credentials in this milestone.
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
| GET | `/api/v1/library?kind=movie` | Movies in the database; initially `items: []` |
| GET | `/api/v1/library?kind=series` | Series in the database; initially `items: []` |

Library results are sorted by title/id and limited to 100; response includes
`limit: 100`. No add/delete API or metadata enrichment exists yet. Pagination
must precede general library ingestion.

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
The Vite development proxy preserves origin/host; reverse-proxy support needs
explicit trusted-proxy configuration before any remote deployment.

Library names accept 1–80 Unicode characters without controls. Folder names
accept 1–80 ASCII letters, digits, spaces, underscores, and hyphens, start with a
letter/digit, reject reserved Windows device names, and must differ ignoring
case. Surrounding whitespace is removed. Folders are preferences only: saving
does not create, scan, move, or rename any file. Media/download roots are read-only
process settings configured through environment variables.

Errors use `{"error":{"code":"...","message":"..."}}`:
- 400 invalid input, kind, or JSON
- 403 cross-origin mutation
- 404 unknown API endpoint (never the frontend HTML)
- 405 unsupported method, with `Allow`
- 409 stale settings revision; reload before retrying
- 413 body too large
- 415 unsupported content type
- 500 unexpected server failure; details in local logs
- 503 readiness or frontend unavailable

Planned route groups (not registered): `/metadata`, library mutations,
`/indexers`, `/releases`, `/download-clients`, `/downloads`, `/jobs`, `/subtitles`,
and `/profiles`. Design each group's concrete request/response types with its
first complete vertical slice; do not expose placeholder 200 responses.
