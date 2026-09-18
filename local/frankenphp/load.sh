#!/usr/bin/env bash
# Generate traffic against the local FrankenPHP stack so Ember's Logs and
# By-Route tabs have something to show.
#
#   WORKERS=N  ./load.sh     # N parallel curl loops (default 20)
#   DURATION=S ./load.sh     # stop after S seconds (default: run until Ctrl+C)
#   SUBSCRIBERS=N ./load.sh  # N Mercure SSE subscribers (default 5, 0 to disable)
#
# Hits a mix of methods and endpoints so status codes span 200/404/500
# (index.php responds based on the requested path). Some routes embed UUIDs,
# numeric IDs and long hex strings so the By Route view's normalization
# (`:uuid`, `:id`, `:hash`) shows visible aggregation instead of one row per
# concrete value.

set -u

WORKERS=${WORKERS:-20}
DURATION=${DURATION:-0}
SUBSCRIBERS=${SUBSCRIBERS:-5}

# Same key as the mercure block in ./Caddyfile.
mercure_key='!ChangeThisMercureHubJWTSecretKey!'
mercure_url="http://localhost:8080/.well-known/mercure"

base_url="http://localhost:8080"

static_paths=(
  "/"
  "/deep/path"
  "/api/users"
  "/health"
  "/notfound"
  "/error"
)

methods=(GET GET GET GET GET POST PUT DELETE HEAD)

command -v curl >/dev/null 2>&1 || { echo >&2 "curl is required"; exit 1; }
command -v uuidgen >/dev/null 2>&1 || { echo >&2 "uuidgen is required (it is shipped with macOS and util-linux)"; exit 1; }
command -v openssl >/dev/null 2>&1 || { echo >&2 "openssl is required (used for hex hashes)"; exit 1; }

gen_dynamic_path() {
  case $((RANDOM % 4)) in
    0) printf '/users/%s\n' "$(uuidgen)" ;;
    1) printf '/orders/%d\n' "$((RANDOM % 100000))" ;;
    2) printf '/sessions/%s\n' "$(openssl rand -hex 16)" ;;
    3) printf '/artifacts/%s\n' "$(openssl rand -hex 32)" ;;
  esac
}

pick_url() {
  local port=$((RANDOM % 2 == 0 ? 8080 : 8081))
  local b_url="http://localhost:$port"
  if (( RANDOM % 10 < 4 )); then
    printf '%s%s\n' "$b_url" "${static_paths[RANDOM % ${#static_paths[@]}]}"
  else
    printf '%s%s\n' "$b_url" "$(gen_dynamic_path)"
  fi
}

worker() {
  while :; do
    url=$(pick_url)
    method=${methods[RANDOM % ${#methods[@]}]}
    curl -sk -o /dev/null -X "$method" --max-time 2 "$url" || true
  done
}

b64url() {
  openssl base64 -A | tr '+/' '-_' | tr -d '='
}

# HS256 JWT allowed to publish on every topic, signed with the hub key.
mercure_jwt() {
  local header payload
  header=$(printf '{"alg":"HS256","typ":"JWT"}' | b64url)
  payload=$(printf '{"mercure":{"publish":["*"]}}' | b64url)
  printf '%s.%s.%s\n' "$header" "$payload" \
    "$(printf '%s.%s' "$header" "$payload" | openssl dgst -sha256 -hmac "$mercure_key" -binary | b64url)"
}

# Each subscriber holds an SSE connection for a while, then reconnects, so
# both mercure_subscribers_connected and mercure_subscribers_total move.
subscriber() {
  while :; do
    curl -sN -o /dev/null --max-time "$((10 + RANDOM % 20))" "$mercure_url?topic=demo" || true
  done
}

publisher() {
  local jwt
  jwt=$(mercure_jwt)
  while :; do
    curl -s -o /dev/null --max-time 2 -X POST "$mercure_url" \
      -H "Authorization: Bearer $jwt" -d topic=demo -d "data=$(date +%s)" || true
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

echo "Starting $WORKERS workers against http://localhost:8080 (Normal) and http://localhost:8081 (Workers)"
for ((i=0; i<WORKERS; i++)); do
  worker &
  pids+=("$!")
done

if [[ "$SUBSCRIBERS" -gt 0 ]]; then
  echo "Starting $SUBSCRIBERS Mercure subscribers and one publisher against $mercure_url"
  for ((i=0; i<SUBSCRIBERS; i++)); do
    subscriber &
    pids+=("$!")
  done
  publisher &
  pids+=("$!")
fi

if [[ "$DURATION" -gt 0 ]]; then
  sleep "$DURATION"
else
  wait
fi
