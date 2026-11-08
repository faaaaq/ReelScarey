# Request accounts, lists and Discord notifications

An administrator creates request accounts on **Requests**. Request users sign
in with their own username and initial password and see a separate request-only
interface. They cannot access administrator APIs. The administrator can disable
an account, which immediately revokes its sessions. Share initial passwords
privately. Request users can change their own password; doing so signs out all
of their request sessions. The administrator can approve or decline requests; approval adds the
canonical title to the catalog, with monitoring enabled. Downloads still depend
on the configured automation and download services.

Request users can search TMDb for a movie or show and submit it. Duplicate
requests from the same account are retained as one request. Request status and
registered media availability are shown separately; a TV show can have some
media without all monitored episodes being available.

Each user can configure up to ten list imports. Enter a numeric TMDb v4 public
list ID, a public Trakt owner/list slug and Trakt application client ID, or an
IMDb external-list ID and API key from their MDBList account. For IMDb, first
add the IMDb list to MDBList; MDBList refreshes that copy on its own account
schedule, then ReelScarey checks the copy daily. This explicitly uses a third
party and requires an MDBList account. **Check now** schedules another check;
imports create pending requests. Private TMDb/Trakt lists use a provider-issued
user access token. Trakt watchlists are supported for public profiles
(application client ID only) and private profiles (user access token); the
newest 200 titles are imported. OAuth enrollment and token refresh are not
automated. The app uses the administrator's TMDb connection; Trakt client IDs
and MDBList API keys are stored encrypted and never returned. Imports are bounded to 200 movies/shows and ten
pages per list; an item missing its TMDb ID stops that list without partial
import. Removing a list does not remove previously created requests.

IMDb provides a list export. A user can upload the CSV export as a one-time
import of up to 25 unique title IDs (12 KB in the interface); all IDs must
resolve unambiguously through TMDb before any requests are added. This is not a
continuous IMDb list subscription. In this environment a public IMDb export
endpoint returned HTTP 202 without list data; no access restrictions were bypassed.

Private TMDb and Trakt lists can use a provider-issued user access token on the
same fixed provider hosts and with the same account ownership and revision checks.
Choose the private provider in Import lists. TMDb requires its list owner's user
access token, rather than the administrator's application token. Trakt requires
both the application client ID and a user token with access to the list. Tokens
are encrypted with the existing instance key and are never returned in responses
or history. Imported requests still need administrator approval. Expired/revoked
tokens stop synchronization with a visible error; remove and add the list with a
new token. OAuth enrollment/refresh is not automated in this milestone.

Provider contracts: [TMDb private-list access](https://developer.themoviedb.org/v4/reference/list-details)
and [Trakt user authentication](https://developer.trakt.tv/docs/authentication-oauth).
Direct IMDb synchronization is not represented as working: IMDb staff describe
the [multi-step export and lack of a single export URL](https://community-imdb.sprinklr.com/conversations/imdbcom/imdb-redesigns-listlike-pages/662a8541586efb2637c22757?commentId=66a0f5374a39ff190e711922).

Users may enter their own Discord webhook URL. The URL is encrypted, write-only,
and restricted to the canonical Discord webhook host. When an approved movie
has a registered file, or all monitored regular seasons are loaded and their
monitored episodes have registered files, the app sends a notification.
It does not issue mentions. Delivery runs in the container and survives restart.
If Discord may have received a message but the result is unknown, the status is
marked uncertain; the user may explicitly retry, which can send a duplicate.
Deleting a webhook stops future sends.

Administrators can configure the Discord companion bot on **Requests**. Create
a Discord application and bot, add it to your server with permission to use
commands and post in the administrator channel, and enter its application ID,
public key, bot token, server ID, administrator user ID and channel ID. Configure
the application's interactions endpoint to the HTTPS URL shown in ReelScarey;
this requires a publicly reachable HTTPS reverse proxy. Register the commands
from the same page, and link each request account to its Discord user ID. The
bot token is encrypted and is not displayed again.

Linked users can use `/request` to search and submit requests and `/releases`
with an approved request ID to search releases. For TV, provide a season and,
optionally, an episode; load the season's metadata in ReelScarey first. New
requests also notify the administrator, who can approve and search from the
message. Release buttons are limited to the person who received them and expire.
TV imports still require the normal file mapping review. If a notice's delivery
is uncertain, an administrator can retry it from Requests; this can duplicate a
message. Actual Discord and MDBList accounts have not yet been used for live
acceptance, so verify your own setup with a small test first.

These features require the schema migration included in the image. Back up the
database and secrets key before upgrading. Tests use disposable accounts, media,
fake provider responses and a fake Discord transport, not owner services.
