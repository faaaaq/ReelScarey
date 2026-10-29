# Repeatable container workload budget

Run `bash scripts/container-workload.sh reelscarey:local 300` to exercise a fresh
disposable configuration for five minutes. The script never attaches the running
installation or owner media. It seeds a synthetic 1,000-movie catalog, creates a
disposable administrator using normal setup and runs eight concurrent readers
against catalog, planning, calendar, storage, event and profile APIs. A separate
loop updates library settings every five seconds with current revisions.

The app container has one CPU, 384 MiB memory and a 128-process limit. It uses a
read-only root, dropped capabilities and no external network. The Node workload
driver is a separate temporary test helper; deployment still uses one container.
The scenario requires:

- No failed HTTP operations and sustained forward progress.
- A read latency p95 below one second.
- Sampled working memory below 192 MiB and processes below the 128-process cap.
- Preserved catalog/settings and SQLite integrity after controlled restart.

The script prints request counts, settings writes, p95/p99 latency, memory and
process samples. Its duration is bounded to 60–1800 seconds. CI runs the shorter
60-second regression; local acceptance uses the full five-minute scenario.

These are budgets for this defined read/write scenario on the test host. They
do not size NAS/shared-storage installations, speech processing, FFmpeg rewrites,
large concurrent downloads or long-term production capacity. New schedules stay
paused and no public providers or private accounts are exercised.
