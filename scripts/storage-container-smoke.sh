#!/usr/bin/env bash
set -euo pipefail
image=${1:-reelscarey:ci}
name="reelscarey-storage-check-$$"
scripts=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
if docker container inspect "$name" >/dev/null 2>&1; then exit 1; fi
config=$(docker volume create --label reelscarey.acceptance=storage)
media=$(docker volume create --label reelscarey.acceptance=storage)
downloads=$(docker volume create --label reelscarey.acceptance=storage)
backups=$(docker volume create --label reelscarey.acceptance=storage)
target=$(docker volume create --label reelscarey.acceptance=storage)
cleanup(){
 docker logs --tail 8 "$name" || true
 docker rm -fv "$name" >/dev/null || true
 docker volume rm "$config" "$media" "$downloads" "$backups" "$target" >/dev/null || true
}
trap cleanup EXIT
docker run --rm --network none --user 0 -v "$target:/migration-target" --entrypoint sh "$image" -c 'chown 10001:10001 /migration-target; printf "Disposable external file\n" > /migration-target/outside.txt; chmod 644 /migration-target/outside.txt'
start_app(){
 docker run -d --name "$name" --network none --read-only --user 10001:10001 --cap-drop ALL \
  --security-opt no-new-privileges:true --tmpfs /tmp:rw,noexec,nosuid,size=64m \
  -e REELSCAREY_LISTEN_ADDR=127.0.0.1:18787 -e 'REELSCAREY_BROWSE_ROOTS={"archive":"/archive"}' \
  -v "$config:/config" "${media_args[@]}" -v "$downloads:/downloads:ro" \
  -v "$backups:/backups" -v "$target:/archive:ro" -v "$scripts:/verification:ro" "$image" >/dev/null
 for attempt in {1..60};do
  if docker exec "$name" reelscarey healthcheck >/dev/null 2>&1;then return;fi
  if [ "$attempt" = 60 ];then echo 'Disposable app failed to start.' >&2;return 1;fi
  sleep 1
 done
}
check_app(){
 docker run --rm --network "container:$name" -e REELSCAREY_STORAGE_SMOKE=1 \
  -v "$config:/fixture:ro" -v "$scripts:/verification:ro" node:24-bookworm-slim \
  node /verification/storage-container-smoke.mjs "$1"
}
maintenance(){
 docker run --rm --network none --read-only --tmpfs /tmp --user 10001:10001 --cap-drop ALL \
  --security-opt no-new-privileges:true -v "$config:/config" "${media_args[@]}" \
  -v "$downloads:/downloads:ro" -v "$backups:/backups" -v "$target:/migration-target" "$image" "$@"
}
media_args=(-v "$media:/media")
start_app
docker exec "$name" python3 /verification/seven-feature-seed.py
check_app before
docker stop "$name" >/dev/null
plan=$(maintenance media-root-plan /migration-target/library)
digest=$(printf '%s' "$plan" | python3 -c 'import json,sys; print(json.load(sys.stdin)["digest"])')
maintenance media-root-copy "$digest"
maintenance media-root-copy "$digest"
if maintenance media-root-activate "$digest" >/dev/null 2>&1;then echo 'Original mount was incorrectly activated.' >&2;exit 1;fi
docker start "$name" >/dev/null
sleep 1
if [ "$(docker inspect --format '{{.State.Running}}' "$name")" != false ];then echo 'Pending migration did not block startup.' >&2;exit 1;fi
docker rm -v "$name" >/dev/null
# The Docker daemon owns volume storage. Do not traverse /var/lib/docker as
# the calling user; that fails on normal non-root CI and remote Docker hosts.
media_args=(--mount "type=volume,src=$target,dst=/media,volume-subpath=library")
maintenance media-root-activate "$digest"
start_app
check_app after
docker restart "$name" >/dev/null
for attempt in {1..60};do if docker exec "$name" reelscarey healthcheck >/dev/null 2>&1;then break;fi;sleep 1;done
check_app after
docker exec "$name" python3 -c 'import hashlib,pathlib,sqlite3; db=sqlite3.connect("/config/reelscarey.db"); rows=db.execute("SELECT relative_path,sha256 FROM media_files").fetchall(); assert rows; assert all(hashlib.sha256((pathlib.Path("/media")/p).read_bytes()).hexdigest()==s for p,s in rows); assert db.execute("PRAGMA integrity_check").fetchone()[0]=="ok"; assert db.execute("PRAGMA foreign_key_check").fetchall()==[]; print("Generated video hashes and catalog integrity verified after mount activation and restart.")'
docker run --rm --network none -v "$media:/original:ro" --mount "type=volume,src=$target,dst=/copied,volume-subpath=library,readonly" --entrypoint python3 "$image" -c 'import pathlib,hashlib; root=pathlib.Path("/original"); files=[p for p in root.rglob("*") if p.is_file()]; assert len(files)==4; assert all(hashlib.sha256(p.read_bytes()).digest()==hashlib.sha256((pathlib.Path("/copied")/p.relative_to(root)).read_bytes()).digest() for p in files); print("All original media and sidecar bytes retained and copied exactly.")'
