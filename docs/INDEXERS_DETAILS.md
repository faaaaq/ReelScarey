# Indexers and title details (0.9 development)

Open **Indexers** in the administration navigation to add, test, edit, disable
or remove a source. Up to twelve managed connections can coexist with the older
single Torznab connection. Existing Torznab settings continue to work and appear
in this list; their edit link opens Settings. Prowlarr is optional.

## Direct connections

| Type | Connection | Search behavior |
| --- | --- | --- |
| IPTorrents | HTTPS homepage, signed-in Cookie header and matching browser User-Agent | Direct title search, movie/TV categories, optional freeleech filter |
| XSpeeds | HTTPS homepage, signed-in Cookie header and matching browser User-Agent | Direct website search, optional freeleech filter |
| NYAA | HTTPS homepage; default `https://nyaa.si`; no credentials | Public searchable RSS |
| Torznab | API endpoint and optional API key | Independent searchable endpoint; optional Prowlarr/Jackett |
| Private torrent RSS | Website origin and complete private feed URL; optional cookie/agent | Filters entries currently in the feed; does not search its archive |

For private trackers, sign in normally in your browser. In the browser's network
tools, select a request to the tracker's torrent listing and copy the **Cookie**
header value and **User-Agent** into the app's private fields. Do not paste them
into chat. Enter header values only, without `Cookie:` or `User-Agent:` prefixes.
IPTorrents should use Icons for categories and show the files-count column.
Tracker session expiry, CAPTCHA and browser challenges require a fresh browser
sign-in; ReelScarey does not automate those challenges. Tracker layout changes
may require an adapter update. Actual private-account verification must be done
with the owner's saved session; protocol fixtures are not account acceptance.

Secrets are encrypted with the application's local key. Cookies, feed URLs and
API keys are write-only. Blank secret fields on edit preserve the saved value;
changing the website or browser agent requires re-entering the associated secret.
Save tests the new connection before replacing an existing one. A separate
enable/disable switch works while the provider is offline. Remove asks for
confirmation in the UI and preserves existing downloads and library files.

Searches contact up to four sources concurrently, within a ten-second aggregate
deadline. Results alternate between sources, with a combined limit of 200.
Failures remain visible while healthy sources continue. Direct adapters bound
response sizes, space requests by at least one second, cache searches for one
minute, and pause after rate limits. Explicit tests always contact the provider.
Redirects and foreign-origin torrent links are rejected. Sessions are sent only
to the configured origin. No third-party challenge solver is used.

Each selection retains its source and configuration generation. Disabling,
changing or removing a connection invalidates old selections. Already queued
payloads continue independently. History snapshots the source's display name;
renaming/removing the source does not rewrite past downloads. Public API results
contain opaque selections, never private torrent links, magnets or passkeys.
Scheduled searches use all enabled sources through the same search path.

NYAA supports manual search. Automatic anime matching remains conservative:
absolute episode numbering and ambiguous season mappings are not inferred.
Downloaded TV files still require explicit mapping review before import.
Website HTML does not always provide publication dates or seed counts; unknown
values remain unknown and may make automatic quality selection ineligible.

## Movie and TV detail pages

Click a library poster or title. Movie pages and existing TV pages include
synopsis, genres, rating, main cast, creators, directors, writers, IMDb/TMDb links
and a supported YouTube trailer link. TV credits use aggregate series credits.
The trailer opens on YouTube; the page does not embed an autoplay player.
Metadata is cached locally and refreshed on first visit when missing or older
than 24 hours. Manual refresh requires a valid TMDb connection. Provider outages
retain saved credits and all local import facts.

The local section shows current destination roots and verified import records:
full paths, directories, size, codec, resolution, duration, import time, original
release, grouped TV episodes and links to the individual download. History shows
source, state, progress, sizes, dates and actionable failures. Older downloads
may have no recorded indexer name. Files and history each paginate at 50 rows.

Totals count unique registered files: a file containing two TV episodes counts
once. Sizes are import snapshots, not current disk scans or physical hardlink
space usage. Changing folder preferences does not move existing files. External
file changes are not detected by this page. No existing owner media is scanned.

## Implementation and references

Migration 008 adds encrypted managed indexers, cached enrichment and a historical
indexer-name column. Earlier released migrations are unchanged. Backups include
the new encrypted records and validate their associated data before recovery.

The HTML adapter is independently implemented using BSD-licensed
[`golang.org/x/net/html` v0.60.0](https://pkg.go.dev/golang.org/x/net@v0.60.0/html).
It is now a direct locked dependency for robust HTML parsing. No Jackett source
is copied or vendored. Protocol observations were checked against the upstream
[IPTorrents definition](https://github.com/Jackett/Jackett/blob/9ce4f3973e6232199465e0230b018103db4bca03/src/Jackett.Common/Indexers/Definitions/IPTorrents.cs),
[XSpeeds definition](https://github.com/Jackett/Jackett/blob/9ce4f3973e6232199465e0230b018103db4bca03/src/Jackett.Common/Indexers/Definitions/XSpeeds.cs)
and [NYAA RSS template](https://github.com/nyaadevs/nyaa/blob/4fe0ff5b1aa7ec7c9bb2667d97e10ce2a318c676/nyaa/templates/rss.xml).
Enrichment uses TMDb's documented
[append-to-response](https://developer.themoviedb.org/docs/append-to-response),
[movie credits](https://developer.themoviedb.org/reference/movie-credits) and
[aggregate TV credits](https://developer.themoviedb.org/reference/tv-series-aggregate-credits).
