#!/bin/sh
set -e

if [ "$(id -u)" != "0" ]; then
    exec "$@"
fi

mkdir -p /app/config /app/data /app/lib
chown -R botuser:botuser /app/config /app/data /app/lib
export HOME=/home/botuser

exec setpriv --reuid=botuser --regid=botuser --init-groups "$@"
