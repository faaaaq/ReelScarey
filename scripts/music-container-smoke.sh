#!/usr/bin/env bash
# Disposable music catalog, scan, registration, organization, tag-write and
# rollback check. Uses a generated FLAC and the public MusicBrainz service.
set -euo pipefail
image=${1:-reelscarey:ci}
name="reelscarey-music-check-$$"
fixture=$(mktemp -d /tmp/reelscarey-music-check.XXXXXX)
port=${REELSCAREY_MUSIC_PORT:-18790}
base="http://127.0.0.1:$port"
uid=$(id -u)
gid=$(id -g)
cleanup() {
  docker rm -f "$name" >/dev/null 2>&1 || true
  docker run --rm --network none --user 0 -v "$fixture:/fixture" --entrypoint python3 "$image" -c 'import pathlib,shutil; root=pathlib.Path("/fixture"); [shutil.rmtree(p) for p in root.iterdir() if p.is_dir() and not p.is_symlink()]' >/dev/null 2>&1 || true
  case "$fixture" in /tmp/reelscarey-music-check.*) rm -rf -- "$fixture" ;; esac
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

# Chromaprint is used for AcoustID matching and must be present in the image.
docker exec "$name" fpcalc -version >/dev/null

code=$(docker exec -u "$uid:$gid" "$name" reelscarey setup-token)
cookies="$fixture/cookies"
setup=$(curl -fsS -c "$cookies" -X POST "$base/api/v1/auth/setup" \
  -H 'Content-Type: application/json' \
  --data "{\"username\":\"admin\",\"password\":\"test-only-password\",\"setup_token\":\"$code\"}")
csrf=$(printf '%s' "$setup" | jq -r .csrf_token)
test -n "$csrf"
api() { curl -fsS -b "$cookies" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' "$@"; }
retry_api() {
  for _ in $(seq 1 8); do
    if out=$("$@"); then printf '%s' "$out"; return 0; fi
    sleep 15
  done
  return 1
}

artist_mbid=a74b1b7f-71a5-4011-9441-d0b5e4122711
album_mbid=b1392450-e666-3926-a536-22c65f834433
artist=$(retry_api api -X POST "$base/api/v1/library/music/artists" \
  --data "{\"musicbrainz_id\":\"$artist_mbid\"}")
artist_id=$(printf '%s' "$artist" | jq -r .item.id)
test -n "$artist_id" -a "$artist_id" != null
details=$(retry_api api "$base/api/v1/library/music/artists/$artist_id")
album_id=$(printf '%s' "$details" | jq -r --arg mbid "$album_mbid" '.albums[] | select(.musicbrainz_id==$mbid) | .id' | head -1)
test -n "$album_id"
album=$(retry_api api "$base/api/v1/library/music/albums/$album_id")
track_title=$(printf '%s' "$album" | jq -r '.tracks[] | select(.number==1) | .title' | head -1)
test -n "$track_title"

docker exec -u "$uid:$gid" "$name" mkdir -p "/media/Music/Radiohead/OK Computer"
docker exec -u "$uid:$gid" "$name" ffmpeg -v error -f lavfi -i "sine=frequency=440:duration=1" -c:a flac \
  -metadata artist=Radiohead -metadata album="OK Computer" \
  -metadata title="$track_title" -metadata track=1 -metadata disc=1 \
  -y "/media/Music/Radiohead/OK Computer/01 - $track_title.flac"

scan=$(api -X POST "$base/api/v1/library/music/scans" --data '{"root":"Music"}')
scan_id=$(printf '%s' "$scan" | jq -r .id)
for _ in $(seq 1 60); do
  state=$(api "$base/api/v1/library/music/scans/$scan_id" | jq -r .scan.state)
  [ "$state" != scanning ] && break
  sleep 1
done
page=$(api "$base/api/v1/library/music/scans/$scan_id")
test "$(printf '%s' "$page" | jq -r .scan.state)" = review
test "$(printf '%s' "$page" | jq -r .scan.matched_count)" -ge 1
scan_file=$(printf '%s' "$page" | jq -r '.items[] | select(.state=="matched") | .id' | head -1)
test -n "$scan_file"
api -X POST "$base/api/v1/library/music/scans/$scan_id/register" \
  --data "{\"file_ids\":[$scan_file],\"reviewed\":true}" >/dev/null

file_id=$(api "$base/api/v1/library/music/files" | jq -r '.items[0].id')
test -n "$file_id" -a "$file_id" != null
plan=$(api -X POST "$base/api/v1/library/music/edits/preview" --data "{\"file_ids\":[$file_id]}")
plan_hash=$(printf '%s' "$plan" | jq -r .plan_hash)
source_path=$(printf '%s' "$plan" | jq -r '.files[0].source_path')
dest_path=$(printf '%s' "$plan" | jq -r '.files[0].destination_path')
action=$(printf '%s' "$plan" | jq -r '.files[0].action')
test -n "$plan_hash" -a -n "$source_path" -a -n "$dest_path"
case "$action" in move|retag|move_retag) ;; *) echo "unexpected action: $action" >&2; exit 1 ;; esac

job=$(api -X POST "$base/api/v1/library/music/edits/apply" \
  --data "{\"file_ids\":[$file_id],\"plan_hash\":\"$plan_hash\",\"reviewed\":true}")
job_id=$(printf '%s' "$job" | jq -r .id)
for _ in $(seq 1 60); do
  state=$(api "$base/api/v1/library/music/edits/$job_id" | jq -r .state)
  [ "$state" != running ] && break
  sleep 1
done
test "$(api "$base/api/v1/library/music/edits/$job_id" | jq -r .state)" = completed
docker exec -u "$uid:$gid" "$name" sh -c "ffprobe -v error -show_entries format_tags=musicbrainz_artistid -of default=nw=1 '/media/$dest_path' | grep -q '$artist_mbid'"
docker exec -u "$uid:$gid" "$name" test -f "/media/$source_path.reelscarey-backup"

api -X POST "$base/api/v1/library/music/edits/$job_id/rollback" --data '{"reviewed":true}' >/dev/null
docker exec -u "$uid:$gid" "$name" test -f "/media/$source_path"
docker exec -u "$uid:$gid" "$name" test ! -f "/media/$dest_path"
docker exec -u "$uid:$gid" "$name" test ! -f "/media/$source_path.reelscarey-backup"

# Reviewed completed-folder import: generate one FLAC per catalog track.
printf '%s' "$album" | jq -r '.tracks[] | "\(.disc)|\(.number)|\(.title)"' > "$fixture/tracks.txt"
track_count=$(wc -l < "$fixture/tracks.txt")
test "$track_count" -ge 1
docker cp "$fixture/tracks.txt" "$name:/tmp/tracks.txt"
docker exec -u "$uid:$gid" "$name" sh -c 'mkdir -p /downloads/Album; while IFS="|" read -r disc number title; do ffmpeg -v error -f lavfi -i "sine=frequency=440:duration=0.2" -c:a flac -metadata artist=Radiohead -metadata album="OK Computer" -metadata title="$title" -metadata track="$number" -metadata disc="$disc" -y "/downloads/Album/${disc}-${number}.flac"; done < /tmp/tracks.txt'
import_status=$(curl -sS -o "$fixture/import.json" -w '%{http_code}' -b "$cookies" \
  -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/json' -X POST \
  "$base/api/v1/library/music/albums/$album_id/import" \
  --data '{"source_path":"Album","root":"Music"}')
if [ "$import_status" != 202 ]; then
  cat "$fixture/import.json" >&2
  exit 1
fi
import=$(cat "$fixture/import.json")
import_id=$(printf '%s' "$import" | jq -r .id)
for _ in $(seq 1 90); do
  state=$(api "$base/api/v1/library/music/imports/$import_id" | jq -r .state)
  [ "$state" != running ] && break
  sleep 1
done
import_job=$(api "$base/api/v1/library/music/imports/$import_id")
test "$(printf '%s' "$import_job" | jq -r .state)" = completed
test "$(printf '%s' "$import_job" | jq -r .imported_count)" -eq "$track_count"
first_title=$(printf '%s' "$album" | jq -r '.tracks[] | select(.number==1) | .title' | head -1)
docker exec -u "$uid:$gid" "$name" test -f "/media/Music/Radiohead/OK Computer/01 $first_title.flac"
docker exec -u "$uid:$gid" "$name" test -f "/downloads/Album/1-1.flac"
echo "Music catalog, scan, registration, organization, tag write, rollback and folder import passed."
