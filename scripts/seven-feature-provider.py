#!/usr/bin/python3
"""Read-only provider fixture for the disposable container smoke test only."""
import json
import sys

if "--dump-single-json" not in sys.argv:
    sys.exit("This fixture supports inspection only; no public video is downloaded.")
print(json.dumps({
    "id": "PLfixture012345", "title": "Disposable playlist", "channel": "Fixture",
    "entries": [{"id": key, "title": "Generated " + key, "channel": "Fixture", "duration": 2}
                for key in ["fixture0001", "fixture0002"]],
}))
