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

### State Dump via SIGUSR1

Send `SIGUSR1` to a running daemon to dump the full state snapshot to stderr as JSON. This is useful for debugging without interrupting the process:

```bash
kill -USR1 $(pgrep ember)
```

The dump includes threads, metrics, process info, and derived metrics. Not available on Windows.

## Exported Metrics

### FrankenPHP Thread Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `frankenphp_threads_total` | gauge | `state` (`busy`, `idle`, `other`) | Number of threads by state |
| `frankenphp_thread_memory_bytes` | gauge | `index` | Memory usage per thread (only emitted for threads with memory > 0) |

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
| `frankenphp_request_duration_milliseconds` | gauge | `quantile` (`0.5`, `0.95`, `0.99`) | Request duration percentiles |

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

## Custom Metric Prefix

Use `--metrics-prefix` to add a prefix to all metric names:

```bash
ember --expose :9191 --metrics-prefix myapp
```

This turns `frankenphp_threads_total` into `myapp_frankenphp_threads_total`, `ember_host_rps` into `myapp_ember_host_rps`, and so on.

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
