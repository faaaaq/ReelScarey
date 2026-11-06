#!/bin/sh
set -eu
uid=${PUID:-10001}
gid=${PGID:-10001}
for value in "$uid" "$gid"; do
  case "$value" in ''|*[!0-9]*|0|0*) echo 'PUID and PGID must be positive decimal IDs without leading zeros.' >&2; exit 1 ;; esac
  if [ "${#value}" -gt 10 ] || [ "$value" -gt 2147483647 ]; then
    echo 'PUID and PGID must be between 1 and 2147483647.' >&2; exit 1
  fi
done
if [ "$(id -u)" = 0 ]; then
  # Only normal application startup initializes private storage. CLI and health
  # commands drop privileges too, without changing files or permissions.
  if [ "$#" = 0 ]; then
    python3 /usr/local/lib/reelscarey-init.py "$uid" "$gid"
  fi
  # The opt-in VPN supervisor needs NET_ADMIN retained across the privilege
  # drop: the container must also grant cap_add NET_ADMIN for the app process
  # to create the WireGuard interface. The default stays capability-free.
  if [ "${REELSCAREY_KEEP_NET_ADMIN:-0}" = "1" ] && command -v setpriv >/dev/null 2>&1; then
    exec setpriv --reuid "$uid" --regid "$gid" --init-groups --inh-caps=+net_admin --ambient-caps=+net_admin /usr/local/lib/reelscarey "$@"
  fi
  exec gosu "$uid:$gid" /usr/local/lib/reelscarey "$@"
fi
if { [ -n "${PUID:-}" ] && [ "$(id -u)" != "$uid" ]; } ||
   { [ -n "${PGID:-}" ] && [ "$(id -g)" != "$gid" ]; }; then
  echo 'PUID/PGID must match an explicit Docker user. Omit user: to initialize permissions automatically.' >&2
  exit 1
fi
exec /usr/local/lib/reelscarey "$@"