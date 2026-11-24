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
