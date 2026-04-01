---
name: ember-json
description: "Use Ember's JSON output to programmatically inspect Caddy and FrankenPHP state from scripts, CI pipelines, or AI coding agents. Use this skill whenever someone wants to query Caddy metrics from the command line, parse ember output with jq, write a script that checks server health, detect 5xx errors programmatically, compare metrics before and after a deployment, use ember in a CI/CD pipeline, automate Caddy monitoring, or wait for Caddy readiness in a script. Also trigger when an AI agent needs to inspect the current state of a Caddy server — ember's JSON mode is the tool for that."
---

# Ember JSON & Scripting

Ember's `--json` flag turns the dashboard into a machine-readable data source. Instead of the interactive TUI, it outputs one JSON object per polling interval to stdout — perfect for scripts, CI pipelines, and AI agents that need to inspect Caddy/FrankenPHP state programmatically.

## Getting a snapshot

```bash
# Single snapshot, then exit
ember --json --once

# Streaming mode (one JSON line per second)
ember --json

# Custom interval
ember --json --interval 5s
```

Errors go to stderr, so stdout is always valid JSONL. This matters when piping to `jq` or other tools.

**Important caveat:** derived metrics (RPS, average latency, percentiles) are computed from the delta between two consecutive polls. With `--once`, there is no previous poll, so `derived` fields will be zero. Use streaming mode (`ember --json` without `--once`) if you need derived metrics.

## JSON schema

Each JSON line contains:

```json
{
  "threads": {
    "threadDebugStates": [...],
    "reservedThreadCount": 0
  },
  "metrics": {
    "totalThreads": 0,
    "busyThreads": 0,
    "queueDepth": 0,
    "workers": {},
    "httpRequestsTotal": 0,
    "httpRequestDurationSum": 0,
    "httpRequestDurationCount": 0,
    "httpRequestsInFlight": 0,
    "hasHttpMetrics": true,
    "hosts": {}
  },
  "process": {
    "pid": 12345,
    "cpuPercent": 2.5,
    "rss": 52428800,
    "createTime": 1710000000000,
    "uptime": 3600000000000
  },
  "fetchedAt": "2026-03-16T10:00:00Z",
  "errors": [],
  "derived": {
    "rps": 150.5,
    "avgTime": 12.3,
    "p50": 8.0,
    "p95": 25.0,
    "p99": 80.0
  },
  "hosts": [
    {
      "host": "example.com",
      "rps": 100.2,
      "avgTime": 10.5,
      "inFlight": 3,
      "p50": 7.0,
      "p90": 15.0,
      "p95": 22.0,
      "p99": 75.0,
      "statusCodes": { "200": 95.0, "404": 3.0, "500": 2.0 },
      "methodRates": { "GET": 80.0, "POST": 20.0 }
    }
  ]
}
```

### Field reference

| Field | Description |
|-------|-------------|
| `threads` | Raw FrankenPHP thread debug states. Empty object if Caddy-only (no FrankenPHP). |
| `metrics` | Raw Caddy and FrankenPHP metrics from the admin API. Includes `hasHttpMetrics` to confirm metrics are flowing. |
| `process` | Monitored process info: PID, CPU %, RSS in bytes, creation time (Unix ms), uptime (nanoseconds). |
| `fetchedAt` | Timestamp of this poll in RFC 3339 format. |
| `errors` | Array of error strings from this poll. Omitted when empty. |
| `derived` | Computed metrics: RPS, average response time (ms), error rate. Omitted on the first poll. |
| `derived.errorRate` | Middleware errors per second. Omitted when 0. |
| `derived.p50/p95/p99` | Request duration percentiles in milliseconds. Omitted when unavailable. |
| `hosts` | Per-host breakdown. Omitted when no host-level data exists. |
| `hosts[].statusCodes` | Map of HTTP status code to request rate (req/s). |
| `hosts[].methodRates` | Map of HTTP method to request rate (req/s). |
| `hosts[].errorRate` | Middleware errors/s for this host. Omitted when 0. |
| `hosts[].ttfbP50/P90/P95/P99` | Time-to-First-Byte percentiles in ms. Omitted when unavailable. |
| `hosts[].avgRequestSize` | Average request body size in bytes. Omitted when 0. |

## Common scripting patterns

### Inspect current state

```bash
# Overall RPS
ember --json --once | jq '.derived.rps'

# CPU usage
ember --json --once | jq '.process.cpuPercent'

# RSS in MB
ember --json --once | jq '.process.rss / 1048576'

# List all hosts
ember --json --once | jq -r '.hosts[].host'

# Per-host RPS
ember --json --once | jq -r '.hosts[] | "\(.host): \(.rps) rps"'
```

### Detect problems

```bash
# Check if any host has 5xx errors
ember --json --once | jq -e '.hosts[] | select(.statusCodes["500"] > 0)'

# Find hosts with high latency (P99 > 100ms)
ember --json --once | jq -r '.hosts[] | select(.p99 > 100) | "\(.host): P99=\(.p99)ms"'

# Check if error rate is above threshold
ember --json --once | jq -e '.derived.errorRate > 0.5'
```

### Monitor over time

```bash
# Watch RPS in real time
ember --json | jq -r '.derived.rps'

# Log to file at 5s intervals
ember --json --interval 5s >> ember.log

# Extract per-host 5xx rates continuously
ember --json | jq -r '.hosts[] | "\(.host): \(.statusCodes["500"] // 0)/s"'
```

## Health check: `ember status`

For a quick one-line check without parsing JSON:

```bash
ember status
# Caddy OK | 5 hosts | 450 rps | P99 12ms | CPU 3.2% | RSS 48MB | up 3d 2h
```

For machine-readable output:

```bash
ember status --json
```

Returns:

```json
{
  "status": "ok",
  "hosts": 5,
  "rps": 450,
  "p99": 12.3,
  "cpuPercent": 3.2,
  "rssBytes": 50331648,
  "uptime": "3d 2h",
  "frankenphp": { "busy": 8, "total": 20, "workers": 2 }
}
```

Exit code 0 means Caddy is reachable, 1 means unreachable. This makes `ember status` usable as a health gate in scripts.

## Readiness gate: `ember wait`

Block until Caddy's admin API is reachable:

```bash
ember wait --timeout 30s && echo "Caddy is up"
```

Useful in startup scripts and CI:

```bash
# Wait for Caddy after docker-compose up
docker compose up -d && ember wait --timeout 30s && ember status

# Silent mode (exit code only, no output)
ember wait -q --timeout 10s
```

Exit code 0 means Caddy is ready, 1 means timeout expired.

## Deployment validation: `ember diff`

Compare two JSON snapshots to detect performance regressions:

```bash
# Capture before
ember --json --once > before.json

# ... deploy, migrate, change config ...

# Capture after
ember --json --once > after.json

# Compare
ember diff before.json after.json
```

Sample output:

```
Global
    Requests             1720 -> 2023        +17.6%
    Avg (cumul.)       76.9ms -> 77.7ms      +1.0%
    Errors                  0 -> 0           =
    In-flight             3.0 -> 1.0         -66.7%

Per-host changes
  api
    In-flight             3.0 -> 1.0         -66.7%

No regressions detected
```

**Exit codes:**
- **0** — No regressions detected
- **1** — Regressions found (>10% degradation on latency, error rate, or CPU; >10% drop on RPS)

This makes `ember diff` a natural CI gate:

```bash
ember diff before.json after.json || exit 1
```

## CI pipeline example

```bash
#!/bin/bash
set -e

# Wait for Caddy to be ready
ember wait --timeout 30s

# Capture baseline
ember --json --once > before.json

# Deploy new version
./deploy.sh

# Wait for new version to stabilize
ember wait --timeout 30s
sleep 10

# Capture post-deploy state
ember --json --once > after.json

# Check for regressions
ember diff before.json after.json
```
