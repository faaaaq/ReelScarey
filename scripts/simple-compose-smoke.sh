#!/usr/bin/env bash
# Exact simple Compose template, with disposable mounts and an isolated proxy network.
set -euo pipefail
image=${1:-reelscarey:ci}
root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp -d /tmp/reelscarey-simple-check.XXXXXX)
name="reelscarey-simple-check-$$"
network="$name-proxy"
compose=(docker compose --project-name "$name" -f "$fixture/compose.yaml")
cleanup() {
 "${compose[@]}" logs --tail 8 || true
 "${compose[@]}" down --volumes --remove-orphans >/dev/null || true
 docker network rm "$network" >/dev/null || true
 # App-created files are private to the selected UID. Remove only this fixture.
 docker run --rm --network none --user 0 -v "$fixture:/fixture" --entrypoint python3 "$image" -c 'import pathlib,shutil; root=pathlib.Path("/fixture"); [shutil.rmtree(p) for p in root.iterdir() if p.is_dir() and not p.is_symlink()]' || true
 case "$fixture" in /tmp/reelscarey-simple-check.*) rm -rf -- "$fixture" ;; esac
}
trap cleanup EXIT
mkdir "$fixture/config" "$fixture/downloads" "$fixture/anime" "$fixture/tv"
printf 'Preserved owner bytes\n' > "$fixture/anime/owner.txt"
docker run --rm --network none --user 0 -v "$fixture:/fixture" --entrypoint sh "$image" -c 'chown 1000:1000 /fixture/anime /fixture/tv; chmod 750 /fixture/anime /fixture/tv; chown 2345:2345 /fixture/anime/owner.txt'
docker network create --internal "$network" >/dev/null
python3 - "$root/compose.simple.yaml" "$fixture" "$image" "$name" "$network" <<'PY'
import pathlib,sys
source,fixture,image,name,network=sys.argv[1:]
text=pathlib.Path(source).read_text()
for a,b in {'faaaaq/reelscarey:latest':image, 'container_name: reelscarey':'container_name: '+name,
 'https://reelscarey.example.com':'http://127.0.0.1:18787',
 '/home/faaaaq/configs/reelscarey/config':fixture+'/config',
 '/home/faaaaq/massstorage/PlexDL':fixture+'/downloads',
 '/mnt/masstorage/Anime':fixture+'/anime', '/mnt/masstorage/TV':fixture+'/tv',
 '      REELSCAREY_PUBLIC_URL:':'      REELSCAREY_LISTEN_ADDR: 0.0.0.0:18787\n      REELSCAREY_PUBLIC_URL:',
 '    external: true':'    external: true\n    name: '+network}.items(): text=text.replace(a,b)
pathlib.Path(fixture,'compose.yaml').write_text(text)
PY
"${compose[@]}" config --quiet
"${compose[@]}" up -d --pull never
ready() {
 for attempt in {1..60}; do
  if docker exec "$name" reelscarey healthcheck >/dev/null 2>&1; then return; fi
  sleep 1
 done
 echo 'Simple Compose app failed to start.' >&2; return 1
}
ready
docker exec "$name" python3 -c 'import pathlib; s=pathlib.Path("/proc/1/status").read_text(); assert "Uid:\t1000\t1000\t1000\t1000" in s; assert "Gid:\t1000\t1000\t1000\t1000" in s; assert pathlib.Path("/config/state/reelscarey.db").stat().st_uid==1000; p=pathlib.Path("/data/anime/owner.txt"); assert p.stat().st_uid==2345 and p.read_text()=="Preserved owner bytes\n"'
test "$(docker exec "$name" date -d 2026-01-15T12:00:00Z +%Z)" = CST
docker exec -u 1000:1000 "$name" sh -c 'test -w /data/anime && test -w /data/tvshows && test -w /config/backups && test -w /config/youtube-staging/work && test ! -w /downloads; printf retained > /data/.retained-test'
docker exec "$name" reelscarey doctor
key_before=$(docker exec "$name" sha256sum /config/state/secrets.key)
# The same authenticated backup/upload/restore acceptance used by releases.
docker exec -u 1000:1000 "$name" python3 -c 'import sqlite3; db=sqlite3.connect("/config/state/reelscarey.db"); db.execute("INSERT INTO media_items(kind,tmdb_id,title,year,monitored,status,overview,poster_path,release_date) VALUES(?,?,?,?,?,?,?,?,?)",("movie",101,"Disposable backup fixture",2026,0,"missing","","","2026-01-01")); db.commit()'
docker run --rm --network "container:$name" -e REELSCAREY_BACKUP_SMOKE=1 -v "$fixture/config/state:/fixture:ro" -v "$root/scripts:/verification:ro" node:24-bookworm-slim node /verification/backup-container-smoke.mjs
"${compose[@]}" up -d --force-recreate --pull never
ready
test "$key_before" = "$(docker exec "$name" sha256sum /config/state/secrets.key)"
docker exec -u 1000:1000 "$name" sh -c 'test "$(cat /data/.retained-test)" = retained; test -n "$(find /config/backups -name "*.zip" -print -quit)"'
# Recreate as an HTTPS reverse-proxy backend; validate Host/Origin and Secure cookies.
sed -i 's|http://127.0.0.1:18787|https://reelscarey.example.test|' "$fixture/compose.yaml"
"${compose[@]}" up -d --force-recreate --pull never
ready
docker run --rm --network "container:$name" -v "$root/scripts:/verification:ro" node:24-bookworm-slim node /verification/simple-proxy-smoke.mjs
# Refuse invalid identities rather than accidentally serving as root.
for bad in 0 -1 abc 0001 4294967295; do
 if docker run --rm --network none -e PUID="$bad" "$image" version >/dev/null 2>&1; then echo "Invalid PUID accepted: $bad" >&2; exit 1; fi
done
# Explicit non-root installations remain available without initialization rights.
docker run --rm --network none --user 1234:1234 "$image" version
echo 'Simple Compose UID/GID, timezone, separate mounts, backup/restore, recreation and HTTPS proxy checks passed.'