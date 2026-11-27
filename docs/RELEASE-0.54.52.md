# ReelScarey 0.54.52

- Consolidate non-administrator identities into central accounts with one
  username/password and independent Requests, browser Music player and
  Subsonic grants. Settings -> Accounts manages creation, access, disabled
  status, shared password reset and Discord linking. Limited users see only
  their granted systems and can change their own password.
- Add a standalone player-only experience with an explicit catalog/artwork/
  playlist/stream API allowlist, no administrator navigation or polling,
  account-scoped browser queues and read-only global streaming preferences.
  Music management, scans, imports, exports, credentials and global preference
  writes remain administrator-only; Subsonic roles never grant ReelScarey
  administrator authority.
- Apply the central Requests grant to Discord interactions, queued bot work,
  list workers and notification delivery. Legacy request enable restores the
  central grant under the bot lock. Legacy Subsonic profile mutations are
  transactional and orphan credentials fail closed; separate identity creation,
  deletion and password reset move to Accounts while protocol-role editing
  remains on Music -> Server.
- Schema migration 116 preserves request history, Discord links, playback
  state and system-local IDs. It does not merge same-named legacy identities.
  Existing request users keep Requests, existing Subsonic users keep Subsonic,
  and migrated Subsonic-only accounts are marked Password required before an
  administrator enables their browser credential. Restores clear central
  account sessions along with existing session stores.

Local validation passed frontend format and TypeScript checks, all 530
frontend tests and the production build, the full Go test suite, Go vet and
the application build. `git diff --check` also passed. Independent backend and
frontend follow-up reviews found no remaining blockers after their findings
were fixed. A local focused Go race run could not start because this Windows
environment has CGO disabled; the required branch race checks and release
candidate containers run in CI. The clean local `npm ci` also passed. The
real-backend request/account Playwright journey passed locally after updating
it for the centralized Accounts workflow.

## Publication and live deployment

Pending mandatory release-branch checks, Docker publication and live upgrade.
