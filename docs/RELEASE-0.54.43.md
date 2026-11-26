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

The commit added Go collaboration regression coverage and a Vitest for the
candidate review action. Application checks and both Docker builds stopped
at Prettier because that Vitest file needed formatting. The Git tag exists
(removed on 2026-11-25, when every tag before 0.54.51 was deleted), but the
manifest job was skipped and this patch did not reach Docker Hub.
The previous release, 0.54.42, retained `latest`. See the
[failed publish run](https://github.com/faaaaq/ReelScarey/actions/runs/44894228106)
and the [recovery release](RELEASE-0.54.44.md).
