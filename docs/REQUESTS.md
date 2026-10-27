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

Each user can configure up to ten public list imports. Enter a numeric TMDb v4
list ID, or a Trakt owner, list slug and Trakt application client ID. The app
checks lists daily and creates pending requests; **Check now** schedules another
check. Private lists and personal watchlists requiring OAuth are not supported.
The app uses the administrator's TMDb connection; the Trakt client ID is stored
encrypted and is never returned. Imports are bounded to 200 movies/shows and ten
pages per list; an item missing its TMDb ID stops that list without partial
import. Removing a list does not remove previously created requests.

IMDb provides a list export. A user can upload the CSV export as a one-time
import of up to 25 unique title IDs (12 KB in the interface); all IDs must
resolve unambiguously through TMDb before any requests are added. This is not a
continuous IMDb list subscription. In this environment a public IMDb export
endpoint returned HTTP 202 without list data; no access restrictions were bypassed.

Users may enter their own Discord webhook URL. The URL is encrypted, write-only,
and restricted to the canonical Discord webhook host. When an approved movie
has a registered file, or all monitored regular seasons are loaded and their
monitored episodes have registered files, the app sends a notification.
It does not issue mentions. Delivery runs in the container and survives restart.
If Discord may have received a message but the result is unknown, the status is
marked uncertain; the user may explicitly retry, which can send a duplicate.
Deleting a webhook stops future sends. No Discord companion bot or interactive
release-selection messages are implemented yet.

These features require the schema migration included in the image. Back up the
database and secrets key before upgrading. Tests use disposable accounts, media,
fake provider responses and a fake Discord transport, not owner services.
