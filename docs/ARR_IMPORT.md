# Sonarr and Radarr backup and calendar import

Settings → **Sonarr / Radarr Import** adds saved titles from a Radarr or Sonarr
backup ZIP or an iCalendar (`.ics`) export. It exists so a movie that Radarr
already knows about — including one with a future release date — can be added to
ReelScarey without searching for it by hand.

## What is read

- **Radarr backup**: `Movies` (`TmdbId`, `Title`, `Year`, `Monitored`), with
  `MovieMetadata` used when the backup stores identity there.
- **Sonarr backup**: `Series` (`TvdbId`, `Title`, `Year`, `Monitored`). The TVDB
  ID is resolved to a TMDb show through TMDb's `/find` endpoint.
- **Radarr calendar**: event `SUMMARY` (`Title (Theatrical Release)` and the
  digital/physical variants), `DTSTART` and `UID` only. Release-kind suffixes are
  stripped, duplicate release events for one movie collapse to the latest date,
  and descriptions/categories/URLs are ignored.
- **Sonarr calendar**: event `SUMMARY` (`Show - S01E02 - Episode`) and
  `DTSTART`. Only the show title and next date are kept.

The selected titles are fetched from TMDb again for canonical metadata. A
calendar event has no TMDb ID, so the preview resolves it through an exact TMDb
title search (and the event year for movies). Multiple or missing matches stay
unselected for manual addition. The **monitored** choice from a backup is
preserved for movies. Sonarr shows are added with future-only monitoring because
Sonarr's per-season choices are not imported.

## What is never imported

The backup ZIP can contain indexers, download clients, credentials, API keys,
root folders, file paths, tags, quality profiles, notifications and media
covers. None of those are read. ReelScarey never imports files, moves media,
adopts an existing path or silently replaces a registered file. Choose the
library import flow for files that are already on disk.

## Review and limits

1. Choose a `radarr_backup_*.zip`, `sonarr_backup_*.zip` or exported `.ics`.
2. **Review backup** reads the database or calendar in a temporary directory and
   shows the titles, date, monitored choice, TMDb/TVDB identity and whether each
   title already exists. Calendar titles dated before today start unselected.
3. Titles already in the library, unmatched titles and titles without a TMDb ID
   cannot be selected.
4. **Add selected** fetches TMDb metadata and adds the chosen titles. The result
   reports added, skipped and failed titles.

The archive limit is 256 MiB, the extracted database limit is 512 MiB and the
calendar limit is 5 MiB. Review is bounded to 10,000 titles/events and an apply
request to 5,000 titles. The temporary file is deleted after the preview; the
backup is not stored.

## Why future movies now appear

A Radarr movie with a future release date is saved with that date. ReelScarey
keeps it unsearched until the release date arrives, then the normal calendar and
automatic search rules apply. Adding the title does not create a download,
change monitoring for other titles or alter an existing file.

A calendar export only contains events in its date range; it cannot include a
movie with no date at all. Use the Radarr backup ZIP when the complete saved
movie list is needed.

