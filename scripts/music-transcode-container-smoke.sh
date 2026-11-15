#!/usr/bin/env bash
# Disposable music transcoding smoke: migration 103, in-container ffmpeg and
# ffprobe, the authenticated music API boundary, Opus stream-cache encoding,
# the finished-file export, byte-range seeking, conversion worker restart
# recovery, corrupt-source safety and the path/credential leak boundary.
# A generated FLAC is registered directly in the catalog (the same direct-SQL
# pattern compose-smoke.sh uses) so this check never depends on the public
# MusicBrainz service and stays deterministic.
set -euo pipefail
image=${1:-reelscarey:ci}
name="reelscarey-music-transcode-check-$$"
fixture=$(mktemp -d /tmp/reelscarey-music-transcode-check.XXXXXX)
port=${REELSCAREY_MUSIC_TRANSCODE_PORT:-18791}
base="http://127.0.0.1:$port"
uid=$(id -u)
gid=$(id -g)
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker run --rm --network none --user 0 -v "$fixture:/fixture" --entrypoint python3 "$image" -c 'import pathlib,shutil; root=pathlib.Path("/fixture"); [shutil.rmtree(p) for p in root.iterdir() if p.is_dir() and not p.is_symlink()]' >/dev/null 2>&1 || true
  case "$fixture" in /tmp/reelscarey-music-transcode-check.*) rm -rf -- "$fixture" ;; esac
}
trap cleanup EXIT

mkdir -p "$fixture/config" "$fixture/media" "$fixture/downloads"
docker run -d --name "$name" \
  -p "127.0.0.1:$port:8787" \
  -e PUID="$uid" -e PGID="$gid" \
  -e REELSCAREY_LISTEN_ADDR=0.0.0.0:8787 \
  -e REELSCAREY_PUBLIC_URL="$base" \
  -v "$fixture/config:/config" \
  -v "$fixture/media:/media" \
  -v "$fixture/downloads:/downloads" \
  "$image" >/dev/null

ready() {
  for _ in $(seq 1 90); do
    if curl -fsS "$base/readyz" >/dev/null 2>&1; then return; fi
    sleep 1
  done
  docker logs "$name" >&2 || true
  echo "container did not become ready" >&2
  return 1
}
ready

# ffmpeg AND ffprobe must execute inside the actual container: the generated
# fixture and every transcode below run in there, never on the host.
docker exec "$name" ffmpeg -version >/dev/null
docker exec "$name" ffprobe -version >/dev/null

# A fresh database reaches migration 103 (music transcoding) with its tables
# and preference columns present.
docker exec -i "$name" python3 <<'PY'
import sqlite3, sys
db = sqlite3.connect("/config/reelscarey.db")
version, count = db.execute(
    "SELECT COALESCE((SELECT MAX(version) FROM schema_migrations),0),"
    " (SELECT COUNT(*) FROM schema_migrations"
    "  WHERE version=103 AND name='migrations/103_music_transcoding.sql')"
).fetchone()
if version < 103 or count != 1:
    sys.exit(f"schema version {version} does not include migration 103")
tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
for table in ("music_quality_profiles", "music_conversion_jobs", "music_stream_cache"):
    if table not in tables:
        sys.exit(f"missing table {table}")
columns = {row[1] for row in db.execute("PRAGMA table_info(music_preferences)")}
for column in ("download_profile", "streaming_quality", "transcoding_enabled",
               "cache_limit_mb", "max_conversions", "original_retention", "fallback_policy"):
    if column not in columns:
        sys.exit(f"missing music_preferences column {column}")
print("schema 103 ok")
PY

code=$(docker exec -u "$uid:$gid" "$name" reelscarey setup-token)
cookies="$fixture/cookies"
setup=$(curl -fsS -c "$cookies" -X POST "$base/api/v1/auth/setup" \
  -H 'Content-Type: application/json' \
  --data "{\"username\":\"admin\",\"password\":\"test-only-password\",\"setup_token\":\"$code\"}")
csrf=$(printf '%s' "$setup" | jq -r .csrf_token)
test -n "$csrf"
api() { curl -fsS -b "$cookies" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' "$@"; }
api_status() { curl -sS -b "$cookies" -o "$fixture/body" -w '%{http_code}' "$@"; }
no_media_path() {
  # The served body or JSON error must never carry the media root or any
  # host absolute path of the container filesystem.
  if grep -aqF '/media' "$1"; then
    echo "response leaked the media root path: $1" >&2
    exit 1
  fi
}

# The music boundary denies unauthenticated reads of catalog, stream and
# export endpoints instead of leaking anything.
test "$(curl -sS -o /dev/null -w '%{http_code}' "$base/api/v1/library/music/files")" = 401
test "$(curl -sS -o /dev/null -w '%{http_code}' "$base/api/v1/library/music/files/1/stream")" = 401
test "$(curl -sS -o /dev/null -w '%{http_code}' "$base/api/v1/library/music/files/1/export?profile=opus-balanced")" = 401

# The music API serves authenticated requests and the builtin transcoding
# profiles decoded by migration 103 are visible.
profiles_json="$fixture/profiles.json"
api "$base/api/v1/library/music/profiles" > "$profiles_json"
printf '%s' "$(cat "$profiles_json")" | jq -e '.profiles[] | select(.key=="opus-balanced" and .codec=="opus")' >/dev/null
printf '%s' "$(cat "$profiles_json")" | jq -e '.profiles[] | select(.key=="opus-economy" and .codec=="opus")' >/dev/null

# Register one generated FLAC directly in the catalog. The audio tags match
# the catalog rows, so a later stream/export proves conversion of real audio,
# not a fixture shortcut.
docker exec -u "$uid:$gid" "$name" mkdir -p "/media/Music/Fixture Artist/Fixture Album"
docker exec -u "$uid:$gid" "$name" ffmpeg -v error -f lavfi -i "sine=frequency=440:duration=1" -c:a flac \
  -metadata artist="Fixture Artist" -metadata album="Fixture Album" \
  -metadata title="Fixture Track" -metadata track=1 -metadata disc=1 \
  -y "/media/Music/Fixture Artist/Fixture Album/01 - Fixture Track.flac"
file_id=$(docker exec -i "$name" python3 <<'PY'
import hashlib, pathlib, sqlite3, time
db = sqlite3.connect("/config/reelscarey.db")
now = int(time.time())
audio = pathlib.Path("/media/Music/Fixture Artist/Fixture Album/01 - Fixture Track.flac")
digest = hashlib.sha256(audio.read_bytes()).hexdigest()
artist = db.execute(
    "INSERT INTO music_artists(musicbrainz_id,name,sort_name,status,monitored,created_at,updated_at)"
    " VALUES(?,?,?,?,1,?,?)",
    ("d1f1f1ed-1111-4111-8111-111111111111", "Fixture Artist", "Fixture Artist", "available", now, now),
).lastrowid
album = db.execute(
    "INSERT INTO music_albums(artist_id,musicbrainz_id,title,status,monitored,created_at,updated_at)"
    " VALUES(?,?,?,?,1,?,?)",
    (artist, "f1a1f1ed-2222-4222-8222-222222222222", "Fixture Album", "available", now, now),
).lastrowid
track = db.execute(
    "INSERT INTO music_tracks(album_id,musicbrainz_id,recording_id,disc,number,title,duration_ms,created_at,updated_at)"
    " VALUES(?,?,?,?,?,?,?,?,?)",
    (album, "", "", 1, 1, "Fixture Track", 1000, now, now),
).lastrowid
file_id = db.execute(
    "INSERT INTO music_files(artist_id,album_id,track_id,relative_path,size_bytes,sha256,format,"
    "sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)"
    " VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
    (artist, album, track, "Music/Fixture Artist/Fixture Album/01 - Fixture Track.flac",
     audio.stat().st_size, digest, "flac", 44100, 2, 1000, "{}", now, "existing", ""),
).lastrowid
db.commit()
print(file_id)
PY
)
test -n "$file_id"

files_json="$fixture/files.json"
api "$base/api/v1/library/music/files" > "$files_json"
test "$(jq -r --argjson id "$file_id" '[.items[] | select(.id==$id)][0].relative_path' "$files_json")" \
  = "Music/Fixture Artist/Fixture Album/01 - Fixture Track.flac"
no_media_path "$files_json"

# A stream request converts the FLAC to Opus inside the container and lands
# the encode in the persistent stream-cache directory.
stream_headers="$fixture/stream.headers"
test "$(curl -sS -D "$stream_headers" -o "$fixture/stream-1.bin" -w '%{http_code}' \
  -b "$cookies" "$base/api/v1/library/music/files/$file_id/stream")" = 200
grep -qi '^content-type: audio/ogg' "$stream_headers"
test "$(head -c 4 "$fixture/stream-1.bin")" = OggS
entries=0
for _ in $(seq 1 60); do
  entries=$(api "$base/api/v1/library/music/cache" | jq -r '.entries // 0')
  [ "$entries" -ge 1 ] && break
  sleep 1
done
test "$entries" -ge 1
docker exec "$name" sh -c 'test -d /media/.reelscarey-stream-cache && test -n "$(find /media/.reelscarey-stream-cache -type f -size +1c -print -quit)"'

# Byte-range seeking works against the cached conversion (the on-the-fly pipe
# is documented as non-range; once the cache entry exists ServeContent handles
# ranges).
test "$(curl -sS -D "$fixture/range.headers" -o "$fixture/range.bin" -w '%{http_code}' \
  -b "$cookies" -H 'Range: bytes=0-1' "$base/api/v1/library/music/files/$file_id/stream")" = 206
grep -qi '^content-range: bytes 0-1/' "$fixture/range.headers"
test "$(wc -c < "$fixture/range.bin")" -eq 2

# The finished-file export with the opus-balanced profile returns a complete
# self-contained Opus file with range support.
export_headers="$fixture/export.headers"
test "$(curl -sS -D "$export_headers" -o "$fixture/export.opus" -w '%{http_code}' \
  -b "$cookies" "$base/api/v1/library/music/files/$file_id/export?profile=opus-balanced")" = 200
grep -qi '^content-type: audio/ogg' "$export_headers"
grep -qi '^content-disposition: attachment; filename="' "$export_headers"
test -s "$fixture/export.opus"
test "$(head -c 4 "$fixture/export.opus")" = OggS
no_media_path "$fixture/export.opus"
test "$(curl -sS -o "$fixture/export-range.bin" -w '%{http_code}' \
  -b "$cookies" -H 'Range: bytes=0-1' "$base/api/v1/library/music/files/$file_id/export?profile=opus-balanced")" = 206
test "$(wc -c < "$fixture/export-range.bin")" -eq 2

# An unknown profile is a plain JSON error with no path or credential leak.
test "$(api_status "$base/api/v1/library/music/files/$file_id/export?profile=not-a-profile")" = 400
no_media_path "$fixture/body"

# The conversion worker converts a real file through the reviewed optimize
# path and publishes a verified variant while the source stays in place.
apply=$(api -X POST "$base/api/v1/library/music/optimize/apply" \
  --data "{\"profile\":\"opus-economy\",\"file_ids\":[$file_id]}")
test "$(printf '%s' "$apply" | jq -r .enqueued)" -ge 1
conversions_json="$fixture/conversions.json"
state=""
for _ in $(seq 1 90); do
  api "$base/api/v1/library/music/conversions" > "$conversions_json"
  state=$(jq -r --argjson id "$file_id" '[.items[] | select(.file_id==$id and .profile=="opus-economy")][0].state' "$conversions_json")
  case "$state" in
    completed) break ;;
    queued|processing) sleep 1 ;;
    *) echo "unexpected conversion state: $state" >&2; exit 1 ;;
  esac
done
test "$state" = completed
output_path=$(jq -r --argjson id "$file_id" '[.items[] | select(.file_id==$id and .profile=="opus-economy")][0].output_path' "$conversions_json")
test -n "$output_path" -a "$output_path" != null
docker exec -u "$uid:$gid" "$name" test -f "/media/$output_path"
docker exec "$name" sh -c "ffprobe -v error -show_entries stream=codec_name -of csv=p=0 '/media/$output_path'" | grep -qx opus
docker exec -u "$uid:$gid" "$name" test -f "/media/Music/Fixture Artist/Fixture Album/01 - Fixture Track.flac"

# A corrupt source must never complete, never damage the source file and
# never leak its absolute path through the queue. The row is inserted with
# the same direct-SQL mechanism compose-smoke.sh already uses for fixtures.
docker exec -u "$uid:$gid" "$name" sh -c 'printf "this is not audio\n" > "/media/Music/Fixture Artist/Fixture Album/broken.bin"'
corrupt_id=$(docker exec -i "$name" python3 <<'PY'
import hashlib, pathlib, sqlite3, time
db = sqlite3.connect("/config/reelscarey.db")
now = int(time.time())
audio = pathlib.Path("/media/Music/Fixture Artist/Fixture Album/broken.bin")
digest = hashlib.sha256(audio.read_bytes()).hexdigest()
row = db.execute("SELECT artist_id,album_id,track_id FROM music_files ORDER BY id LIMIT 1").fetchone()
corrupt_id = db.execute(
    "INSERT INTO music_files(artist_id,album_id,track_id,relative_path,size_bytes,sha256,format,"
    "sample_rate,channels,duration_ms,tag_json,imported_at,origin,source_name)"
    " VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
    (row[0], row[1], row[2], "Music/Fixture Artist/Fixture Album/broken.bin",
     audio.stat().st_size, digest, "", 0, 0, 0, "{}", now, "existing", ""),
).lastrowid
db.commit()
print(corrupt_id)
PY
)
test -n "$corrupt_id"
broken_before=$(docker exec "$name" sha256sum "/media/Music/Fixture Artist/Fixture Album/broken.bin" | cut -d ' ' -f1)
# The reviewed optimize path deliberately refuses to enqueue a file that its
# honest probe cannot inspect, so the queue-level failure path is reached by
# enqueuing the job row directly with the same values EnqueueConversion
# writes. This is a fixture, like the corrupt file row itself, not a bypass
# claim: the worker still runs the real conversion and its real failure
# handling against the row.
corrupt_job=$(docker exec -i "$name" python3 <<PY
import sqlite3, time
db = sqlite3.connect("/config/reelscarey.db")
now = int(time.time())
job = db.execute(
    "INSERT INTO music_conversion_jobs(kind,file_id,profile,state,priority,created_at,updated_at)"
    " VALUES('library',$corrupt_id,'opus-economy','queued',8,?,?)",
    (now, now),
).lastrowid
db.commit()
print(job)
PY
)
test -n "$corrupt_job"
# The failed run retries with a durable backoff before its final failure, so
# this waits for the first completed attempt instead of the terminal state.
state=""; attempts=0
for _ in $(seq 1 90); do
  api "$base/api/v1/library/music/conversions" > "$conversions_json"
  read -r state attempts < <(jq -r --argjson id "$corrupt_job" \
    '[.items[] | select(.id==$id)][0] | "\(.state) \(.attempts)"' "$conversions_json")
  if [ "$attempts" -ge 1 ] && [ "$state" != processing ]; then break; fi
  sleep 1
done
case "$state" in
  queued|failed) ;;
  *) echo "corrupt source reached unexpected state: $state" >&2; exit 1 ;;
esac
broken_after=$(docker exec "$name" sha256sum "/media/Music/Fixture Artist/Fixture Album/broken.bin" | cut -d ' ' -f1)
test "$broken_before" = "$broken_after"
no_media_path "$conversions_json"
corrupt_error=$(jq -r --argjson id "$corrupt_job" '[.items[] | select(.id==$id)][0].error' "$conversions_json")
case "$corrupt_error" in
  *'/media'*) echo "conversion error leaked a path: $corrupt_error" >&2; exit 1 ;;
esac
api "$base/api/v1/library/music/files" > "$files_json"
test "$(jq -r --argjson id "$corrupt_id" '[.items[] | select(.id==$id)][0].variant_role' "$files_json")" = primary
test "$(jq '[.items[] | select(.relative_path | endswith("broken.bin"))] | length' "$files_json")" -eq 1

# Restart recovery: enqueue one more conversion and restart the container
# immediately, so the durable queue must recover the job at startup and
# complete it instead of staying stuck in processing.
apply=$(api -X POST "$base/api/v1/library/music/optimize/apply" \
  --data "{\"profile\":\"opus-premium\",\"file_ids\":[$file_id]}")
test "$(printf '%s' "$apply" | jq -r .enqueued)" -ge 1
restart_job=$(api "$base/api/v1/library/music/conversions" | jq -r \
  --argjson id "$file_id" '[.items[] | select(.file_id==$id and .profile=="opus-premium" and .kind=="library")][0].id')
test -n "$restart_job" -a "$restart_job" != null
docker restart "$name"
ready
state=""
for _ in $(seq 1 120); do
  state=$(api "$base/api/v1/library/music/conversions" | jq -r --argjson id "$restart_job" \
    '[.items[] | select(.id==$id)][0].state')
  case "$state" in
    completed) break ;;
    queued|processing) sleep 1 ;;
    *) echo "restarted conversion reached unexpected state: $state" >&2; exit 1 ;;
  esac
done
test "$state" = completed

# Persistent catalog and cache survive the restart: the session cookie is
# still valid, the registered file and the published variant are still
# listed, and the stream cache serves from disk again.
api "$base/api/v1/library/music/files" > "$files_json"
test "$(jq -r --argjson id "$file_id" '[.items[] | select(.id==$id)][0].relative_path' "$files_json")" \
  = "Music/Fixture Artist/Fixture Album/01 - Fixture Track.flac"
test "$(jq '[.items[] | select(.variant_role=="variant")] | length' "$files_json")" -ge 1
no_media_path "$files_json"
test "$(api "$base/api/v1/library/music/cache" | jq -r '.entries // 0')" -ge 1
docker exec "$name" sh -c 'test -d /media/.reelscarey-stream-cache'
stream_headers="$fixture/stream-restart.headers"
test "$(curl -sS -D "$stream_headers" -o "$fixture/stream-restart.bin" -w '%{http_code}' \
  -b "$cookies" "$base/api/v1/library/music/files/$file_id/stream")" = 200
grep -qi '^content-type: audio/ogg' "$stream_headers"
test "$(head -c 4 "$fixture/stream-restart.bin")" = OggS

echo "Music transcoding container smoke passed: schema 103, in-container ffmpeg/ffprobe, authenticated music API, Opus stream cache, finished-file export with ranges, restart recovery, corrupt-source safety and no path leaks."
