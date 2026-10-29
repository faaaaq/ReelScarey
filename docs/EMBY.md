# Emby watch activity

Open **Emby activity**, enter the Emby URL reachable from the container and an
API key created in Emby's administration settings. A typical server URL is
`http://emby:8096/emby`; use the address and base path reachable from the
container. The key is encrypted with the
existing ReelScarey secrets key, write-only, and sent to the configured server in
the `X-Emby-Token` header. Changing the server URL requires a new key. Redirects
are rejected. Use HTTPS where the network requires it.

Choose **Refresh activity** to read Emby users and their most recently played
movies and episodes. Emby's last-played date, play count, completion and progress
are shown per user. This is an on-demand snapshot, not a complete history of
individual play events or a background tracker. Results are limited to the first
20 users and 100 items per user; the page indicates when results are truncated.
Optionally enable **Playback history** to poll Emby sessions every 30 seconds and
record observed plays in the application database. Recording starts paused and can
be paused again. Each row shows its first and last observation; a session no
longer present is marked ended. If the server is unreachable, the configuration
changes, or ReelScarey restarts, affected sessions are marked uncertain rather
than inventing an end time. Plays shorter than the polling interval can be missed;
this is not a complete server-side playback log. The most recent 200 records are
displayed. Backups include this personal viewing history.

The application does not modify Emby playback or automatically match Emby
items to ReelScarey titles. An unavailable server produces an error rather than
an empty success. Tests use an isolated API fixture; a real Emby server has not
yet been tested.
