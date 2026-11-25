# ReelScarey 0.54.51

- Queue a durable source upgrade for the whole music library, an artist, an
  album or one track from the Wanted page. Only a copy whose measured quality
  is strictly better and whose duration matches becomes the track's primary
  file; the original stays in place as an archive variant, and anything
  unexpected fails closed without replacing anything. The queue is schema
  migration 115.
- Deleting a video no longer removes subtitles that are named for another
  video in the same folder, such as `Movie.1985.srt` beside `Movie.1985.mkv`
  when only `Movie.mkv` is deleted. Track cleanup shares the fix.

Local validation passed frontend format and type checks, all 518 frontend
tests, the production build, the full Go suite (39 packages), Go vet and the
application build, which reports 0.54.51. `npm ci` was not run locally because
this environment cannot reach the package registry; the branch checks and both
image builds perform the clean install. The source-upgrade worker was verified
with a fixture audio source only. No upgrade has run against a live provider.

## Publication and live deployment

The [candidate checks](https://github.com/faaaaq/ReelScarey/actions/runs/45712050577)
passed on `b279d1502c672342f8f67d4103621ad543e43324`, including the clean
frontend install, Linux, Windows and browser tests and the race-enabled
concurrency suite.

The [publish run](https://github.com/faaaaq/ReelScarey/actions/runs/45717908731)
passed for AMD64 and ARM64. Docker Hub tags `0.54.51` and `latest` resolved to the
same signed manifest, verified against primary commit
`b279d1502c672342f8f67d4103621ad543e43324`.

```text
sha256:d19ddaa1d23a62cb840e9f291e2a48940718b04ee7fee21585d3c3a038e7e32c
```

Scareypit is healthy on 0.54.51. The previous configuration and Compose file
are saved at `/home/faaaaq/configs/reelscarey/config-backup-20261125172250` and
`/home/faaaaq/configs/reelscarey/compose.yml.bak-20261125172250`. The backed-up
database passed SQLite's integrity check. Migration 115 applied, the deployed
database is on schema 115 and its integrity and foreign-key checks passed.
0.54.50 refuses a database on schema 115, so a rollback restores that
configuration backup together with the Compose backup.
