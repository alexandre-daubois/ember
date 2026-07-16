#!/usr/bin/env bash
# Generate traffic against the local FrankenPHP stack so Ember's Logs and
# By-Route tabs have something to show.
#
#   WORKERS=N  ./load.sh     # N parallel curl loops (default 20)
#   DURATION=S ./load.sh     # stop after S seconds (default: run until Ctrl+C)
#
# Hits a mix of methods and endpoints so status codes span 200/404/500
# (index.php responds based on the requested path). Some routes embed UUIDs,
# numeric IDs and long hex strings so the By Route view's normalization
# (`:uuid`, `:id`, `:hash`) shows visible aggregation instead of one row per
# concrete value.

set -u

WORKERS=${WORKERS:-20}
DURATION=${DURATION:-0}

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
  if (( RANDOM % 10 < 4 )); then
    printf '%s%s\n' "$base_url" "${static_paths[RANDOM % ${#static_paths[@]}]}"
  else
    printf '%s%s\n' "$base_url" "$(gen_dynamic_path)"
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

echo "Starting $WORKERS workers against $base_url"
for ((i=0; i<WORKERS; i++)); do
  worker &
  pids+=("$!")
done

if [[ "$DURATION" -gt 0 ]]; then
  sleep "$DURATION"
else
  wait
fi
