#!/usr/bin/env bash
# Exercises the shipped compose.yaml template with disposable mounts only.
set -euo pipefail
image=${1:-reelscarey:ci}
project="reelscarey-compose-check-$$"
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp -d /tmp/reelscarey-compose-check.XXXXXX)
compose=(docker compose --project-name "$project" --env-file "$fixture/live.env" -f "$root/compose.yaml")
cleanup() {
 "${compose[@]}" logs --tail 8 || true
 "${compose[@]}" down --volumes --remove-orphans >/dev/null || true
 # App-created files are private to the selected UID. Remove only this fixture.
 docker run --rm --network none --user 0 -v "$fixture:/fixture" --entrypoint python3 "$image" -c 'import pathlib,shutil; root=pathlib.Path("/fixture"); [shutil.rmtree(p) for p in root.iterdir() if p.is_dir() and not p.is_symlink()]' || true
 case "$fixture" in /tmp/reelscarey-compose-check.*) rm -rf -- "$fixture" ;; esac
}
trap cleanup EXIT
mkdir "$fixture/config" "$fixture/media" "$fixture/downloads"
printf 'Preserved owner bytes\n' > "$fixture/media/owner.txt"
# Configure permissions inside Docker so this also runs as an ordinary host user.
docker run --rm --network none --user 0 -v "$fixture:/fixture" --entrypoint sh "$image" -c 'chown 1000:1000 /fixture/media /fixture/downloads; chmod 750 /fixture/media /fixture/downloads; chown 2345:2345 /fixture/media/owner.txt'
cat > "$fixture/live.env" <<EOF
REELSCAREY_IMAGE=$image
REELSCAREY_CONTAINER_NAME=$project
REELSCAREY_BIND_IP=127.0.0.1
REELSCAREY_PORT=18787
REELSCAREY_PUBLIC_URL=http://127.0.0.1:18787
PUID=1000
PGID=1000
TZ=America/Chicago
CONFIG_PATH=$fixture/config
MEDIA_PATH=$fixture/media
DOWNLOADS_PATH=$fixture/downloads
EOF
"${compose[@]}" config --quiet
"${compose[@]}" up -d --pull never
ready() {
 for attempt in {1..90}; do
  if "${compose[@]}" exec -T reelscarey reelscarey healthcheck >/dev/null 2>&1; then return; fi
  sleep 1
 done
 echo 'compose.yaml installation failed to start.' >&2
 return 1
}
ready
curl --fail --silent http://127.0.0.1:18787/readyz >/dev/null
test "$(curl --silent --output /dev/null --write-out '%{http_code}' http://127.0.0.1:18787/api/v1/system)" = 401
# The app itself runs as the requested identity: it created the database as PUID.
"${compose[@]}" exec -T reelscarey python3 -c 'import pathlib; owner=pathlib.Path("/config/reelscarey.db").stat().st_uid; assert owner==1000, owner'
# The requested identity can write the library, the download folder, /config and
# the two private volumes, and the private folders stay separate mounts.
"${compose[@]}" exec -T -u 1000:1000 reelscarey sh -c 'test -w /media && test -w /downloads && test -w /config && test -w /backups && test -w /youtube-staging'
# Owner bytes in the library survive the private-folder initialization untouched.
"${compose[@]}" exec -T reelscarey sh -c 'test "$(cat /media/owner.txt)" = "Preserved owner bytes"'
test "$("${compose[@]}" exec -T reelscarey date -d 2026-01-15T12:00:00Z +%Z)" = CST
key_before=$("${compose[@]}" exec -T reelscarey sha256sum /config/secrets.key)
"${compose[@]}" exec -T reelscarey reelscarey doctor
"${compose[@]}" restart reelscarey
ready
test "$key_before" = "$("${compose[@]}" exec -T reelscarey sha256sum /config/secrets.key)"
"${compose[@]}" exec -T reelscarey reelscarey doctor
# The same authenticated backup/upload/restore acceptance used by releases.
"${compose[@]}" exec -T -u 1000:1000 reelscarey python3 -c 'import sqlite3; db=sqlite3.connect("/config/reelscarey.db"); db.execute("INSERT INTO media_items(kind,tmdb_id,title,year,monitored,status,overview,poster_path,release_date) VALUES(?,?,?,?,?,?,?,?,?)",("movie",101,"Disposable backup fixture",2026,0,"missing","","","2026-01-01")); db.commit()'
docker run --rm --network "container:$project" -e REELSCAREY_BACKUP_SMOKE=1 -e REELSCAREY_SMOKE_BASE=http://127.0.0.1:8787 -v "$fixture/config:/fixture:ro" -v "$root/scripts:/verification:ro" node:24-bookworm-slim node /verification/backup-container-smoke.mjs
"${compose[@]}" up -d --force-recreate --pull never
ready
test "$key_before" = "$("${compose[@]}" exec -T reelscarey sha256sum /config/secrets.key)"
test -n "$("${compose[@]}" exec -T reelscarey sh -c 'find /backups -name "*.zip" -print -quit')"
# Recreate as an HTTPS reverse-proxy backend; validate Host/Origin and Secure cookies.
sed -i 's|http://127.0.0.1:18787|https://reelscarey.example.test|' "$fixture/live.env"
"${compose[@]}" up -d --force-recreate --pull never
ready
docker run --rm --network "container:$project" -e REELSCAREY_SMOKE_BASE=http://127.0.0.1:8787 -v "$root/scripts:/verification:ro" node:24-bookworm-slim node /verification/simple-proxy-smoke.mjs
# Refuse invalid identities rather than accidentally serving as root.
for bad in 0 -1 abc 0001 4294967295; do
 if docker run --rm --network none -e PUID="$bad" "$image" version >/dev/null 2>&1; then echo "Invalid PUID accepted: $bad" >&2; exit 1; fi
done
echo 'compose.yaml startup, authenticated boundary, mount permissions, timezone, restart persistence, backup/restore, HTTPS proxy and PUID validation verified.'
