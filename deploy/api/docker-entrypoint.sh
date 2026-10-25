#!/bin/sh
set -e

# An empty command or flags alone target the API; explicit commands pass through.
if [ "$#" -eq 0 ] || [ "${1#-}" != "$1" ]; then
    set -- /app/sub2api "$@"
fi

if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/data
    # A read-only submount may reject chown. Still drop privileges; never fall
    # back to running the API as root if the data directory is not writable.
    chown -R sub2api:sub2api /app/data 2>/dev/null || true
    exec su-exec sub2api "$@"
fi

exec "$@"
