#!/usr/bin/env bash
#
# Generates traffic against the local Caddy/FrankenPHP setup
# for demo purposes (Ember TUI recording).
#
# Usage: ./demo-traffic.sh [duration_seconds] [concurrency]
# Default: 60 seconds, 8 concurrent workers
#
# Requires: curl

set -euo pipefail

DURATION=${1:-60}
CONCURRENCY=${2:-8}

MAIN="https://localhost"
APP="https://app.localhost:8443"
API="https://api.localhost:9443"

ROUTES=(
    "$MAIN/"
    "$MAIN/"
    "$MAIN/"
    "$MAIN/blog/"
    "$MAIN/blog/"
    "$MAIN/blog/page/1"
    "$MAIN/blog/rss.xml"
    "$MAIN/blog/search?q=lorem"
    "$MAIN/login"

    "$APP/"
    "$APP/"
    "$APP/blog/"
    "$APP/blog/page/1"
    "$APP/blog/search?q=test"

    "$API/"
    "$API/blog/"
    "$API/blog/rss.xml"

    "$MAIN/leak/"
    "$APP/leak/leaker"

    "$MAIN/nonexistent"
    "$API/this-does-not-exist"
)

ROUTE_COUNT=${#ROUTES[@]}

worker() {
    local end=$((SECONDS + DURATION))
    while [ $SECONDS -lt $end ]; do
        curl -sk -o /dev/null "${ROUTES[$((RANDOM % ROUTE_COUNT))]}" 2>/dev/null || true
        sleep "0.0$((RANDOM % 5 + 1))"
    done
}

echo "Sending traffic for ${DURATION}s with ${CONCURRENCY} concurrent workers..."
echo "Press Ctrl+C to stop early."

pids=()
for ((i = 0; i < CONCURRENCY; i++)); do
    worker &
    pids+=($!)
done

trap 'kill "${pids[@]}" 2>/dev/null; exit 0' INT TERM

wait "${pids[@]}" 2>/dev/null
echo "Done."
