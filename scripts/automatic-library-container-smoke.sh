#!/usr/bin/env bash
set -euo pipefail
image=${1:-reelscarey:ci}
name="reelscarey-automatic-check-$$"
scripts=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
if docker container inspect "$name" >/dev/null 2>&1; then exit 1; fi
config=$(docker volume create --label reelscarey.acceptance=automatic)
cleanup() {
  docker logs --tail 10 "$name" || true
  docker rm -fv "$name" >/dev/null || true
  docker volume rm "$config" >/dev/null || true
}
trap cleanup EXIT
docker run -d --name "$name" --network none --read-only --user 10001:10001 --cap-drop ALL \
  --security-opt no-new-privileges:true --tmpfs /tmp:rw,noexec,nosuid,size=64m \
  -e REELSCAREY_LISTEN_ADDR=127.0.0.1:18787 \
  -v "$config:/config" -v "$scripts:/verification:ro" "$image" >/dev/null
ready() {
  for attempt in {1..60}; do
    if docker exec "$name" reelscarey healthcheck >/dev/null 2>&1; then return; fi
    sleep 1
  done
  echo 'Automatic-matching acceptance app did not start.' >&2
  exit 1
}
ready
docker exec "$name" python3 /verification/automatic-library-seed.py
docker run --rm --network "container:$name" -e REELSCAREY_AUTOMATIC_SMOKE=1 \
  -v "$config:/fixture:ro" -v "$scripts:/verification:ro" \
  node:24-bookworm-slim node /verification/automatic-library-container-smoke.mjs
docker restart "$name" >/dev/null
ready
docker exec "$name" python3 /verification/automatic-library-seed.py --verify
