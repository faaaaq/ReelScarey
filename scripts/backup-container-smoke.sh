#!/usr/bin/env bash
# Disposable containers and volumes only; never attach an existing app volume.
set -euo pipefail
image=${1:-reelscarey:ci}
name="reelscarey-backup-check-$$"
scripts=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
if docker container inspect "$name" >/dev/null 2>&1; then
  echo "Acceptance container already exists; refusing to reuse it." >&2
  exit 1
fi
config=$(docker volume create --label reelscarey.acceptance=backup)
archives=$(docker volume create --label reelscarey.acceptance=backup)
cleanup() {
  docker logs --tail 12 "$name" || true
  docker rm -fv "$name" >/dev/null || true
  docker volume rm "$config" "$archives" >/dev/null || true
}
trap cleanup EXIT
docker run -d --name "$name" --network none --read-only --user 10001:10001 --cap-drop ALL \
  --security-opt no-new-privileges:true --tmpfs /tmp:rw,noexec,nosuid,size=64m \
  -e REELSCAREY_LISTEN_ADDR=127.0.0.1:18787 \
  -v "$config:/config" -v "$archives:/backups" "$image" >/dev/null
for attempt in {1..60}; do
  if docker exec "$name" reelscarey healthcheck >/dev/null 2>&1; then break; fi
  if [ "$attempt" = 60 ]; then echo "Acceptance app did not start." >&2; exit 1; fi
  sleep 1
done
docker exec "$name" python3 -c 'import sqlite3; db=sqlite3.connect("/config/reelscarey.db"); db.execute("INSERT INTO media_items(kind,tmdb_id,title,year,monitored,status,overview,poster_path,release_date) VALUES(?,?,?,?,?,?,?,?,?)",("movie",101,"Disposable backup fixture",2026,0,"missing","","","2026-01-01")); db.commit()'
docker run --rm --network "container:$name" -e REELSCAREY_BACKUP_SMOKE=1 \
  -v "$config:/fixture:ro" -v "$scripts:/verification:ro" \
  node:24-bookworm-slim node /verification/backup-container-smoke.mjs
