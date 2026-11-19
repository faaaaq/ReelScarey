#!/usr/bin/env python3
"""Verify the real TIDAL device endpoint without an account or token output."""

import json
import subprocess
import time

PYTHON = "/opt/reelscarey-tidal/bin/python3"
PROVIDER = "/work/tidal-provider.py"


def invoke(command):
    return json.loads(subprocess.check_output(
        [PYTHON, PROVIDER, command], text=True,
    ))


started = invoke("login-begin")
assert started.get("started") is True, started
state = {}
for _ in range(45):
    time.sleep(1)
    state = invoke("login-status")
    if any("https://" in str(line) for line in state.get("lines", [])):
        print({
            "verified": "device link received from real TIDAL",
            "phase": state.get("phase"),
            "line_count": len(state.get("lines", [])),
            "code_printed": False,
        })
        break
    if state.get("phase") == "done":
        raise RuntimeError(state.get("error") or "TIDAL login ended before providing a link")
else:
    raise TimeoutError("TIDAL did not provide a device link within 45 seconds")
