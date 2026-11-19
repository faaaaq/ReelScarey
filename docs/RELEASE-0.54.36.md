# ReelScarey 0.54.36

TIDAL device login now remains alive after Connect TIDAL returns. The provider
persists its waiting state before launching a detached OAuth process, displays
the device link and code while they arrive, rejects duplicate starts and records
completion. A stopped or stale provider now produces an actionable failure
instead of leaving the settings page waiting indefinitely. Provider start
errors retain their actual message.

Offline lifecycle coverage runs the real provider through device-line delivery,
duplicate rejection, successful completion and stale-process recovery, including
the Docker TIDAL Python 3.12 environment. UI coverage verifies link presentation,
polling and successful completion. No TIDAL protocol or credential-storage
behavior changed.
