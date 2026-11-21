# ReelScarey 0.54.41

TIDAL is now actually used as the primary download source. The bundled
provider called `session.search("track", query)`, which tidalapi 0.8.11
rejects with "Tried to search for an invalid type". Every TIDAL catalog
search therefore failed, and every acquisition silently fell back to YouTube
even with a verified TIDAL session. The provider now calls
`session.search(query, models=[Track])`, so a connected TIDAL account
supplies the exact studio recording with its real title, artist, album and
duration before YouTube is considered.

Verified against the live TIDAL service: the query behind the reported
"why did this not download from TIDAL" job returns the exact studio track
(`tidal.com/browse/track/13981865`, Cher, 158 seconds) instead of a YouTube
video. A new offline regression test (`scripts/test_tidal_search.py`) makes
the old call shape fail loudly, and the device-login lifecycle test still
passes on Windows and Linux.

Validation: `python scripts/test_tidal_search.py` and
`python scripts/test_tidal_provider.py` on Windows and Linux; full
`go test ./...`, `go vet ./...` and Go build; frontend format/typecheck,
tests and production build.
