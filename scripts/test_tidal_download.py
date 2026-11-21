#!/usr/bin/env python3
"""Offline regression test for the TIDAL provider's download wiring.

tidaler 0.1.8 dereferences event_abort and waits on event_run: passing no
events crashes with "'NoneType' object has no attribute 'is_set'", and an
unset run event blocks the worker forever. The stub Download asserts the
real provider supplies both events with the run event already set, then
produces a fake audio file. No TIDAL account, network or owner media is used.

Usage: python scripts/test_tidal_download.py
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
module("tidalapi", Quality=lambda value: value, Track=type("Track", (), {}))


class Settings:
    def __init__(self):
        self.data = types.SimpleNamespace(quality_audio="HI_RES_LOSSLESS", quality_video="")


class Tidal:
    def __init__(self, settings):
        self.settings = settings
        self.session = object()

    def login_token(self, do_pkce=True):
        return True

    def login_finalize(self):
        return True


class Download:
    def __init__(self, tidal_obj, skip_existing, path_base, fn_logger, progress,
                 progress_overall, event_abort=None, event_run=None):
        self.path_base = path_base
        self.event_abort = event_abort
        self.event_run = event_run

    def item(self, media, file_template, download_delay, quality_audio, quality_video):
        # tidaler 0.1.8 crashes without event_abort and waits forever unless
        # event_run is set; reproduce that contract in the stub.
        if self.event_abort is None:
            raise AttributeError("'NoneType' object has no attribute 'is_set'")
        if self.event_run is None or not self.event_run.is_set():
            raise RuntimeError("download would wait forever on an unset run event")
        target = os.path.join(self.path_base, "Cher - Gypsys, Tramps & Thieves.flac")
        with open(target, "wb") as f:
            f.write(b"fLaC-offline-fixture")
        return True


module("tidaler")
module("tidaler.config", Settings=Settings, Tidal=Tidal)
module("tidaler.constants", MediaType=type("MediaType", (), {"TRACK": "track", "ALBUM": "album"}))
module("tidaler.download", Download=Download)
module("tidaler.helper")
module("tidaler.helper.path",
       path_config_base=lambda: os.path.join(os.environ["XDG_CONFIG_HOME"], "tidaler"),
       path_file_token=lambda: os.path.join(os.environ["XDG_CONFIG_HOME"], "tidaler", "token.json"))
module("tidaler.helper.tidal",
       get_tidal_media_id=lambda url: url.rsplit("/", 1)[-1],
       get_tidal_media_type=lambda url: "track",
       instantiate_media=lambda session, media_type, media_id: object())
module("tidaler.helper.wrapper", LoggerWrapped=lambda fn: fn)
'''


def main():
    with tempfile.TemporaryDirectory(prefix="tidal-download-test-") as work:
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
        target = Path(work) / "out"
        result = subprocess.run(
            [sys.executable, str(provider), "download", "https://tidal.com/browse/track/13981865", str(target)],
            env=env, capture_output=True, text=True, timeout=60,
        )
        if result.returncode != 0:
            raise SystemExit("provider exited %d: %s" % (result.returncode, result.stderr))
        payload = json.loads(result.stdout.strip().splitlines()[-1])
        if not payload.get("ok"):
            raise SystemExit("download failed: %s" % payload)
        if payload.get("format") != "flac":
            raise SystemExit("unexpected format: %s" % payload)
        path = Path(payload["path"])
        if not path.is_file() or path.read_bytes() != b"fLaC-offline-fixture":
            raise SystemExit("the produced audio file is missing or wrong: %s" % path)
        print("PASS: download supplies event_abort and a set event_run and returns the audio file")


if __name__ == "__main__":
    main()
