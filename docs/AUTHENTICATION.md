# Administrator and credential operations

ReelScarey currently has one administrator. Setup, login, logout, session expiry
and local password recovery are implemented. Email reset, multi-user roles and
external identity providers are outside this milestone.

## First run or upgrade from the foundation

The migration preserves existing library preferences. On startup without an
admin, ReelScarey generates a random one-time code in `<config>/setup-token`.
It is never written to normal application logs. Open the app, enter that code,
choose a username and a unique password of at least 12 characters. Creation is
atomic: concurrent attempts cannot create a second admin. The code file is then
removed, and the database prevents setup from reopening even if a copy remains.

Retrieve the code on the server (this command intentionally prints the code):

```sh
docker compose exec reelscarey reelscarey setup-token
```

Native installation: read the setup-token file or run `bin/reelscarey setup-token`
with the same config environment as the running service. On Windows use
`.\bin\reelscarey.exe setup-token`. Keep codes and provider tokens out of chats,
issue reports, screenshots and source control.

### If a setup code is rejected

The code belongs to one config directory. Run the setup-token command with the
same REELSCAREY_CONFIG_DIR as the server, or read that directory's setup-token
file directly. A code from the default data/config folder will not match a
server using another config directory. For the development preview created in
this project, the directory is data/preview/config. Copy the file's single line,
not its filename or surrounding quotes. If repeated attempts are throttled,
wait one minute before trying again.

## Sessions

Login creates a random cookie with HttpOnly, SameSite=Strict, Path=/ and a fixed
24-hour lifetime. SQLite stores its hash, not the cookie value. Sessions survive
application restarts; logout and password recovery revoke them server-side.
The API client uses a separate CSRF token for mutations. No session credential
is stored in localStorage. At most ten active sessions are retained.

Login/setup limits are ten attempts per remote IP per minute and sixty globally;
Argon2id work is limited to two concurrent operations. Limits reset on restart.
The service ignores X-Forwarded-For rather than trusting attacker-supplied IPs;
proxy deployments may consequently share an IP limit. This is a small single-
admin service, not a substitute for the planned production security review.

## Lost password

An owner with access to the config volume can run the maintenance command
`reelscarey reset-password`. It reads the new password from standard input and
revokes all sessions atomically. It does not accept a password argument or log it.
Create a temporary, access-restricted password file on the server, then:

```sh
docker compose exec -T reelscarey reelscarey reset-password < /secure/new-password.txt
```

For a native install, pipe that file into `bin/reelscarey reset-password` with the
same REELSCAREY_CONFIG_DIR. Remove the temporary password file using your normal
secure file-handling procedure, then sign in using the existing username.

## TMDb credentials and backups

The same secrets.key also protects Torznab/qBittorrent configuration documents
and secret-bearing release/torrent payloads. Missing, wrong or tampered keys
fail closed at startup, even if TMDb is disconnected. See MOVIE_WORKFLOW.md for
write-only integration credentials, replacement tests and active-job restrictions.

Sign in, open Settings, and enter the **API Read Access Token** from your
[TMDb API account page](https://www.themoviedb.org/settings/api). The integration
uses [Bearer application authentication](https://developer.themoviedb.org/docs/authentication-application),
[movie search](https://developer.themoviedb.org/reference/search-movie),
[movie details](https://developer.themoviedb.org/reference/movie-details), and
[credential validation](https://developer.themoviedb.org/reference/authentication-validate-key).
The API validates tokens before replacing a saved value. Search is in en-US
with adult-search inclusion disabled. Future language profiles remain separate.

Saved tokens use AES-256-GCM in SQLite. The generated `secrets.key` file must be
backed up with the database; losing it makes saved tokens unreadable. Startup
fails if a key is missing while ciphertext exists, or if decryption fails.
Encryption protects a database-only leak; possession of the entire config
folder also gives access to the key. Restrict the folder and backups using host
permissions. New secret files use mode 0600 on Unix; Windows inherits ACLs.

The provider status API exposes only configured/time fields. Provider failures
are summarized without returning upstream bodies or headers. The original TMDb
token is not included in HTML, JSON responses, localStorage, normal logs, or
artwork URLs. Disconnecting TMDb removes its token and retains your library.

## Host and TLS configuration

Default native and Compose ports are loopback-only. Only loopback Host names
are accepted unless REELSCAREY_PUBLIC_URL specifies another browser origin.
For HTTPS through a reverse proxy, preserve the original Host and set that
variable to the exact `https://...` origin. Cookies then require Secure.
Forwarded-Proto/Host headers are ignored. Deploying remotely also requires TLS
and an explicit network/proxy configuration; the environment variable alone
does not change Compose's loopback port publication.

## Sources and attribution

Password hashing follows the Argon2id baseline in the
[OWASP password storage guidance](https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html).
Session handling follows [OWASP session guidance](https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html).
TMDb's [attribution requirements](https://developer.themoviedb.org/docs/faq)
are represented in Settings with their unmodified official blue-short logo and
required notice. Logo source:
https://www.themoviedb.org/assets/2/v4/logos/v2/blue_short-8e7b30f73a4020692ccca9c88bafe5dcb6f8a62a4c6bc55cd9ba82bb2cd95f6c.svg
