#!/usr/bin/env python3
"""Offline regression test for the TIDAL provider's catalog search.

A sitecustomize stub mimics tidalapi 0.8.11, whose signature is
search(query, models=[Track]). The older call shape search("track", query)
must fail loudly in the stub, exactly as the real library does with
"Tried to search for an invalid type", so a regression cannot pass silently.
No TIDAL account, network or owner media is used.

Usage: python scripts/test_tidal_search.py
"""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile

SITE_CUSTOMIZE = r'''
import os
import sys
import types


def module(name, **attributes):
    if "." in name:
        parent_name = name.rsplit(".", 1)[0]
        if parent_name not in sys.modules:
            parent = types.ModuleType(parent_name)
            parent.__path__ = []
            sys.modules[parent_name] = parent
        sys.modules[parent_name].__dict__.setdefault(name.rsplit(".", 1)[1], None)
    value = sys.modules.get(name) or types.ModuleType(name)
    value.__dict__.update(attributes)
    sys.modules[name] = value
    if "." in name:
        parent_name, child_name = name.rsplit(".", 1)
        setattr(sys.modules[parent_name], child_name, value)
    return value


module("rich")
module("rich.console", Console=type("Console", (), {"__init__": lambda self, *a, **k: None, "print": lambda self, *a, **k: None}))
module("rich.progress", Progress=type("Progress", (), {"__init__": lambda self, *a, **k: None}))


class Quality:
    def __init__(self, value):
        self.value = value


class Track:
    pass


module("tidalapi", Quality=Quality, Track=Track)
module("tidalapi.media", Track=Track)


class Settings:
    def __init__(self):
        self.data = types.SimpleNamespace(quality_audio="HI_RES_LOSSLESS", quality_video="")


def search(query, models=None, limit=50, offset=0):
    # tidalapi 0.8.11 contract: the first argument is the query and models is
    # a list of model classes. Anything else is an invalid search type.
    if not isinstance(models, list) or Track not in models:
        raise RuntimeError("Tried to search for an invalid type")
    track = types.SimpleNamespace(
        id=13981865,
        name="Gypsys, Tramps & Thieves",
        duration=158,
        artists=[types.SimpleNamespace(name="Cher")],
        artist=types.SimpleNamespace(name="Cher"),
        album=types.SimpleNamespace(name="Gypsys, Tramps & Thieves"),
    )
    return {"tracks": [track]}


class Tidal:
    def __init__(self, settings):
        self.settings = settings
        self.session = types.SimpleNamespace(search=search)

    def login_token(self, do_pkce=True):
        return True

    def login_finalize(self):
        return True


module("tidaler")
module("tidaler.config", Settings=Settings, Tidal=Tidal)
module("tidaler.constants", MediaType=type("MediaType", (), {"TRACK": "track", "ALBUM": "album"}))
module("tidaler.download", Download=type("Download", (), {"__init__": lambda self, *a, **k: None}))
module("tidaler.helper")
module("tidaler.helper.path",
       path_config_base=lambda: os.path.join(os.environ["XDG_CONFIG_HOME"], "tidaler"),
       path_file_token=lambda: os.path.join(os.environ["XDG_CONFIG_HOME"], "tidaler", "token.json"))
module("tidaler.helper.tidal",
       get_tidal_media_id=lambda url: url.rsplit("/", 1)[-1],
       get_tidal_media_type=lambda url: "track",
       instantiate_media=lambda *a, **k: None)
module("tidaler.helper.wrapper", LoggerWrapped=type("LoggerWrapped", (), {"__init__": lambda self, *a, **k: None}))
'''


def main():
    with tempfile.TemporaryDirectory(prefix="tidal-search-test-") as work:
        stub = Path(work) / "sitecustomize.py"
        stub.write_text(SITE_CUSTOMIZE, encoding="utf-8")
        env = os.environ.copy()
        env.update({
            "PYTHONPATH": str(stub.parent),
            "XDG_CONFIG_HOME": str(Path(work) / "config"),
            "HOME": str(Path(work) / "config"),
            "PYTHONNOUSERSITE": "1",
        })
        provider = Path(__file__).with_name("tidal-provider.py")
        result = subprocess.run(
            [sys.executable, str(provider), "search", "Cher Gypsys, Tramps & Thieves", "5"],
            env=env, capture_output=True, text=True, timeout=60,
        )
        if result.returncode != 0:
            raise SystemExit("provider exited %d: %s" % (result.returncode, result.stderr))
        payload = json.loads(result.stdout.strip().splitlines()[-1])
        if payload.get("error"):
            raise SystemExit("TIDAL search failed: %s" % payload["error"])
        candidates = payload.get("candidates") or []
        if len(candidates) != 1:
            raise SystemExit("expected one candidate, got %r" % (candidates,))
        track = candidates[0]
        assert track["id"] == "https://tidal.com/browse/track/13981865", track
        assert track["title"] == "Gypsys, Tramps & Thieves", track
        assert track["artist"] == "Cher", track
        assert track["album"] == "Gypsys, Tramps & Thieves", track
        assert track["duration_seconds"] == 158, track
        print("PASS: search(query, models=[Track]) returns parsed TIDAL candidates")


if __name__ == "__main__":
    main()
