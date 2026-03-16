# Caddy Dashboard

The Caddy tab is always available. It displays per-host HTTP traffic metrics collected from the Caddy Prometheus endpoint.

## Host Traffic Table

The main view shows a table with one row per host:

| Column | Description |
|--------|-------------|
| **Host** | Hostname from the Caddy configuration |
| **RPS** | Requests per second |
| **Sparkline** | Miniature RPS trend (last 8 samples) |
| **Avg** | Average response time in milliseconds |
| **P90** | 90th percentile latency |
| **P95** | 95th percentile latency |
| **P99** | 99th percentile latency |
| **In-flight** | Currently in-progress requests |
| **2xx/s** | Successful response rate |
| **4xx/s** | Client error rate (displayed in yellow) |
| **5xx/s** | Server error rate (displayed in red) |

> **Note:** If you see a host named `*`, it means Caddy metrics lack per-host labels. The `*` row aggregates all traffic. Make sure your Caddyfile routes include host matchers for per-host breakdown.

## Latency Percentiles

P50, P90, P95, and P99 are computed from Prometheus histogram buckets (`caddy_http_request_duration_seconds`). These percentiles appear per host in the traffic table and in the host detail panel.

> **Tip:** If percentile columns are empty, verify that the `metrics` directive is present in your Caddyfile global block. See [Caddy Configuration](caddy-configuration.md).

## Sorting

Press `s` to cycle the sort field forward, `S` to cycle backward. Available sort fields:

`host` → `rps` → `avg` → `p90` → `p95` → `p99` → `in-flight` → `2xx` → `4xx` → `5xx`

The current sort field is shown in the bottom status bar.

## Filtering

Press `/` to enter filter mode. Type a hostname pattern to filter the table. Press `Esc` to clear the filter and return to the full list.

## Host Detail Panel

Press `Enter` on a host to open the detail panel:

- **Traffic**: RPS, in-flight requests, total request count
- **Latency**: P50, P90, P95, P99 (when available), average response time
- **Status Codes**: Individual status codes with their rates
- **HTTP Methods**: Request rates and percentage of total per method (GET, POST, etc.)
- **Response Size**: Average response body size

Press `Esc` to close the detail panel.

## Graphs

Press `g` to toggle full-screen graphs showing:

- **CPU %**: Process CPU usage over time
- **RPS**: Requests per second over time
- **RSS**: Resident memory over time

Graphs display the last 300 samples. Press `g` or `Esc` to return to the table view.

## See Also

- [FrankenPHP Dashboard](frankenphp-dashboard.md)
- [Caddy Configuration](caddy-configuration.md)
- [CLI Reference](cli-reference.md)
