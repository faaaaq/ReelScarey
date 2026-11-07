# Reviewed bulk monitoring (0.14 development)

Select movies or TV shows in either poster or table view. **Select this page**
selects only the currently displayed titles (at most 24). Selection stays when
switching views and clears when the page, filters or sorting change. It does not
include off-page matches and is not saved across reloads or navigation.

Choose Pause monitoring or Resume monitoring, then Review monitoring. The review
reads the exact selected IDs and shows their current title, year, monitoring and
the proposed state. It counts actual changes separately from unchanged titles.
Cancel makes no changes. Apply saves the entire reviewed batch in one transaction;
a missing/changed title rejects the complete batch. No partial success is hidden.
An entirely unchanged selection finishes without sending a mutation.

Only parent movie/show monitoring changes. TV season, episode and specials choices
remain intact. Profiles retain their assignments, including the schedule default.
Resuming monitoring makes titles eligible for independently enabled automatic
searches; review your Automation settings when needed. This operation does not
start a search/download, cancel existing downloads, touch files or modify clients,
indexers or schedules. Current download/import/subtitle workers keep their normal
policy checks and behavior.

Each preview includes the existing series and item-automation revisions. Apply
checks the complete snapshot and monitoring values before any write. Changed shows
bump their normal series revision, invalidating older season/show editors. Changed
titles bump the item-automation revision without changing profiles, invalidating
stale assignment editors and pending automatic queue decisions. For a previously
unassigned title, a NULL profile retains the schedule default. Unchanged rows do
not create assignments or bump revisions. Edits away and back still invalidate
old reviews. No new schema or migration is required; schema remains 10.

If a response is lost or preferences change, the interface requires another
review before another apply attempt. A fresh review can show that a prior request
already succeeded. Completed configuration changes survive restart; there is no
background bulk job or exactly-once submission claim.

## API

Both endpoints require the administrator session, CSRF and same-origin validation.
Monitoring bodies use a dedicated 1 MiB limit so a whole library can be reviewed; strict unknown-field decoding still applies.

- POST `/api/v1/library/monitoring/preview`: `{kind, ids}` reads 1–5000 distinct
  positive catalog IDs for kind movie or series. Returns `{kind, items}` with
  id/title/year/monitored/series_revision/automation_revision. It does not write.
- PUT `/api/v1/library/monitoring`: `{kind, monitored, items}` where items contain
  id, explicit monitored (the reviewed current value), series_revision and
  automation_revision. The top-level monitored is the desired state. Returns
  total/changed/unchanged/monitored. Missing or stale targets return 409 with no
  changes; invalid selections return 400. A missing preview target returns 404.

The UI always reviews before applying; API clients must supply current expected
values. This is optimistic configuration editing, not a preview-token permission
system. Reads/writes use five-second request budgets. Tests use disposable
catalogs and services, never owner media or private sessions.

## Appearance

The compact layout, typography, aligned rules, poster/table views and plain copy
are retained. Graphite backgrounds and blue accents replace the olive palette,
including navigation, forms, overview, planning screens, sign-in and favicon.
Green remains only where it conveys a successful/available state. No external
fonts, application dependencies, slogans or promotional art are introduced.

IPTorrents and XSpeeds account testing remains deferred. Bulk episode editing,
profile assignment, deletion, moving files and replacement imports are outside
this milestone.
