#!/bin/sh
set -e

if [ "$(id -u)" != "0" ]; then
    exec "$@"
fi

mkdir -p /app/config /app/data /app/lib /app/locales
chown -R botuser:botuser /app/config /app/data /app/lib /app/locales
export HOME=/home/botuser

exec setpriv --reuid=botuser --regid=botuser --init-groups "$@"
