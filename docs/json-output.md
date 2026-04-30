# JSON Output

The `--json` flag switches Ember to a streaming JSONL (JSON Lines) mode. One JSON object is printed to stdout per polling interval, making it easy to pipe into other tools.

```bash
ember --json
```

Errors are printed to stderr, so stdout always contains valid JSONL.

## Output Schema

Each line is a JSON object with the following fields:

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

### Field Reference

| Field | Description |
|-------|-------------|
| `threads` | Raw FrankenPHP thread debug states (empty if Caddy-only) |
| `metrics` | Raw Caddy and FrankenPHP metrics from the admin API |
| `process` | Monitored process info: PID, CPU %, RSS (bytes), uptime |
| `fetchedAt` | Timestamp of this poll (RFC 3339) |
| `errors` | Array of error strings from this poll (omitted if empty) |
| `derived` | Computed metrics: RPS, average response time, error rate, percentiles (omitted on first poll) |
| `derived.errorRate` | Middleware errors per second (omitted when 0) |
| `derived.p50/p95/p99` | Request duration percentiles in ms (omitted when unavailable) |
| `hosts` | Per-host breakdown (omitted when no host-level data) |
| `hosts[].errorRate` | Middleware errors per second for this host (omitted when 0) |
| `hosts[].ttfbP50/P90/P95/P99` | Time-to-First-Byte percentiles in ms (omitted when unavailable) |
| `hosts[].statusCodes` | Status code → rate (req/s) |
| `hosts[].methodRates` | HTTP method → rate (req/s) |
| `hosts[].avgRequestSize` | Average request body size in bytes (omitted when 0) |
| `upstreams` | Reverse proxy upstream health (omitted when no `reverse_proxy` is configured) |
| `upstreams[].address` | Upstream address (host:port) |
| `upstreams[].handler` | Reverse proxy handler name (omitted when Caddy doesn't expose the label) |
| `upstreams[].healthy` | Whether the upstream is healthy |
| `upstreams[].healthChanged` | Whether health status changed since last poll (omitted when false) |

## Single Snapshot

The `--once` flag outputs a single JSON object and exits, without needing to kill the process:

```bash
ember --json --once
```

This is useful for scripting, CI pipelines, or feeding data into other tools:

```bash
# Get current state as JSON
ember --json --once | jq '.process.cpuPercent'

# Check if any host has 5xx errors
ember --json --once | jq -e '.hosts[] | select(.statusCodes["500"] > 0)'
```

> **Note:** Derived metrics (RPS, average latency, percentiles) require two data points and will be zero on a single snapshot since there is no previous poll to compute a delta.

## Scripting Examples

### Watch RPS in real time

```bash
ember --json | jq -r '.derived.rps'
```

### Extract per-host 5xx rates

```bash
ember --json | jq -r '.hosts[] | "\(.host): \(.statusCodes["500"] // 0)/s"'
```

### Log to file with timestamps

```bash
ember --json --interval 5s >> ember.log
```

> **Tip:** Combine `--json` with `--interval` to control the output rate. For example, `ember --json --interval 5s` outputs one line every 5 seconds.

## Multi-instance output

When `--addr` is repeated, Ember polls every instance per tick and emits one JSONL line per instance, prefixed with an `instance` field:

```bash
ember --json \
  --addr web1=https://web1.fr \
  --addr web2=https://web2.fr
```

```jsonl
{"instance":"web1","threads":{...},"metrics":{...},"hosts":[...],"fetchedAt":"..."}
{"instance":"web2","threads":{...},"metrics":{...},"hosts":[...],"fetchedAt":"..."}
{"instance":"web1","threads":{...},"metrics":{...},"hosts":[...],"fetchedAt":"..."}
```

Instances are emitted in alphabetical order by name within a tick, so downstream consumers can rely on deterministic grouping. With `--once`, exactly one line per instance is produced before exit.

When only a single `--addr` is provided, the `instance` field is omitted: the output is byte-for-byte identical to the pre-multi-instance format.

Multi-instance JSONL snapshots can be diffed with [`ember diff`](cli-reference.md#ember-diff): lines are grouped by `instance`, the last entry per instance wins, and one diff block is emitted per alias.

## See Also

- [CLI Reference](cli-reference.md)
- [Prometheus Export](prometheus-export.md)
