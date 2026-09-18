#!/usr/bin/env bash
# Generate traffic against the local Mercure hub so Ember's Mercure tab and
# Logs › Mercure have something to show.
#
#   SUBSCRIBERS=N ./load.sh  # N SSE subscribers (default 5)
#   DURATION=S    ./load.sh  # stop after S seconds (default: run until Ctrl+C)
#
# Subscribers reconnect every 10 to 30 seconds so both the connected gauge and
# the subscribers counter move. The publisher spreads updates over a few topic
# shapes (a plain name, per-resource URLs with numeric IDs and UUIDs, a private
# one) so By Topic shows the pattern folding and the private column.

set -u

SUBSCRIBERS=${SUBSCRIBERS:-5}
DURATION=${DURATION:-0}

# Same key as MERCURE_*_JWT_KEY in ./compose.yaml.
mercure_key='!ChangeThisMercureHubJWTSecretKey!'
mercure_url="http://localhost:8082/.well-known/mercure"

command -v curl >/dev/null 2>&1 || { echo >&2 "curl is required"; exit 1; }
command -v uuidgen >/dev/null 2>&1 || { echo >&2 "uuidgen is required (it is shipped with macOS and util-linux)"; exit 1; }
command -v openssl >/dev/null 2>&1 || { echo >&2 "openssl is required (used to sign the JWT)"; exit 1; }

b64url() {
  openssl base64 -A | tr '+/' '-_' | tr -d '='
}

# HS256 JWT allowed to publish and subscribe on every topic.
mercure_jwt() {
  local header payload
  header=$(printf '{"alg":"HS256","typ":"JWT"}' | b64url)
  payload=$(printf '{"mercure":{"publish":["*"],"subscribe":["*"]}}' | b64url)
  printf '%s.%s.%s\n' "$header" "$payload" \
    "$(printf '%s.%s' "$header" "$payload" | openssl dgst -sha256 -hmac "$mercure_key" -binary | b64url)"
}

jwt=$(mercure_jwt)

subscriber() {
  while :; do
    curl -sN -o /dev/null --max-time "$((10 + RANDOM % 20))" \
      -H "Authorization: Bearer $jwt" "$mercure_url?topic=*" || true
  done
}

publish() {
  curl -s -o /dev/null --max-time 2 -X POST "$mercure_url" -H "Authorization: Bearer $jwt" "$@" || true
}

publisher() {
  while :; do
    case $((RANDOM % 5)) in
      0) publish -d topic=demo -d "data={\"ts\":$(date +%s)}" ;;
      1) publish -d "topic=https://example.com/books/$((RANDOM % 50))" -d type=book -d 'data={"title":"Dune"}' ;;
      2) publish -d "topic=https://example.com/users/$(uuidgen)" -d type=user -d 'data={"status":"online"}' ;;
      3) publish -d "topic=https://example.com/orders/$((RANDOM % 1000))" -d private=on -d 'data={"state":"paid"}' ;;
      4) publish -d topic=demo -d "topic=https://example.com/books/$((RANDOM % 50))" -d 'data=plain text update' ;;
    esac
    sleep 0.2
  done
}

pids=()
cleanup() {
  trap - INT TERM EXIT
  # The SSE curls outlive their loop by up to 30s unless killed with it.
  # Parents go first so they do not get to report their killed child.
  local children
  children=$(for pid in "${pids[@]}"; do pgrep -P "$pid"; done)
  kill "${pids[@]}" 2>/dev/null || true
  [[ -n "$children" ]] && kill $children 2>/dev/null
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

echo "Starting $SUBSCRIBERS subscribers and one publisher against $mercure_url"
for ((i=0; i<SUBSCRIBERS; i++)); do
  subscriber &
  pids+=("$!")
done
publisher &
pids+=("$!")

if [[ "$DURATION" -gt 0 ]]; then
  sleep "$DURATION"
else
  wait
fi
