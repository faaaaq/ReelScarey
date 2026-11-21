# ReelScarey 0.54.43

Collaboration tracks can now be downloaded. When the catalog artist is a
featured guest — for example "1Train" by A$AP Rocky featuring Yelawolf and
others, requested from Yelawolf's artist page — every upload is credited to
the primary artist or a re-uploader, and the channel rule used to mark every
candidate ineligible. Collaboration titles (feat./ft./featuring/with) now
treat a different contributor's channel as neutral evidence instead of a
conflict; title and duration evidence still decide, and non-collaboration
requests keep the strict channel rule.

Failed downloads no longer require a separate queue page. The top-track and
batch download popups now embed the full candidate review: the specific track
error, every returned candidate with its eligibility reasons and score, a
"Download this candidate" action for eligible rows, and a replacement-source
retry. Ambiguous or unmatched evidence is therefore reviewable and retryable
in the same dialog that started the download.

Validation: new Go regression tests with the real collaboration evidence and
a new Vitest for the candidate review action; full `go test ./...`,
`go vet ./...` and Go build; frontend format/typecheck, tests and production
build.
