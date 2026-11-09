# Security boundaries and release gates

ReelScarey is a single-administrator application for an owner-controlled server.
It is not designed for hostile multi-user hosting. Keep SQLite/config local,
private and accessible only to the app's OS account. Local administrators can
read the encryption key and database and are inside the trust boundary.

## Protected assets and untrusted input

Assets include the administrator password hash and sessions, provider credentials,
catalog, queue and import journals, media and torrent sources. Provider metadata,
Torznab XML, torrent metainfo, qBittorrent paths and subtitle payloads are untrusted.
Provider URLs configured by the administrator can reach private services; this
is intentional for self-hosted indexers/clients, not an internet-facing proxy.
Protect settings access and restrict the server's outbound network as needed.

Authentication uses a private setup code, Argon2id password hashes, bounded
login throttling and revocable hashed sessions. Cookies are HttpOnly, Strict
SameSite and Secure when HTTPS is configured. Authenticated mutations require
same-origin checks and a separate CSRF token. Host validation limits browser
rebinding. Use a TLS reverse proxy and the exact public URL for remote access;
only the loopback published port should be exposed without it. Configured HTTPS
adds HSTS for one day; the application does not terminate TLS itself. Forwarded
headers are not trusted to select origins or authenticate users.

Credentials use AES-GCM with a separate local random key. Missing or mismatched
keys fail closed. Backups intentionally contain the key alongside encrypted
records and therefore need independent encryption/access controls. Provider
credentials are write-only through APIs; signed provider URLs/raw bodies must
not appear in logs or returned error messages. Password reset revokes sessions.

External requests have time/body limits and redirects are refused. Subtitle
download URLs have a narrow HTTPS host allowlist. File publication uses rooted
filesystem operations, containment/identity checks, verified private staging,
exclusive hardlinks, journals and no overwrite. Source torrents are preserved.
Automatic subtitle choices require canonical identity plus timing evidence;
uncertain matches need review. Existing media upgrades remain suggestions.
Unknown staging files are preserved rather than removed speculatively.

Durable leases and transaction fences recheck changed monitoring, quality and
schedule policies. A config process lock prevents cooperating instances from
running workers twice. Verified restores revoke sessions, pause schedules and
hold workers until explicit authenticated review. Locks and checksums cannot
defend against a malicious local administrator replacing trusted files.

## Validation and remaining work

CI runs race/static/API tests, private real-client torrents and generated media,
restart/recovery checks, dependency scans and container smoke. govulncheck checks
reachable known Go vulnerabilities against its current database; npm audit checks
the locked frontend dependency tree. These are not an independent security audit
and do not prove the absence of vulnerabilities. Docker images are the only
supported deployment; no standalone package or checksum manifest is produced.

Before public release: independent security review, representative NAS/storage
validation, target arm64 runtime validation, sustained workload/resource budgets,
owner acceptance with its shared download mount/indexer, license selection and
signed release provenance remain outstanding. Never use an owner's existing
library, torrent jobs or subtitle quota as automated test fixtures.
