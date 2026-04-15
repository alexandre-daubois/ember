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
| **In-flight** | Currently in-progress requests |
| **2xx/s** | Successful response rate |
| **4xx/s** | Client error rate (displayed in yellow) |
| **5xx/s** | Server error rate (displayed in red) |

> **Note:** If you see a host named `*`, it means Caddy metrics lack per-host labels. The `*` row aggregates all traffic. Make sure your Caddyfile routes include host matchers for per-host breakdown.

## Latency Percentiles

P50, P90, P95, and P99 are computed from Prometheus histogram buckets (`caddy_http_request_duration_seconds`). These percentiles are available in the host detail panel (press `Enter` on a host).

> **Tip:** If percentiles don't appear in the detail panel, verify that the `metrics` directive is present in your Caddyfile global block. See [Caddy Configuration](caddy-configuration.md).

## Sorting

Press `s` to cycle the sort field forward, `S` to cycle backward.

The current sort field is shown in the bottom status bar.

## Filtering

Press `/` to enter filter mode. Type a hostname pattern to filter the table. Press `Esc` to clear the filter and return to the full list.

## Host Detail Panel

Press `Enter` on a host to open the detail panel:

- **Traffic**: RPS, in-flight requests, total request count, error rate (when > 0, displayed in red)
- **Latency**: P50, P90, P95, P99 (when available), average response time
- **TTFB**: Time-to-First-Byte percentiles (P50, P90, P95, P99), computed from `caddy_http_response_duration_seconds`
- **Waterfall (P50)**: stacked bar showing TTFB and transfer time breakdown. Requires latency P50 percentiles and TTFB P50.
- **Status Codes**: Individual status codes with their rates
- **HTTP Methods**: Request rates and percentage of total per method (GET, POST, etc.)
- **Transfer Size**: Average request body size and average response body size

Press `Esc` to close the detail panel.

## Config Inspector

The **Caddy Config** tab (accessible via `Tab` or `2`) fetches the live Caddy configuration via the admin API (`GET /config/`) and displays it as an interactive collapsible JSON tree. The config is refreshed by pressing `r`.

Navigation in the Caddy Config tab:

| Key | Action |
|-----|--------|
| `Up` / `Down` / `j` / `k` | Move cursor |
| `Enter` / `Right` / `l` | Expand node |
| `Left` / `h` | Collapse node or jump to parent |
| `/` | Search within config (keys and values) |
| `n` / `N` | Jump to next / previous search match |
| `e` | Expand all nodes |
| `E` | Collapse all nodes |
| `r` | Refresh config from Caddy |

## Upstreams

The **Upstreams** tab appears automatically when Caddy exposes `caddy_reverse_proxy_upstreams_healthy` metrics, which happens when at least one `reverse_proxy` handler is configured.

The table shows one row per upstream:

| Column | Description |
|--------|-------------|
| **Upstream** | Upstream address (host:port) |
| **Check** | Active health check URI and interval (e.g. `/health @5s`), extracted from Caddy config |
| **LB** | Load balancing policy (e.g. `round_robin`, `least_conn`), extracted from Caddy config |
| **Health** | Health status: `● healthy` or `○ down` |
| **Down** | Duration since the upstream went down (e.g. `5s`, `2m30s`, `1h5m`) |

A `!` suffix on the health status indicates a state change since the previous poll (e.g. an upstream just went down or recovered).

The Check and LB columns are populated from the Caddy config when the tab first appears. Press `r` to refresh the config data.

Press `s`/`S` to sort by address or health status. Press `/` to filter by address or handler name.

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
