# ReelScarey 0.54.40

Downloads into tag-derived local albums now import successfully. Local catalog
albums and tracks carry pseudo-identifiers (`local-album:`/`local-track:`),
and the reviewed import wrote them as `musicbrainz_*` file tags. The tag
reader only accepts genuine MusicBrainz UUIDs, so the identifiers disappeared
again on read-back and the write-then-verify step failed every such import
with "A written MusicBrainz identifier did not match the reviewed edit. The
original was retained." Only genuine MusicBrainz UUIDs are now written to
files, required by verification, and used to trigger a retag; local
pseudo-identifiers remain database identity only. This affects both YouTube
and TIDAL downloads in every container, including WebM/Opus.

The Download top track dialog now describes the actual source behavior:
TIDAL is the default download source whenever a TIDAL account is connected in
Music settings, with YouTube as the fallback, and the saved source preference
can restrict downloads to one provider. It no longer claims that every
download searches YouTube.

Validation: full `go test ./...`, `go vet ./...` and Go build; frontend
format/typecheck, 79 test files / 462 tests and production build. New Go
coverage includes the exact failing live identifier values and a real FFmpeg
WebM round trip proving local pseudo-identifiers are not written while
genuine MusicBrainz UUIDs still round-trip.
