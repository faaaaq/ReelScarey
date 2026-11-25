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
