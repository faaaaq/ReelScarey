# Franchise discovery and event history

When a saved movie or show is opened, ReelScarey queries the public Wikidata
query service using its TMDb ID. It looks for explicit media-franchise statements
shared with other items carrying TMDb movie or TV IDs. Results are labeled as
Wikidata suggestions, checked against saved titles by exact provider ID, and
never create or link library items automatically. Adding a suggestion remains an
explicit action. Queries send the title's TMDb ID to Wikidata; no credentials or
local paths are sent. Results are cached for 30 minutes, limited to 200 provider
records and may be incomplete, outdated or rate-limited. A missing or ambiguous
provider identity does not establish that a franchise has no other titles.
TMDb movie collections and manually linked cross-media titles remain available.

The recent-activity tray and paginated timeline record title changes, registered
files, downloads, YouTube jobs, request and notification state, automation,
indexers and connections, organization, scans, subtitle work, user preferences,
and opt-in Emby observations. These records are part of configuration backups.
They do not contain provider credentials, webhook URLs, passwords or list IDs.

Migration 028 also adds historical facts that were already saved: title creation,
and creation or last-known states of selected prior jobs and requests. These are
labeled as historical records or states. The app cannot reconstruct intermediate
events that were never stored. The timeline uses time and event ID for stable
pagination, so restored older facts appear at their recorded times. It is not a
security audit log and does not record every read, transient progress change, or
action performed outside ReelScarey.
