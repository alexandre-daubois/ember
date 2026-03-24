# Caddy Configuration

Ember reads data from the Caddy admin API. This page explains what your Caddyfile needs for Ember to work.

## Admin API

Ember connects to the Caddy admin API (default: `localhost:2019`). Make sure it's enabled:

```
{
    admin localhost:2019
}
```

> **Caution:** The admin API is unauthenticated by default. Do not expose it on a public interface. See [Caddy's admin API documentation](https://caddyserver.com/docs/caddyfile/options#admin) for authentication options.

## Metrics Directive

The `metrics` directive enables Prometheus-format metrics on the admin API. Without it, Ember cannot display HTTP traffic data.

```
{
    admin localhost:2019
    metrics
}
```

This exposes the following Caddy metrics at the `/metrics` admin endpoint:

| Metric | Description |
|--------|-------------|
| `caddy_http_requests_total` | Total HTTP requests (with host, method, status labels) |
| `caddy_http_request_duration_seconds` | Request duration histogram (with host labels and buckets) |
| `caddy_http_requests_in_flight` | Currently in-flight requests |
| `caddy_http_response_size_bytes` | Response body size |

Ember parses these metrics to compute per-host RPS, average latency, percentiles, status code rates, and more.

## FrankenPHP Detection

Ember automatically detects FrankenPHP by sending `GET /frankenphp/threads` to the admin API. If the endpoint responds with `200 OK`, the FrankenPHP tab is enabled.

If auto-detection does not work, you can specify the process ID manually:

```bash
ember --frankenphp-pid 12345
```

When no `--frankenphp-pid` is provided, Ember scans the process list for a FrankenPHP process. If none is found, it falls back to scanning for a Caddy process for CPU/RSS monitoring. When process scanning is unavailable, Ember derives CPU and RSS from the standard Go `process_*` Prometheus metrics exposed by Caddy's `/metrics` endpoint.

## Remote Caddy Instances

Use the `--addr` flag to point Ember to a remote Caddy admin API:

```bash
ember --addr http://10.0.0.5:2019
```

The admin API must be reachable from wherever Ember runs.

## TLS and mTLS

When the Caddy admin API is served over HTTPS (recommended for production), Ember needs TLS configuration to connect.

### Custom CA Certificate

If your Caddy instance uses a certificate signed by a private CA (e.g., internal PKI, self-signed):

```bash
ember --addr https://caddy.internal:2019 --ca-cert /path/to/ca.pem
```

### Mutual TLS (mTLS)

Caddy can require clients to present a certificate. Pass both a client certificate and its private key:

```bash
ember --addr https://caddy.internal:2019 \
  --ca-cert /path/to/ca.pem \
  --client-cert /path/to/client.pem \
  --client-key /path/to/client-key.pem
```

### Skip TLS Verification

For development or debugging only:

```bash
ember --addr https://localhost:2019 --insecure
```

> **Warning:** `--insecure` disables all certificate verification. Do not use in production.

### TLS Flag Reference

| Flag | Description |
|------|-------------|
| `--ca-cert` | Path to a PEM-encoded CA certificate to trust |
| `--client-cert` | Path to a PEM-encoded client certificate (for mTLS) |
| `--client-key` | Path to the private key matching `--client-cert` |
| `--insecure` | Skip all TLS verification |

These flags are global and work with all modes (TUI, JSON, daemon) and subcommands (`status`, `wait`).

## Minimal Caddyfile

A complete working example:

```
{
    admin localhost:2019
    metrics
}

example.com {
    respond "Hello, world!" 200
}
```

## See Also

- [Getting Started](getting-started.md)
- [Caddy Dashboard](caddy-dashboard.md)
- [FrankenPHP Dashboard](frankenphp-dashboard.md)
