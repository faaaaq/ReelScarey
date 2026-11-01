#!/usr/bin/env bash
# Exercises the published-image server Compose layout with disposable data only.
set -euo pipefail
image=${1:-reelscarey:ci}
project="reelscarey-server-check-$$"
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp -d /tmp/reelscarey-server-check.XXXXXX)
compose=(docker compose --project-name "$project" --env-file "$fixture/server.env" -f "$root/compose.server.yaml")
cleanup() {
 "${compose[@]}" logs --tail 6 || true
 "${compose[@]}" down --volumes --remove-orphans >/dev/null || true
 case "$fixture" in /tmp/reelscarey-server-check.*) rm -rf -- "$fixture" ;; esac
}
trap cleanup EXIT
mkdir "$fixture/media" "$fixture/downloads"
# Configure permissions inside Docker so this also runs as an ordinary host user.
docker run --rm --network none --user 0 -v "$fixture:/fixture" --entrypoint sh "$image" -c 'chown 10001:10001 /fixture/media /fixture/downloads; chmod 750 /fixture/media /fixture/downloads'
cat > "$fixture/server.env" <<EOF
REELSCAREY_IMAGE=$image
REELSCAREY_BIND_IP=127.0.0.1
REELSCAREY_PORT=18789
REELSCAREY_PUBLIC_URL=http://127.0.0.1:18789
MEDIA_PATH=$fixture/media
DOWNLOADS_PATH=$fixture/downloads
EOF
"${compose[@]}" config --quiet
"${compose[@]}" up -d --pull never
ready() {
 for attempt in {1..60}; do
  if "${compose[@]}" exec -T reelscarey reelscarey healthcheck >/dev/null 2>&1; then return; fi
  sleep 1
 done
 echo 'Disposable server Compose installation failed to start.' >&2
 return 1
}
ready
curl --fail --silent http://127.0.0.1:18789/readyz >/dev/null
test "$(curl --silent --output /dev/null --write-out '%{http_code}' http://127.0.0.1:18789/api/v1/system)" = 401
"${compose[@]}" exec -T reelscarey sh -c 'test "$(id -u)" = 10001 && test -w /media && test ! -w /downloads && test -w /backups && test -w /youtube-staging'
key_before=$("${compose[@]}" exec -T reelscarey sha256sum /config/secrets.key)
"${compose[@]}" exec -T reelscarey reelscarey doctor
"${compose[@]}" restart reelscarey
ready
test "$key_before" = "$("${compose[@]}" exec -T reelscarey sha256sum /config/secrets.key)"
"${compose[@]}" exec -T reelscarey reelscarey doctor
echo 'Ubuntu server Compose startup, authenticated boundary, mount permissions and restart persistence verified.'