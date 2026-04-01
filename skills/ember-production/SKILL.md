---
name: ember-production
description: "Deploy Ember in production environments with daemon mode, Prometheus metrics export, and Docker. Use this skill whenever someone asks about running ember headless, exporting Prometheus metrics, setting up ember in Docker or docker-compose, integrating ember with Grafana, configuring health checks, or any production deployment of Caddy/FrankenPHP monitoring. For JSON output, scripting, and CI pipelines, see the ember-json skill instead."
---

# Ember Production Deployment

This skill covers running Ember in production: headless daemon mode, Prometheus metrics export, and Docker deployment. For JSON output, scripting, CI integration, and `ember diff`/`ember wait`, see the **ember-json** skill.

## Daemon Mode

The `--daemon` flag disables the TUI and runs Ember as a background metrics collector. It requires `--expose` to serve a Prometheus endpoint.

```bash
ember --expose :9191 --daemon
```

This starts an HTTP server with:
- `/metrics` — Prometheus text format (v0.0.4)
- `/healthz` — JSON health check

### Combined with the TUI

You can also keep the TUI while exposing metrics:

```bash
ember --expose :9191
```

### Structured logging

For log aggregation (ELK, Loki, etc.), use JSON log format:

```bash
ember --expose :9191 --daemon --log-format json
```

Logs go to stderr. When Ember loses Caddy connectivity, it logs one error immediately then suppresses duplicates for 30 seconds to avoid flooding. A `"fetch recovered"` message is logged when connectivity returns.

### Signals (Unix only)

| Signal | Effect |
|--------|--------|
| `SIGUSR1` | Dump full state snapshot to stderr as JSON (for debugging) |
| `SIGHUP` | Reload TLS certificates from disk without restarting |

```bash
# Debug dump
kill -USR1 $(pgrep ember)

# Rotate certificates
kill -HUP $(pgrep ember)
```

## Prometheus Metrics

### Exported metrics

**Caddy host metrics:**

| Metric | Type | Labels |
|--------|------|--------|
| `ember_host_rps` | gauge | `host` |
| `ember_host_latency_avg_milliseconds` | gauge | `host` |
| `ember_host_latency_milliseconds` | gauge | `host`, `quantile` (0.5, 0.9, 0.95, 0.99) |
| `ember_host_inflight` | gauge | `host` |
| `ember_host_status_rate` | gauge | `host`, `class` (2xx, 3xx, 4xx, 5xx) |
| `ember_host_error_rate` | gauge | `host` |

**FrankenPHP metrics:**

| Metric | Type | Labels |
|--------|------|--------|
| `frankenphp_threads_total` | gauge | `state` (busy, idle, other) |
| `frankenphp_thread_memory_bytes` | gauge | `index` |
| `frankenphp_worker_crashes_total` | counter | `worker` |
| `frankenphp_worker_restarts_total` | counter | `worker` |
| `frankenphp_worker_queue_depth` | gauge | `worker` |
| `frankenphp_worker_requests_total` | counter | `worker` |
| `frankenphp_request_duration_milliseconds` | gauge | `quantile` (0.5, 0.95, 0.99) |

**Process metrics:**

| Metric | Type |
|--------|------|
| `process_cpu_percent` | gauge |
| `process_rss_bytes` | gauge |

### Custom metric prefix

Add a prefix to all metric names to avoid collisions:

```bash
ember --expose :9191 --metrics-prefix myapp
```

This turns `ember_host_rps` into `myapp_ember_host_rps`, `frankenphp_threads_total` into `myapp_frankenphp_threads_total`, etc.

### Authentication

Protect the metrics endpoint with HTTP Basic Auth:

```bash
ember --expose :9191 --daemon --metrics-auth admin:secret
```

Prefer the environment variable to avoid exposing credentials in `ps` output:

```bash
export EMBER_METRICS_AUTH=admin:secret
ember --expose :9191 --daemon
```

### Prometheus scrape configuration

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

### Health endpoint

`GET /healthz` returns JSON:

- **200 OK**: `{"status": "ok", "last_fetch": "...", "age_seconds": 1.2}` — data is fresh
- **503**: `{"status": "stale", ...}` — data older than 3x the polling interval (minimum 5s)
- **503**: `{"status": "no data yet"}` — no data collected yet

Use `/healthz` as a Kubernetes liveness probe to detect when Ember loses contact with Caddy.

## Docker Deployment

The Ember image (`ghcr.io/alexandre-daubois/ember`) is built from scratch: no OS, no shell, just the static binary and CA certificates.

Default behavior: `--daemon --expose :9191`

### Host network (simplest)

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember
```

### Custom address

```bash
docker run --rm ghcr.io/alexandre-daubois/ember \
  --daemon --expose :9191 --addr http://caddy:2019
```

### Docker Compose (sidecar pattern)

```yaml
services:
  caddy:
    image: caddy:latest
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile

  ember:
    image: ghcr.io/alexandre-daubois/ember
    network_mode: "service:caddy"
    depends_on:
      - caddy
```

With `network_mode: "service:caddy"`, Ember shares Caddy's network namespace and can reach `localhost:2019` directly.

### Custom flags in Docker

Override the default CMD by appending flags:

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember \
  --daemon --expose :9191 --interval 2s --metrics-prefix myapp
```

The image has no shell — use `docker logs` to read Ember's stderr output.

## Environment Variables

All main flags have environment variable equivalents — convenient for containers where flags are less practical:

| Variable | Flag |
|----------|------|
| `EMBER_ADDR` | `--addr` |
| `EMBER_INTERVAL` | `--interval` |
| `EMBER_EXPOSE` | `--expose` |
| `EMBER_METRICS_PREFIX` | `--metrics-prefix` |
| `EMBER_METRICS_AUTH` | `--metrics-auth` |

`EMBER_METRICS_AUTH` is recommended over the flag to avoid leaking credentials in `ps` output.
