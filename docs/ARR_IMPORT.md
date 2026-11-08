# Sonarr and Radarr backup import

Settings → **Sonarr / Radarr Import** adds saved titles from a Radarr or Sonarr
backup ZIP. It exists so a movie that Radarr already knows about — including one
with a future release date — can be added to ReelScarey without searching for it
by hand.

## What is read

Only the title list:

- **Radarr**: `Movies` (`TmdbId`, `Title`, `Year`, `Monitored`), with
  `MovieMetadata` used when the backup stores identity there.
- **Sonarr**: `Series` (`TvdbId`, `Title`, `Year`, `Monitored`). The TVDB ID is
  resolved to a TMDb show through TMDb's `/find` endpoint when the preview runs.

The selected titles are fetched from TMDb again for canonical metadata. The
**monitored** choice from the backup is preserved for movies. Sonarr shows are
added with future-only monitoring because Sonarr's per-season choices are not
imported.

## What is never imported

The backup ZIP can contain indexers, download clients, credentials, API keys,
root folders, file paths, tags, quality profiles, notifications and media
covers. None of those are read. ReelScarey never imports files, moves media,
adopts an existing path or silently replaces a registered file. Choose the
library import flow for files that are already on disk.

## Review and limits

1. Choose a `radarr_backup_*.zip` or `sonarr_backup_*.zip`.
2. **Review backup** reads the database in a temporary directory and shows the
   titles, monitored choice, TMDb/TVDB identity and whether each title already
   exists.
3. Titles already in the library, unmatched titles and titles without a TMDb ID
   cannot be selected.
4. **Add selected** fetches TMDb metadata and adds the chosen titles. The result
   reports added, skipped and failed titles.

The archive limit is 256 MiB and the extracted database limit is 512 MiB. The
review is bounded to 10,000 titles and an apply request to 5,000 titles. The
temporary database is deleted after the preview; the backup is not stored.

## Why future movies now appear

A Radarr movie with a future release date is saved with that date. ReelScarey
keeps it unsearched until the release date arrives, then the normal calendar and
automatic search rules apply. Adding the title does not create a download,
change monitoring for other titles or alter an existing file.
