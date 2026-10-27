#!/bin/sh
set -e

if [ "$(id -u)" = "0" ]; then
    mkdir -p /app/config /app/data /app/lib /app/locales
    chown -R botuser:botuser /app/config /app/data /app/lib /app/locales
    export HOME=/home/botuser
    set -- setpriv --reuid=botuser --regid=botuser --init-groups "$@"
fi

"$@" &
pid=$!
trap 'kill -TERM "$pid" 2>/dev/null' TERM INT
set +e
wait "$pid"
status=$?
if kill -0 "$pid" 2>/dev/null; then
    wait "$pid"
    status=$?
fi
set -e

case "$status" in
    3) echo "DISCORD_BOT_TOKEN is invalid. Fix it and restart the container." ;;
    4) echo "DISCORD_BOT_TOKEN is not set. Set it and restart the container." ;;
    *) exit "$status" ;;
esac

trap 'exit 0' TERM INT
sleep infinity &
wait
