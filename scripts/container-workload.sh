#!/usr/bin/env bash
set -euo pipefail
image=${1:-reelscarey:ci}
seconds=${2:-300}
name="reelscarey-workload-$$"
scripts=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
if docker container inspect "$name" >/dev/null 2>&1;then exit 1;fi
config=$(docker volume create --label reelscarey.acceptance=workload)
metrics=$(mktemp)
sampler=''
cleanup(){
 if [ -n "$sampler" ];then kill "$sampler" 2>/dev/null || true;wait "$sampler" 2>/dev/null || true;fi
 docker logs --tail 4 "$name" || true
 docker rm -fv "$name" >/dev/null || true
 docker volume rm "$config" >/dev/null || true
 rm -f -- "$metrics"
}
trap cleanup EXIT
docker run -d --name "$name" --network none --read-only --cpus=1 --memory=384m --pids-limit=128 \
 --cap-drop ALL --security-opt no-new-privileges:true --tmpfs /tmp:rw,noexec,nosuid,size=64m \
 -e REELSCAREY_LISTEN_ADDR=127.0.0.1:18787 -v "$config:/config" "$image" >/dev/null
for attempt in {1..60};do if docker exec "$name" reelscarey healthcheck >/dev/null 2>&1;then break;fi;if [ "$attempt" = 60 ];then exit 1;fi;sleep 1;done
docker exec "$name" python3 -c 'import sqlite3; db=sqlite3.connect("/config/reelscarey.db"); db.executemany("INSERT INTO media_items(kind,tmdb_id,title,year,monitored,overview,poster_path,release_date) VALUES(?,?,?,?,0,?,?,?)",[("movie",n,"Synthetic film %04d"%n,2026,"Generated catalog fixture.","","2026-10-29") for n in range(1,1001)]); db.commit()'
(while docker inspect --format '{{.State.Running}}' "$name" 2>/dev/null | grep -qx true;do docker stats --no-stream --format '{{json .}}' "$name" >> "$metrics";sleep 3;done)&
sampler=$!
docker run --rm --network "container:$name" -e REELSCAREY_WORKLOAD=1 -v "$config:/fixture:ro" \
 -v "$scripts:/verification:ro" node:24-bookworm-slim node /verification/container-workload.mjs "$seconds"
kill "$sampler" 2>/dev/null || true
wait "$sampler" 2>/dev/null || true
sampler=''
python3 - "$metrics" <<'PY'
import json,re,sys
rows=[json.loads(line) for line in open(sys.argv[1]) if line.strip()]
def mib(text):
 value,unit=re.match(r'([0-9.]+)([A-Za-z]+)',text.split('/')[0].strip()).groups()
 return float(value)*{'B':1/1048576,'KiB':1/1024,'MiB':1,'GiB':1024}[unit]
memory=[mib(r['MemUsage']) for r in rows]
assert len(rows)>=10, 'Insufficient resource samples'
assert max(memory)<192, 'Working memory exceeded the 192 MiB scenario budget'
assert max(int(r['PIDs']) for r in rows)<128, 'Process budget exceeded'
print(json.dumps({'samples':len(rows),'peak_mib':round(max(memory),2),'final_mib':round(memory[-1],2),'processes_peak':max(int(r['PIDs']) for r in rows),'container_limits':'1 CPU / 384 MiB / 128 processes'}))
PY
docker restart "$name" >/dev/null
for attempt in {1..60};do if docker exec "$name" reelscarey healthcheck >/dev/null 2>&1;then break;fi;sleep 1;done
docker exec "$name" python3 -c 'import sqlite3; db=sqlite3.connect("/config/reelscarey.db"); assert db.execute("SELECT COUNT(*) FROM media_items WHERE kind=?",("movie",)).fetchone()[0]==1000; assert db.execute("PRAGMA integrity_check").fetchone()[0]=="ok"; assert db.execute("PRAGMA foreign_key_check").fetchall()==[]; print("Post-workload restart, catalog and integrity verified.")'
