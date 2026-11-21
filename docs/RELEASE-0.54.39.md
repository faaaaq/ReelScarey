# ReelScarey 0.54.39

Artist top tracks and the full discography now resolve for compilation-only
catalogs. Deezer's per-artist album list omits compilation releases entirely,
so an artist whose saved albums are "Best of" collections (for example Cher)
could never be corroborated and the page refused to show any provider tracks.
Resolution now uses a bounded evidence chain: the artist's own album list,
then exact-title album-search attribution, and finally saved track-title
attribution through Deezer track search. A same-named candidate corroborated
by any tier owns the page; a differently named alias owner must still carry
an exact catalog title in its own album list, so a tribute release cannot win
by a search hit alone.

Top-track and album downloads accept release-variant titles. Provider titles
routinely carry "(Remastered)" or "(Deluxe)" while YouTube uploads of the same
recording do not, so requiring the full requested version set rejected every
candidate and failed the acquisition. Remaster and deluxe qualifiers no longer
disqualify a candidate in either direction; live, remix, cover, instrumental,
acoustic, demo, sped-up, radio-edit and extended qualifiers still must agree,
and an explicit remaster tag still ranks higher. The Download top track dialog
no longer prints the single track state beside an identical job state.

TIDAL remains the primary acquisition source when installed, with YouTube as
the fallback. The detached device-login flow introduced in 0.54.36 is
re-verified in this release on Windows and Linux through the offline provider
lifecycle test, and the Music documentation now covers the Settings Connect
TIDAL flow, its stale-worker recovery and the candidate metadata TIDAL
contributes to matching.

Validation: full `go test ./...`, `go vet ./...` and Go build; frontend
format/typecheck, 79 test files / 462 tests and production build;
`python scripts/test_tidal_provider.py` (offline device-login lifecycle) on
Windows and Linux. New Go regression tests cover compilation corroboration
and release-variant candidate selection with the real provider evidence.
