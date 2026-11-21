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

import contextlib
import io
import json
import os
import subprocess
import sys
import time
from threading import Event

try:
    import fcntl
except ImportError:  # Windows development harness; Docker uses fcntl.
    fcntl = None

# The provider must print exactly one JSON object on stdout. All library
# chatter goes to a captured sink; uncaught errors go to stderr.
from rich.console import Console
from rich.progress import Progress
from tidalapi import Quality, Track

from tidaler.config import Settings, Tidal
from tidaler.constants import MediaType
from tidaler.download import Download
from tidaler.helper.path import path_config_base, path_file_token
from tidaler.helper.tidal import get_tidal_media_id, get_tidal_media_type, instantiate_media
from tidaler.helper.wrapper import LoggerWrapped

QUALITIES = {"LOW", "HIGH", "LOSSLESS", "HI_RES_LOSSLESS"}
AUDIO_EXTENSIONS = {".flac", ".m4a", ".mp3", ".mp4", ".ogg", ".alac"}
LOGIN_STATE = "reelscarey-login-state.json"
LOGIN_START_GRACE_SECONDS = 10


def out(payload):
    sys.stdout.write(json.dumps(payload) + "\n")
    sys.stdout.flush()


def silent_progress():
    sink = io.StringIO()
    console = Console(file=sink, width=120, force_terminal=False, no_color=True)
    return console


def session_ready():
    # tidaler/tidalapi may print login or token-refresh chatter while a device
    # flow is still waiting. stdout is ReelScarey's JSON protocol, so isolate
    # library output just as the download progress sink does. Without this,
    # verify can return device text plus JSON and the Go adapter cannot parse it.
    with contextlib.redirect_stdout(io.StringIO()):
        settings = Settings()
        tidal = Tidal(settings)
        if not tidal.login_token(do_pkce=True):
            return None, tidal
        if not tidal.login_finalize():
            return None, tidal
    return tidal, tidal


def requested_quality():
    quality = os.environ.get("REELSCAREY_TIDAL_QUALITY", "HI_RES_LOSSLESS").upper()
    return quality if quality in QUALITIES else "HI_RES_LOSSLESS"


def status():
    # A file can be stale, malformed, copied from another installation or left
    # behind by a failed login. It is credential presence, never proof of a
    # verified account session.
    token_present = os.path.isfile(path_file_token())
    quality = requested_quality()
    detail = ("No TIDAL credential file is stored. Connect TIDAL to authorize this server."
              if not token_present else
              "A TIDAL credential file is present, but the session has not been verified by this local check.")
    out({"available": True, "logged_in": False, "token_present": token_present,
         "verification_state": "not_checked", "quality": quality, "detail": detail})


def verify():
    token_present = os.path.isfile(path_file_token())
    checked_at = int(time.time())
    if not token_present:
        out({"available": True, "logged_in": False, "token_present": False,
             "verification_state": "not_checked", "verified_at": checked_at,
             "detail": "No TIDAL credential file is stored. Connect TIDAL to authorize this server."})
        return
    try:
        tidal, _ = session_ready()
        if tidal is None:
            out({"available": True, "logged_in": False, "token_present": True,
                 "verification_state": "failed", "verified_at": checked_at,
                 "detail": "TIDAL did not accept the stored session. Reconnect TIDAL; do not assume the credential file is valid."})
            return
        out({"available": True, "logged_in": True, "token_present": True,
             "verification_state": "verified", "verified_at": checked_at,
             "quality": requested_quality(),
             "detail": "TIDAL accepted the stored session. The account connection is verified."})
    except Exception as error:  # noqa: BLE001 - actionable, bounded provider detail
        out({"available": True, "logged_in": False, "token_present": token_present,
             "verification_state": "failed", "verified_at": checked_at,
             "detail": "TIDAL session verification failed: " + str(error)[:240] +
                       ". Reconnect TIDAL or check the network and try again."})


def _read_login_state(state_path):
    try:
        with open(state_path, encoding="utf-8") as f:
            return json.load(f)
    except (OSError, ValueError):
        return {"phase": "idle"}


def _login_pid_path(state_path):
    return state_path + ".pid"


def _pid_alive(pid):
    try:
        os.kill(pid, 0)
    except ProcessLookupError:
        return False
    except PermissionError:
        return True
    except OSError:
        return False
    return True


def _login_process_alive(state_path):
    try:
        with open(_login_pid_path(state_path), encoding="ascii") as f:
            return _pid_alive(int(f.read().strip()))
    except (OSError, ValueError):
        return False


def _login_stale(state_path, state):
    if state.get("phase") != "waiting" or _login_process_alive(state_path):
        return False
    try:
        started_at = float(state.get("started_at", 0))
    except (TypeError, ValueError):
        started_at = 0
    return time.time() - started_at > LOGIN_START_GRACE_SECONDS


@contextlib.contextmanager
def _login_lock(state_path):
    # Each provider invocation is a separate process, so a thread lock cannot
    # prevent two clicks from starting competing OAuth flows. Docker uses an
    # flock; the development-only Windows fallback still uses the state fence.
    with open(state_path + ".lock", "a", encoding="ascii") as handle:
        if fcntl is not None:
            fcntl.flock(handle.fileno(), fcntl.LOCK_EX)
        try:
            yield
        finally:
            if fcntl is not None:
                fcntl.flock(handle.fileno(), fcntl.LOCK_UN)


def _remove_login_pid(state_path):
    try:
        os.remove(_login_pid_path(state_path))
    except FileNotFoundError:
        pass


def login_begin():
    state_path = os.path.join(path_config_base(), LOGIN_STATE)
    os.makedirs(path_config_base(), exist_ok=True)
    with _login_lock(state_path):
        state = _read_login_state(state_path)
        if state.get("phase") == "waiting" and not _login_stale(state_path, state):
            out({"started": False, "reason": "already_running"})
            return

        # Persist the waiting state before starting the child. The previous
        # daemon-thread implementation exited with the short-lived provider
        # process, often before this file or the device link ever existed.
        state = {"phase": "waiting", "lines": [], "result": "",
                 "started_at": time.time()}
        _write_state(state_path, state)
        try:
            kwargs = {"stdin": subprocess.DEVNULL, "stdout": subprocess.DEVNULL,
                      "stderr": subprocess.DEVNULL, "close_fds": True}
            if os.name == "nt":
                kwargs["creationflags"] = subprocess.CREATE_NEW_PROCESS_GROUP
            else:
                kwargs["start_new_session"] = True
            process = subprocess.Popen(
                [sys.executable, os.path.abspath(__file__), "login-run"], **kwargs)
        except Exception as error:  # noqa: BLE001 - report an actionable start failure
            state.update({"phase": "done", "result": "failed",
                          "error": "The TIDAL login process could not start: " + str(error)[:240]})
            _write_state(state_path, state)
            out({"started": False, "reason": "start_failed", "error": state["error"]})
            return
        with open(_login_pid_path(state_path), "w", encoding="ascii") as f:
            f.write(str(process.pid))
        out({"started": True})


def login_run():
    # Internal child command: this process intentionally remains alive until
    # the owner finishes (or TIDAL rejects) device authorization.
    state_path = os.path.join(path_config_base(), LOGIN_STATE)
    state = _read_login_state(state_path)
    state.setdefault("started_at", time.time())
    lines = state.get("lines") or []

    def fn_print(message=""):
        lines.append(str(message))
        state.update({"phase": "waiting", "lines": lines, "result": ""})
        _write_state(state_path, state)

    try:
        settings = Settings()
        tidal = Tidal(settings)
        # tidaler 0.1.8 exposes login_oauth_simple, which returns None after
        # the future completes. login_finalize performs the actual session
        # check and persists the token; do not treat its return as the OAuth
        # result itself.
        tidal.session.login_oauth_simple(fn_print=fn_print)
        if tidal.login_finalize():
            state.update({"phase": "done", "result": "success"})
        else:
            state.update({"phase": "done", "result": "failed",
                          "error": "TIDAL device authorization did not complete. Try again."})
    except Exception as error:  # noqa: BLE001 - the owner sees the message
        state.update({"phase": "done", "result": "failed", "error": str(error)[:300]})
    _write_state(state_path, state)
    _remove_login_pid(state_path)


def login_status():
    state_path = os.path.join(path_config_base(), LOGIN_STATE)
    state = _read_login_state(state_path)
    if _login_stale(state_path, state):
        state.update({"phase": "done", "result": "failed",
                      "error": "The TIDAL login provider stopped before authorization completed. Try again."})
        _write_state(state_path, state)
        _remove_login_pid(state_path)
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
    # tidalapi 0.8.x: search(query, models=[Track]). Passing the type as the
    # first positional argument made every search fail with "Tried to search
    # for an invalid type", silently pushing acquisition onto YouTube.
    result = tidal.session.search(query, models=[Track])
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
    quality = requested_quality()
    settings.data.download_base_path = directory
    settings.data.quality_audio = Quality(quality)
    settings.data.video_download = False
    settings.data.download_delay = False
    settings.data.skip_existing = True
    settings.data.format_track = "{artist_name} - {track_title}{track_explicit}"
    settings.data.format_album = "{artist_name} - {track_title}{track_explicit}"
    sink = silent_progress()
    # tidaler 0.1.8 dereferences both events: event_abort must exist and
    # event_run must already be set, otherwise the worker either crashes
    # with "'NoneType' object has no attribute 'is_set'" or waits forever.
    abort_event = Event()
    run_event = Event()
    run_event.set()
    dl = Download(
        tidal_obj=tidal,
        skip_existing=settings.data.skip_existing,
        path_base=directory,
        fn_logger=LoggerWrapped(sink.print),
        progress=Progress(console=sink),
        progress_overall=Progress(console=sink),
        event_abort=abort_event,
        event_run=run_event,
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
        elif command == "login-run":
            login_run()
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
