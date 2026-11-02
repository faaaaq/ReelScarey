#!/usr/bin/env bash
set -euo pipefail
image=${1:-reelscarey:ci}
name="reelscarey-seven-check-$$"
scripts=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
if docker container inspect "$name" >/dev/null 2>&1; then exit 1; fi
config=$(docker volume create --label reelscarey.acceptance=seven)
cleanup() {
  docker logs --tail 10 "$name" || true
  docker rm -fv "$name" >/dev/null || true
  docker volume rm "$config" >/dev/null || true
}
trap cleanup EXIT
docker run -d --name "$name" --network none --read-only --user 10001:10001 --cap-drop ALL \
  --security-opt no-new-privileges:true --tmpfs /tmp:rw,noexec,nosuid,size=64m \
  -e REELSCAREY_LISTEN_ADDR=127.0.0.1:18787 \
  -e REELSCAREY_YTDLP_PATH=/verification/seven-feature-provider.py \
  -v "$config:/config" -v "$scripts:/verification:ro" "$image" >/dev/null
for attempt in {1..60}; do
  if docker exec "$name" reelscarey healthcheck >/dev/null 2>&1; then break; fi
  if [ "$attempt" = 60 ]; then echo "Acceptance app did not start." >&2; exit 1; fi
  sleep 1
done
docker exec "$name" python3 /verification/seven-feature-seed.py
docker run --rm --network "container:$name" -e REELSCAREY_SEVEN_SMOKE=1 \
  -v "$config:/fixture:ro" -v "$scripts:/verification:ro" \
  node:24-bookworm-slim node /verification/seven-feature-container-smoke.mjs
docker exec "$name" python3 /verification/seven-feature-seed.py --verify
