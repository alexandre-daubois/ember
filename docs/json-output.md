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
    "ThreadDebugStates": [...],
    "ReservedThreadCount": 0
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
| `derived` | Computed metrics: RPS, average response time, percentiles (omitted on first poll) |
| `derived.p50/p95/p99` | Request duration percentiles in ms (omitted when unavailable) |
| `hosts` | Per-host breakdown (omitted when no host-level data) |
| `hosts[].statusCodes` | Status code → rate (req/s) |
| `hosts[].methodRates` | HTTP method → rate (req/s) |

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

## See Also

- [CLI Reference](cli-reference.md)
- [Prometheus Export](prometheus-export.md)
