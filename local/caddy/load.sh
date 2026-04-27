#!/usr/bin/env bash
# Generate traffic against the local Caddy stack so Ember's Logs, Hosts,
# Upstreams and By-Route tabs have something to show.
#
#   WORKERS=N  ./load.sh     # N parallel curl loops (default 20)
#   DURATION=S ./load.sh     # stop after S seconds (default: run until Ctrl+C)
#
# Hits a mix of hosts, methods, and endpoints so status codes span 200/404/500
# and the reverse-proxied app.localhost exercises the three whoami upstreams.
# Some routes embed UUIDs, numeric IDs and long hex strings so the By Route
# view's normalization (`:uuid`, `:id`, `:hash`) shows visible aggregation
# instead of one row per concrete value.

set -u

WORKERS=${WORKERS:-20}
DURATION=${DURATION:-0}

static_urls=(
  "https://app.localhost/"
  "https://app.localhost/deep/path"
  "https://app.localhost/api/users"
  "https://api.localhost/"
  "https://api.localhost/health"
  "https://api.localhost/notfound"
  "https://api.localhost/error"
  "https://static.localhost/"
  "https://static.localhost/assets/logo.png"
)

methods=(GET GET GET GET GET POST PUT DELETE HEAD)

command -v curl >/dev/null 2>&1 || { echo >&2 "curl is required"; exit 1; }
command -v uuidgen >/dev/null 2>&1 || { echo >&2 "uuidgen is required (it is shipped with macOS and util-linux)"; exit 1; }
command -v openssl >/dev/null 2>&1 || { echo >&2 "openssl is required (used for hex hashes)"; exit 1; }

gen_dynamic_url() {
  case $((RANDOM % 4)) in
    0) printf 'https://api.localhost/users/%s\n' "$(uuidgen)" ;;
    1) printf 'https://api.localhost/orders/%d\n' "$((RANDOM % 100000))" ;;
    2) printf 'https://api.localhost/sessions/%s\n' "$(openssl rand -hex 16)" ;;
    3) printf 'https://api.localhost/artifacts/%s\n' "$(openssl rand -hex 32)" ;;
  esac
}

pick_url() {
  if (( RANDOM % 10 < 4 )); then
    printf '%s\n' "${static_urls[RANDOM % ${#static_urls[@]}]}"
  else
    gen_dynamic_url
  fi
}

worker() {
  while :; do
    url=$(pick_url)
    method=${methods[RANDOM % ${#methods[@]}]}
    curl -sk -o /dev/null -X "$method" --max-time 2 "$url" || true
  done
}

pids=()
cleanup() {
  trap - INT TERM EXIT
  kill "${pids[@]}" 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup INT TERM EXIT

echo "Starting $WORKERS workers against https://{app,api,static}.localhost"
for ((i=0; i<WORKERS; i++)); do
  worker &
  pids+=("$!")
done

if [[ "$DURATION" -gt 0 ]]; then
  sleep "$DURATION"
else
  wait
fi
