# ReelScarey 0.54.42

TIDAL downloads now complete end-to-end. After the search-signature fix in
0.54.41, the actual download still failed because tidaler 0.1.8 dereferences
`event_abort` and waits on `event_run`: with no events supplied the worker
crashed with "'NoneType' object has no attribute 'is_set'", and an unset run
event would have blocked it forever. The provider now passes an abort Event
and an already-set run Event.

Verified with a real TIDAL download in a disposable container against the
live service: a 17.6 MB FLAC (44.1 kHz, 16-bit, exactly 158.000 seconds) of
the studio track behind the reported "why did this not download from TIDAL"
job, with the scratch files removed afterwards. A new offline regression test
(`scripts/test_tidal_download.py`) reproduces the event contract so the
failure cannot return silently, and the search and device-login tests still
pass on Windows and Linux.

Validation: `python scripts/test_tidal_download.py`,
`python scripts/test_tidal_search.py` and `python scripts/test_tidal_provider.py`
on Windows and Linux; full `go test ./...`, `go vet ./...` and Go build;
frontend format/typecheck, tests and production build.
