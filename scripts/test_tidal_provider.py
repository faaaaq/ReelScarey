#!/usr/bin/env python3
"""Offline lifecycle test for the real TIDAL provider adapter script.

A sitecustomize stub replaces tidaler and its dependencies. No account,
network, token or owner media is used. The test drives separate provider
processes exactly as ReelScarey does.
"""

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

SITE_CUSTOMIZE = r'''
import os
import sys
import time
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
module("rich.console")
module("rich.progress")


class Console:
    def __init__(self, *args, **kwargs):
        pass

    def print(self, *args, **kwargs):
        pass


class Progress:
    def __init__(self, *args, **kwargs):
        pass


sys.modules["rich.console"].Console = Console
sys.modules["rich.progress"].Progress = Progress
module("tidalapi", Quality=lambda value: value)
module("tidaler")
module("tidaler.constants", MediaType=type("MediaType", (), {"TRACK": "track", "ALBUM": "album"}))
module("tidaler.download", Download=type("Download", (), {"__init__": lambda self, *args, **kwargs: None}))
module("tidaler.helper")
module("tidaler.helper.path")
module("tidaler.helper.tidal")
module("tidaler.helper.wrapper", LoggerWrapped=lambda fn: fn)


def path_config_base():
    return os.environ["XDG_CONFIG_HOME"]


def path_file_token():
    return os.path.join(path_config_base(), "token.json")


sys.modules["tidaler.helper.path"].path_config_base = path_config_base
sys.modules["tidaler.helper.path"].path_file_token = path_file_token
sys.modules["tidaler.helper.tidal"].get_tidal_media_id = lambda url: "1"
sys.modules["tidaler.helper.tidal"].get_tidal_media_type = lambda url: "track"
sys.modules["tidaler.helper.tidal"].instantiate_media = lambda *args: None


class Settings:
    pass


class FakeSession:
    def login_oauth_simple(self, fn_print):
        fn_print("https://tidal.test/device")
        fn_print("Code: OFFLINE-TEST")
        marker = os.path.join(path_config_base(), "authorized")
        deadline = time.time() + 10
        while not os.path.exists(marker) and time.time() < deadline:
            time.sleep(0.02)
        if not os.path.exists(marker):
            raise TimeoutError("offline authorization was not completed")


class Tidal:
    def __init__(self, settings):
        self.session = FakeSession()

    def login_token(self, do_pkce=True):
        print("offline library chatter that must not enter the JSON protocol")
        return False

    def login_finalize(self):
        return True


module("tidaler.config", Settings=Settings, Tidal=Tidal)
'''


def invoke(provider, env, *args):
    completed = subprocess.run(
        [sys.executable, str(provider), *args],
        env=env,
        text=True,
        capture_output=True,
        timeout=8,
        check=False,
    )
    if completed.returncode != 0:
        raise AssertionError(
            f"provider {args} exited {completed.returncode}: "
            f"stdout={completed.stdout!r} stderr={completed.stderr!r}"
        )
    lines = completed.stdout.strip().splitlines()
    if not lines:
        raise AssertionError(f"provider {args} returned no JSON: {completed.stderr!r}")
    if len(lines) != 1:
        raise AssertionError(f"provider {args} leaked non-protocol stdout lines: {len(lines)}")
    return json.loads(lines[-1])


def wait_for(provider, env, predicate, description):
    deadline = time.time() + 8
    latest = None
    while time.time() < deadline:
        latest = invoke(provider, env, "login-status")
        if predicate(latest):
            return latest
        time.sleep(0.05)
    raise AssertionError(f"timed out waiting for {description}; last state={latest!r}")


def main():
    provider = Path(__file__).resolve().with_name("tidal-provider.py")
    with tempfile.TemporaryDirectory(prefix="reelscarey-tidal-test-") as directory:
        root = Path(directory)
        (root / "sitecustomize.py").write_text(SITE_CUSTOMIZE, encoding="utf-8")
        env = os.environ.copy()
        env.update({
            "PYTHONPATH": directory,
            "PYTHONDONTWRITEBYTECODE": "1",
            "XDG_CONFIG_HOME": directory,
            "HOME": directory,
        })

        initial = invoke(provider, env, "status")
        assert initial.get("available") is True, initial
        assert initial.get("logged_in") is False, initial
        assert initial.get("token_present") is False, initial
        assert initial.get("verification_state") == "not_checked", initial

        # A leftover file is not an authenticated session. Verify must perform
        # its own check and retain a useful failure instead of trusting status.
        (root / "token.json").write_text("{}", encoding="ascii")
        local = invoke(provider, env, "status")
        assert local.get("logged_in") is False, local
        assert local.get("token_present") is True, local
        assert local.get("verification_state") == "not_checked", local
        checked = invoke(provider, env, "verify")
        assert checked.get("logged_in") is False, checked
        assert checked.get("verification_state") == "failed", checked
        assert checked.get("verified_at", 0) > 0, checked
        assert checked.get("detail"), checked

        started = invoke(provider, env, "login-begin")
        assert started.get("started") is True, started
        state_path = root / "reelscarey-login-state.json"
        assert state_path.is_file(), "login-begin did not persist its waiting state"

        waiting = wait_for(
            provider,
            env,
            lambda state: state.get("phase") == "waiting"
            and "https://tidal.test/device" in state.get("lines", [])
            and "Code: OFFLINE-TEST" in state.get("lines", []),
            "the detached provider to publish its device link",
        )
        assert waiting["result"] == "", waiting

        duplicate = invoke(provider, env, "login-begin")
        assert duplicate.get("started") is False, duplicate
        assert duplicate.get("reason") == "already_running", duplicate

        (root / "authorized").write_text("accepted", encoding="ascii")
        completed = wait_for(
            provider,
            env,
            lambda state: state.get("phase") == "done"
            and state.get("result") == "success",
            "the detached provider to finish authorization",
        )
        assert "https://tidal.test/device" in completed.get("lines", []), completed

        state_path.write_text(json.dumps({
            "phase": "waiting", "lines": [], "result": "", "started_at": 0,
        }), encoding="utf-8")
        stale = invoke(provider, env, "login-status")
        assert stale.get("phase") == "done", stale
        assert stale.get("result") == "failed", stale
        assert "stopped before authorization" in stale.get("error", ""), stale

    print("PASS: detached login publishes device lines, rejects duplicates, completes and recovers stale state")


if __name__ == "__main__":
    main()
