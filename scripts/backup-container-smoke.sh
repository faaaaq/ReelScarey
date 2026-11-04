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
docker exec -i "$name" python3 - <<'PY'
import hashlib,json,pathlib,sqlite3,subprocess
db=sqlite3.connect('/config/reelscarey.db')
db.execute("INSERT INTO media_items(kind,tmdb_id,title,year,monitored,status,release_date) VALUES('movie',101,'Disposable backup fixture',2026,0,'available','2026-01-01')")
for label,width,height in [('HD',1920,1080),('4K',3840,2160)]:
    path=pathlib.Path('/media/Movies/Backup fixture')/(label+'.mp4')
    path.parent.mkdir(parents=True,exist_ok=True)
    subprocess.run(['ffmpeg','-v','error','-f','lavfi','-i',f'color=c=blue:s={width}x{height}:r=1','-frames:v','1','-c:v','libx264','-threads','1','-preset','ultrafast',str(path)],check=True)
    probe=json.loads(subprocess.check_output(['ffprobe','-v','error','-select_streams','v:0','-show_entries','stream=codec_name,width,height','-of','json',str(path)]))['streams'][0]
    facts=json.dumps({'codec':probe['codec_name'],'width':probe['width'],'height':probe['height'],'duration':1})
    db.execute("INSERT INTO media_files(movie_id,relative_path,size_bytes,sha256,probe_json,imported_at,origin) VALUES(1,?,?,?,?,1,'existing')",(str(path.relative_to('/media')),path.stat().st_size,hashlib.sha256(path.read_bytes()).hexdigest(),facts))
db.commit()
PY
docker run --rm --network "container:$name" -e REELSCAREY_BACKUP_SMOKE=1 -e REELSCAREY_BACKUP_VERSIONS=1 \
  -v "$config:/fixture:ro" -v "$scripts:/verification:ro" \
  node:24-bookworm-slim node /verification/backup-container-smoke.mjs
