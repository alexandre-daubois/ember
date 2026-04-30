# Prometheus Export

Ember can expose Caddy and FrankenPHP metrics in Prometheus format via an HTTP endpoint. This works alongside the TUI or in headless daemon mode.

## Enabling the Metrics Endpoint

```bash
# TUI + metrics endpoint on port 9191
ember --expose :9191

# Headless mode (no TUI)
ember --expose :9191 --daemon
```

This starts an HTTP server with two endpoints:

- `/metrics`: Prometheus text format (v0.0.4)
- `/healthz`: JSON health check

## Daemon Mode

The `--daemon` flag disables the TUI and requires `--expose`. Ember polls the Caddy admin API at the configured interval and serves metrics. This is ideal for sidecar deployments, CI environments, or any context where a terminal is not available.

```bash
ember --expose :9191 --daemon --addr http://caddy:2019
```

Logs are written to stderr. Use `--log-format json` for structured JSON logs suitable for log aggregation:

```bash
ember --expose :9191 --daemon --log-format json
```

### Error Throttling

When the daemon cannot reach Caddy, it logs one error message immediately, then suppresses repeated errors for 30 seconds to avoid flooding logs. The suppressed count is included in the next log line. When connectivity is restored, a `"fetch recovered"` message is logged.

### State Dump via SIGUSR1

Send `SIGUSR1` to a running daemon to dump the full state snapshot to stderr as JSON. This is useful for debugging without interrupting the process:

```bash
kill -USR1 $(pgrep ember)
```

The dump includes threads, metrics, process info, and derived metrics. Not available on Windows.

### TLS Certificate Reload via SIGHUP

Send `SIGHUP` to a running daemon to reload TLS certificates from disk without restarting:

```bash
kill -HUP $(pgrep ember)
```

This re-reads `--ca-cert`, `--client-cert`, and `--client-key` files and applies the new configuration. Useful for certificate rotation in long-running deployments. Not available on Windows.

## Exported Metrics

### FrankenPHP Thread Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `frankenphp_threads_total` | gauge | `state` (`busy`, `idle`, `other`) | Number of threads by state |
| `frankenphp_thread_memory_bytes` | gauge | `index` | Memory usage per thread (only emitted for threads with memory > 0). Requires FrankenPHP 1.12.2+. |

### FrankenPHP Worker Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `frankenphp_worker_crashes_total` | counter | `worker` | Total worker crashes |
| `frankenphp_worker_restarts_total` | counter | `worker` | Total worker restarts |
| `frankenphp_worker_queue_depth` | gauge | `worker` | Requests waiting in queue |
| `frankenphp_worker_requests_total` | counter | `worker` | Total requests processed |

### FrankenPHP Request Duration

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `frankenphp_request_duration_milliseconds` | gauge | `quantile` (`0.5`, `0.9`, `0.95`, `0.99`) | Request duration percentiles |

### Caddy Host Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ember_host_rps` | gauge | `host` | Requests per second |
| `ember_host_latency_avg_milliseconds` | gauge | `host` | Average response time |
| `ember_host_latency_milliseconds` | gauge | `host`, `quantile` (`0.5`, `0.9`, `0.95`, `0.99`) | Latency percentiles |
| `ember_host_inflight` | gauge | `host` | In-flight requests |
| `ember_host_status_rate` | gauge | `host`, `class` (`2xx`, `3xx`, `4xx`, `5xx`) | Request rate by status class |
| `ember_host_error_rate` | gauge | `host` | Middleware error rate (handler-level errors, distinct from HTTP status codes) |

### Process Metrics

| Metric | Type | Description |
|--------|------|-------------|
| `process_cpu_percent` | gauge | CPU usage of the monitored process |
| `process_rss_bytes` | gauge | Resident set size of the monitored process |

### Ember Self-Metrics

Ember emits its own scrape metrics so operators can observe the observer. They appear on `/metrics` whenever `--expose` is set, alongside the Caddy and FrankenPHP metrics.

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `ember_build_info` | gauge | `version`, `goversion` | Constant `1`, exposes the build identifiers |
| `ember_scrape_total` | counter | `stage` (`threads`, `metrics`, `process`) | Scrape attempts per sub-fetch (success + error) |
| `ember_scrape_errors_total` | counter | `stage` | Failed scrape attempts per sub-fetch |
| `ember_scrape_duration_seconds` | gauge | `stage` | Duration of the last scrape attempt, in seconds |
| `ember_last_successful_scrape_timestamp_seconds` | gauge | `stage` | Unix timestamp of the last success per stage; `0` means never |

Common alerts:

- `rate(ember_scrape_errors_total[5m]) > 0`: Ember is failing to talk to Caddy.
- `time() - ember_last_successful_scrape_timestamp_seconds{stage="metrics"} > 60`: no fresh metrics for over a minute.
- `ember_scrape_duration_seconds{stage="metrics"} > 1`: scrape latency degraded.

## Multi-instance label

When `--addr` is supplied more than once (only valid in `--daemon` and `--json` modes), every metric above gains an `ember_instance="<name>"` label. The instance name is either the explicit alias from `name=url`, or a slug derived from the host (`web1.fr` -> `web1_fr`). With a single instance no extra label is emitted, so existing dashboards and alerts keep working unchanged.

`ember_build_info` is the only metric that stays unlabelled even in multi-instance mode: there is one Ember binary regardless of how many Caddy instances it polls.

Example scrape config that promotes `ember_instance` to a regular Prometheus target label so PromQL filters look natural:

```yaml
scrape_configs:
  - job_name: ember
    scrape_interval: 5s
    static_configs:
      - targets: ["ember:9191"]
    metric_relabel_configs:
      - source_labels: [ember_instance]
        target_label: instance
        action: replace
```

## Custom Metric Prefix

Use `--metrics-prefix` to add a prefix to all metric names:

```bash
ember --expose :9191 --metrics-prefix myapp
```

This turns `frankenphp_threads_total` into `myapp_frankenphp_threads_total`, `ember_host_rps` into `myapp_ember_host_rps`, and so on.

The prefix must be a legal Prometheus metric name segment: letters, digits and underscores, not starting with a digit. Hyphens, dots and colons are rejected at startup so invalid names never reach a scraper.

## Health Endpoint

`GET /healthz` returns a JSON response indicating whether Ember is collecting data successfully:

**200 OK**: Data is fresh:

```json
{ "status": "ok", "last_fetch": "2026-03-16T10:00:00Z", "age_seconds": 1.2 }
```

**503 Service Unavailable**: Data is stale (older than 3x the polling interval, minimum 5 seconds):

```json
{ "status": "stale", "last_fetch": "2026-03-16T09:58:00Z", "age_seconds": 120 }
```

**503 Service Unavailable**: No data collected yet:

```json
{ "status": "no data yet" }
```

In multi-instance mode the body switches to an aggregated form. The top-level `status` is the worst across instances (`ok` < `stale` < `no data yet`). The endpoint returns `200` only when every instance is `ok`:

```json
{
  "status": "stale",
  "instances": [
    { "name": "web1", "addr": "https://web1.fr", "status": "ok",    "last_fetch": "2026-03-16T10:00:00Z", "age_seconds": 1.2 },
    { "name": "web2", "addr": "https://web2.fr", "status": "stale", "last_fetch": "2026-03-16T09:58:00Z", "age_seconds": 120 }
  ]
}
```

> **Tip:** Use `/healthz` as a Kubernetes liveness probe to detect when Ember loses contact with Caddy.

## Authentication

Use `--metrics-auth` to protect the `/metrics` and `/healthz` endpoints with HTTP Basic Authentication:

```bash
ember --expose :9191 --daemon --metrics-auth admin:secret
```

When enabled, all requests to the metrics server must include valid credentials. Unauthenticated requests receive a `401 Unauthorized` response.

## Prometheus Scrape Configuration

Minimal `prometheus.yml` snippet:

```yaml
scrape_configs:
  - job_name: ember
    scrape_interval: 5s
    static_configs:
      - targets: ["localhost:9191"]
```

With authentication:

```yaml
scrape_configs:
  - job_name: ember
    scrape_interval: 5s
    basic_auth:
      username: admin
      password: secret
    static_configs:
      - targets: ["localhost:9191"]
```

## See Also

- [CLI Reference](cli-reference.md)
- [Docker](docker.md)
- [Caddy Configuration](caddy-configuration.md)
