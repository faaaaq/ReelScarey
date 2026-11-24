# ReelScarey 0.54.49

- Calendar digital dates represent the original digital release. Later
  subscription availability such as the Starz listing for Jurassic World,
  explicit re-releases and dates outside the original release window are
  excluded.
- A successful movie metadata refresh with a complete release list can clear a
  stale digital date. Incomplete responses retain cached dates when no
  replacement is available.

Jurassic World regression coverage uses the TMDb Starz Digital and Starz Encore
TV entries alongside the original theatrical and physical dates. The metadata
refresh regression also verifies that the stale Calendar entry disappears.

Local verification passed for frontend format, types, all 509 tests and the
production build, plus the full Go suite, vet and the application build.

## Publication and live deployment

The [publish run](https://github.com/faaaaq/ReelScarey/actions/runs/45458314299)
passed for AMD64 and ARM64. Docker Hub tags `0.54.49` and `latest` resolved to the
same signed manifest, verified against primary commit
`25dc8d475ea5242b8886c80ac6e26ad02f613d98`.

```text
sha256:22e1e0c0cf9ccfa765766078db00d33457f79fe04cd8fc5d940b3045224deabf
```

Scareypit is healthy on 0.54.49. The previous configuration and Compose file
are saved at `/home/faaaaq/configs/reelscarey/config-backup-20261124183446` and
`/home/faaaaq/configs/reelscarey/compose.yml.bak-20261124183446`. The backed-up
database passed SQLite's integrity check. The deployed database remains on
schema 114.

The live Jurassic World refresh removed the November 1, 2026 digital date and
retained its 2015 primary, theatrical and physical dates. The other 1,280
movies with cached digital dates were queued for normal metadata refresh.
