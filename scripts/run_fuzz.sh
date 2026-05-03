#!/usr/bin/env bash
# Run every Go fuzz target in this repo for FUZZ_TIME (default 30s) each.
# Fails on the first crash. Crash artifacts are under each package's
# testdata/fuzz/<FuzzTarget>/.

set -euo pipefail

FUZZ_TIME="${1:-30s}"

# package : FuzzFunc pairs
TARGETS=(
  "./internal/ui FuzzParseJSONTree"
  "./internal/app FuzzIsLocalAdminAddr"
  "./internal/fetcher FuzzDecodeThreadsResponse"
  "./internal/fetcher FuzzExtractListenPorts"
  "./internal/fetcher FuzzParseLogLine"
  "./pkg/metrics FuzzParsePrometheus"
)

fail=0
for t in "${TARGETS[@]}"; do
  pkg="${t% *}"
  fn="${t#* }"
  echo "=== ${pkg} :: ${fn} (${FUZZ_TIME}) ==="
  if ! go test "${pkg}" -run "^${fn}$" -fuzz "^${fn}$" -fuzztime "${FUZZ_TIME}"; then
    echo "::error::fuzz target ${fn} (${pkg}) failed"
    fail=1
  fi
done

exit "${fail}"
