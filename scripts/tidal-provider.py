#!/usr/bin/env python3
"""Headless TIDAL provider for ReelScarey, built on tidaler.

This adapter reuses tidaler's own session, login and download machinery so
ReelScarey never re-implements TIDAL protocol details. It speaks a
machine-readable JSON protocol on stdout; one JSON object per invocation:

    status                       {"available": true, "logged_in": bool, ...}
    verify                       {"logged_in": bool, "quality": "..."} (network check)
    login-begin                  {"started": bool, ...} (device flow in background)
    login-status                 {"phase": "idle|waiting|done", ...}
    search QUERY LIMIT           {"candidates": [...]}
    download URL DIRECTORY       {"ok": bool, "path": ..., "format": ...}
    inspect URL                  {"kind": "...", "title": "...", "entries": [...]}

Secrets (the TIDAL token) live only in tidaler's token.json inside the
configured XDG config dir and are never printed. Errors go to stderr as text.
"""

import io
import json
import os
import sys
import threading

# The provider must print exactly one JSON object on stdout. All library
# chatter goes to a captured sink; uncaught errors go to stderr.
from rich.console import Console
from rich.progress import Progress
from tidalapi import Quality

from tidaler.config import Settings, Tidal
from tidaler.constants import MediaType
from tidaler.download import Download
from tidaler.helper.path import path_config_base, path_file_token
from tidaler.helper.tidal import get_tidal_media_id, get_tidal_media_type, instantiate_media
from tidaler.helper.wrapper import LoggerWrapped

QUALITIES = {"LOW", "HIGH", "LOSSLESS", "HI_RES_LOSSLESS"}
AUDIO_EXTENSIONS = {".flac", ".m4a", ".mp3", ".mp4", ".ogg", ".alac"}
LOGIN_STATE = "reelscarey-login-state.json"
LOGIN_LOCK = threading.Lock()


def out(payload):
    sys.stdout.write(json.dumps(payload) + "\n")
    sys.stdout.flush()


def silent_progress():
    sink = io.StringIO()
    console = Console(file=sink, width=120, force_terminal=False, no_color=True)
    return console


def session_ready():
    settings = Settings()
    tidal = Tidal(settings)
    if not tidal.login_token(do_pkce=True):
        return None, tidal
    if not tidal.login_finalize():
        return None, tidal
    return tidal, tidal


def status():
    logged_in = os.path.isfile(path_file_token())
    quality = os.environ.get("REELSCAREY_TIDAL_QUALITY", "HI_RES_LOSSLESS").upper()
    if quality not in QUALITIES:
        quality = "HI_RES_LOSSLESS"
    out({"available": True, "logged_in": logged_in, "quality": quality})


def verify():
    tidal, _ = session_ready()
    if tidal is None:
        out({"logged_in": False, "detail": "No valid TIDAL session. Run tidal-login again."})
        return
    out({"logged_in": True, "quality": str(tidal.settings.data.quality_audio)})


def login_begin():
    with LOGIN_LOCK:
        state_path = os.path.join(path_config_base(), LOGIN_STATE)
        if os.path.isfile(state_path):
            try:
                with open(state_path, encoding="utf-8") as f:
                    state = json.load(f)
                if state.get("phase") == "waiting":
                    out({"started": False, "reason": "already_running"})
                    return
            except (OSError, ValueError):
                pass
        lines = []

        def fn_print(message=""):
            lines.append(str(message))
            # Rewrite the state file as the device link and code appear, so
            # the login UI can show them while the flow is still waiting.
            _write_state(state_path, {"phase": "waiting", "lines": lines, "result": ""})

        def worker():
            state = {"phase": "waiting", "lines": lines, "result": ""}
            _write_state(state_path, state)
            try:
                settings = Settings()
                tidal = Tidal(settings)
                result = tidal.session.login_oauth_session(fn_print=fn_print)
                if result and tidal.login_finalize():
                    state.update({"phase": "done", "result": "success"})
                else:
                    state.update({"phase": "done", "result": "failed",
                                  "error": "TIDAL device authorization did not complete. Try again."})
            except Exception as error:  # noqa: BLE001 - the owner sees the message
                state.update({"phase": "done", "result": "failed", "error": str(error)[:300]})
            _write_state(state_path, state)

        threading.Thread(target=worker, daemon=True).start()
        out({"started": True})


def login_status():
    state_path = os.path.join(path_config_base(), LOGIN_STATE)
    try:
        with open(state_path, encoding="utf-8") as f:
            state = json.load(f)
    except (OSError, ValueError):
        state = {"phase": "idle"}
    out(state)


def _write_state(path, state):
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        json.dump(state, f)
    os.replace(tmp, path)


def search(query, limit):
    tidal, _ = session_ready()
    if tidal is None:
        out({"candidates": [], "error": "No valid TIDAL session."})
        return
    result = tidal.session.search("track", query)
    tracks = []
    for match in (result or {}).get("tracks") or []:
        artists = ", ".join(a.name for a in getattr(match, "artists", []) or [] if getattr(a, "name", None))
        if not artists:
            artist = getattr(match, "artist", None)
            artists = getattr(artist, "name", "") or ""
        album = getattr(match, "album", None)
        duration = getattr(match, "duration", None)
        track_id = getattr(match, "id", None)
        if not track_id or not getattr(match, "name", None):
            continue
        tracks.append({
            "id": f"https://tidal.com/browse/track/{track_id}",
            "title": str(getattr(match, "name")).strip(),
            "artist": artists.strip(),
            "album": (getattr(album, "name", "") or "") if album else "",
            "duration_seconds": int(duration) if duration else 0,
        })
        if len(tracks) >= limit:
            break
    out({"candidates": tracks})


def _audio_files(directory):
    found = []
    for root, _dirs, files in os.walk(directory):
        for name in files:
            ext = os.path.splitext(name)[1].lower()
            if ext in AUDIO_EXTENSIONS:
                full = os.path.join(root, name)
                found.append((os.path.getmtime(full), full, ext.lstrip(".")))
    return found


def download(url, directory):
    tidal, _ = session_ready()
    if tidal is None:
        out({"ok": False, "error": "No valid TIDAL session."})
        return
    media_type = get_tidal_media_type(url)
    if media_type != MediaType.TRACK:
        out({"ok": False, "error": "Only single TIDAL track links can be downloaded here."})
        return
    track_id = get_tidal_media_id(url)
    if not isinstance(track_id, str):
        out({"ok": False, "error": "The TIDAL track id could not be read from the link."})
        return
    try:
        media = instantiate_media(tidal.session, media_type, track_id)
    except Exception:  # noqa: BLE001 - tidaler reports this honestly
        out({"ok": False, "error": "TIDAL does not have this track (ID: %s)." % track_id})
        return
    os.makedirs(directory, exist_ok=True)
    settings = Settings()
    quality = os.environ.get("REELSCAREY_TIDAL_QUALITY", "HI_RES_LOSSLESS").upper()
    if quality not in QUALITIES:
        quality = "HI_RES_LOSSLESS"
    settings.data.download_base_path = directory
    settings.data.quality_audio = Quality(quality)
    settings.data.video_download = False
    settings.data.download_delay = False
    settings.data.skip_existing = True
    settings.data.format_track = "{artist_name} - {track_title}{track_explicit}"
    settings.data.format_album = "{artist_name} - {track_title}{track_explicit}"
    sink = silent_progress()
    dl = Download(
        tidal_obj=tidal,
        skip_existing=settings.data.skip_existing,
        path_base=directory,
        fn_logger=LoggerWrapped(sink.print),
        progress=Progress(console=sink),
        progress_overall=Progress(console=sink),
    )
    try:
        dl.item(media=media, file_template=settings.data.format_track,
                download_delay=False, quality_audio=settings.data.quality_audio,
                quality_video=settings.data.quality_video)
    except Exception as error:  # noqa: BLE001 - surfaced verbatim to the journal
        out({"ok": False, "error": str(error)[:300]})
        return
    found = _audio_files(directory)
    if not found:
        out({"ok": False, "error": "The TIDAL download finished without an audio file."})
        return
    found.sort(reverse=True)
    out({"ok": True, "path": found[0][1], "format": found[0][2]})


def inspect(url):
    tidal, _ = session_ready()
    if tidal is None:
        out({"kind": "", "error": "No valid TIDAL session."})
        return
    media_type = get_tidal_media_type(url)
    media_id = get_tidal_media_id(url)
    if not isinstance(media_type, MediaType) or not isinstance(media_id, str):
        out({"kind": "", "error": "The link is not a recognizable TIDAL track or album."})
        return
    try:
        media = instantiate_media(tidal.session, media_type, media_id)
    except Exception:  # noqa: BLE001
        out({"kind": "", "error": "TIDAL does not have this media (ID: %s)." % media_id})
        return
    if media_type == MediaType.TRACK:
        artist = getattr(media, "artist", None)
        out({"kind": "track", "title": getattr(media, "name", ""), "channel": getattr(artist, "name", ""),
             "entries": []})
        return
    if media_type != MediaType.ALBUM:
        out({"kind": "", "error": "Only TIDAL track and album links are supported."})
        return
    entries = []
    for track in media.tracks() or []:
        artist = getattr(track, "artist", None)
        entries.append({"id": f"https://tidal.com/browse/track/{getattr(track, 'id', '')}",
                        "title": getattr(track, "name", ""), "channel": getattr(artist, "name", ""),
                        "duration_seconds": int(getattr(track, "duration", 0) or 0)})
    out({"kind": "album", "title": getattr(media, "name", ""), "channel": "", "entries": entries})


def main():
    argv = sys.argv[1:]
    if not argv:
        out({"available": False, "error": "No command given."})
        return
    command = argv[0]
    try:
        if command == "status":
            status()
        elif command == "verify":
            verify()
        elif command == "login-begin":
            login_begin()
        elif command == "login-status":
            login_status()
        elif command == "search" and len(argv) >= 3:
            search(argv[1], max(1, min(int(argv[2]), 25)))
        elif command == "download" and len(argv) >= 3:
            download(argv[1], argv[2])
        elif command == "inspect" and len(argv) >= 2:
            inspect(argv[1])
        else:
            out({"available": False, "error": "Unknown command or missing arguments."})
    except Exception as error:  # noqa: BLE001 - one honest JSON failure object
        out({"available": False, "error": str(error)[:300]})


if __name__ == "__main__":
    main()
