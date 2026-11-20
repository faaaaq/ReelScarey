# ReelScarey 0.54.38

Ambiguous music matches now have a usable reviewed recovery path. When several
candidates have equally strong evidence, the failed-track candidate review opens
automatically and reports how many candidates are eligible. Each eligible result
has a Download this candidate action and a safe external review link; ineligible
results remain visible for diagnosis but cannot be selected.

Candidate selection uses the existing authenticated, CSRF-protected per-track
retry endpoint with `reviewed: true`. It downloads the exact reviewed candidate
instead of running the same ambiguous search again, preserves the source attempt
journal, and leaves every other track untouched. The action is available in the
album download popup, the persistent artist/album/library progress panels, and
the Music download queue. Manual replacement sources remain available.

Provider-album catalog links are still excluded from explicit-provider audio
routing, but a later reviewed replacement is no longer mistaken for that catalog
link. Existing no-guessing, title/duration evidence, source-ownership and
no-overwrite safeguards remain in place.

Coverage includes an ambiguous TIDAL-only job whose reviewed second candidate is
downloaded without another search or fallback call, an integrated popup test
that allows only eligible candidates and posts the exact candidate ID, and a
real-server browser journey that selects the second equal-score candidate and
verifies it in SQLite. The browser fixture has no downloader configured and
starts no external media download.
