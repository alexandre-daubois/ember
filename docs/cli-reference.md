# CLI Reference

## Synopsis

```
ember [flags]
```

## Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--addr` | string | `http://localhost:2019` | Caddy admin API address |
| `--interval` | duration | `1s` | Polling interval |
| `--timeout` | duration | `0` (none) | Global timeout. Applies to all modes and subcommands. 0 means no timeout. |
| `--slow-threshold` | int | `500` | Slow request threshold in milliseconds. Requests above this are highlighted yellow; above 2x are red. |
| `--frankenphp-pid` | int | `0` (auto) | FrankenPHP PID. Auto-detected if not set. |
| `--json` | bool | `false` | JSON output mode (streaming JSONL). See [JSON Output](json-output.md). |
| `--once` | bool | `false` | Output a single snapshot and exit. Requires `--json`. See [JSON Output](json-output.md). |
| `--expose` | string | _(none)_ | Start Prometheus metrics endpoint on this address (e.g. `:9191`). See [Prometheus Export](prometheus-export.md). |
| `--daemon` | bool | `false` | Headless mode: no TUI. Requires `--expose`. See [Prometheus Export](prometheus-export.md). |
| `--metrics-prefix` | string | _(none)_ | Prefix for exported Prometheus metric names. See [Prometheus Export](prometheus-export.md). |
| `--log-format` | string | `text` | Log format for daemon/json modes (`text` or `json`). JSON format is suitable for log aggregation systems. |
| `--ca-cert` | string | _(none)_ | Path to CA certificate for TLS verification |
| `--client-cert` | string | _(none)_ | Path to client certificate for mTLS |
| `--client-key` | string | _(none)_ | Path to client private key for mTLS |
| `--insecure` | bool | `false` | Skip TLS certificate verification |
| `--metrics-auth` | string | _(none)_ | Basic auth for the metrics endpoint (`user:password`). Requires `--expose`. See [Prometheus Export](prometheus-export.md). |
| `--no-color` | bool | `false` | Disable colors. Also enabled by the `NO_COLOR` env var (see [no-color.org](https://no-color.org/)). |
| `--version` | | | Print version and exit |

## Environment Variables

Some flags can be set via environment variables. Explicit flags always take precedence over environment variables.

| Variable | Flag | Example |
|----------|------|---------|
| `EMBER_ADDR` | `--addr` | `EMBER_ADDR=http://caddy:2019` |
| `EMBER_INTERVAL` | `--interval` | `EMBER_INTERVAL=5s` |
| `EMBER_EXPOSE` | `--expose` | `EMBER_EXPOSE=:9191` |
| `EMBER_METRICS_PREFIX` | `--metrics-prefix` | `EMBER_METRICS_PREFIX=myapp` |
| `EMBER_METRICS_AUTH` | `--metrics-auth` | `EMBER_METRICS_AUTH=admin:secret` |

This is especially useful in container deployments where flags are less convenient than environment variables. Using `EMBER_METRICS_AUTH` is recommended over the flag to avoid exposing credentials in `ps` output.

## Examples

```bash
# Default: connect to localhost:2019
ember

# Connect to a remote Caddy instance
ember --addr http://prod:2019

# Pipe-friendly JSON output
ember --json

# Single JSON snapshot and exit
ember --json --once

# JSON output at a slower rate
ember --json --interval 5s

# TUI with Prometheus metrics on port 9191
ember --expose :9191

# Headless metrics exporter (no TUI)
ember --expose :9191 --daemon

# Stricter slow-request highlighting
ember --slow-threshold 200

# Prefixed Prometheus metrics
ember --expose :9191 --metrics-prefix myapp

# Explicitly specify a FrankenPHP PID
ember --frankenphp-pid 42
```

## Subcommands

### `ember init`

Checks that Caddy is properly configured for Ember and offers to enable HTTP metrics via the admin API if they are missing. Does not modify any files on disk.

**What it checks:**
1. Admin API is reachable
2. HTTP servers are configured
3. HTTP metrics directive is enabled
4. FrankenPHP presence, threads, and workers
5. Metrics are actually flowing

**What it can do:**
- Enable the `metrics` directive via `POST /config/apps/http/metrics` (no Caddy restart needed)

**Examples:**

```bash
ember init
ember init --addr https://prod:2019 --ca-cert ca.pem
ember init -y    # skip confirmation prompts
ember init -yq   # skip prompts, suppress output
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-y`, `--yes` | bool | `false` | Skip confirmation prompts |
| `-q`, `--quiet` | bool | `false` | Suppress output (errors still reported via exit code) |

### `ember version`

Prints the current version. With `--check`, queries the GitHub Releases API to see if a newer version is available.

**Examples:**

```bash
ember version
ember version --check
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--check` | bool | `false` | Check for newer version on GitHub |

### `ember status`

One-line health check of Caddy. Performs two fetches separated by the polling interval to compute derived metrics, then prints a compact status line and exits.

Exit code 0 means Caddy is reachable, 1 means unreachable.

**Output format:**

```
Caddy OK | 5 hosts | 450 rps | P99 12ms | CPU 3.2% | RSS 48MB | up 3d 2h
```

With FrankenPHP:

```
Caddy OK | 5 hosts | 450 rps | P99 12ms | CPU 3.2% | RSS 48MB | up 3d 2h | FrankenPHP 8/20 busy | 2 workers
```

If Caddy is unreachable:

```
Caddy UNREACHABLE | http://localhost:2019
```

**Examples:**

```bash
ember status
ember status --json
ember status --json | jq .rps
ember status --addr http://prod:2019
ember status --interval 2s
```

With `--json`, the output is a JSON object:

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

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--json` | bool | `false` | Output status as JSON |

The `--addr`, `--interval`, and `--frankenphp-pid` flags are also available.

### `ember wait`

Blocks until the Caddy admin API is reachable, then exits with code 0. If `--timeout` is set and Caddy is still unreachable, exits with code 1.

**Examples:**

```bash
ember wait
ember wait --timeout 30s
ember wait -q --timeout 10s && ./deploy.sh
ember wait --addr http://prod:2019 && ember status
docker compose up -d && ember wait && ./deploy.sh
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-q`, `--quiet` | bool | `false` | Suppress output (exit code only) |

The `--addr`, `--interval`, and `--timeout` flags are also available.

### `ember diff`

Compares two JSON snapshots produced by `ember --json --once` and shows the deltas for key metrics. Useful for validating deployments and benchmarks.

Exit code 0 means no regressions detected, 1 means regressions found (>10% degradation on latency, error rate, or CPU; >10% drop on RPS).

**Examples:**

```bash
ember --json --once > before.json
# ... deploy or benchmark ...
ember --json --once > after.json
ember diff before.json after.json
```

**Sample output:**

```
Global
    Requests             1720 -> 2023        +17.6%
    Avg (cumul.)       76.9ms -> 77.7ms      +1.0%
    Errors                  0 -> 0           =
    In-flight             3.0 -> 1.0         -66.7%
    CPU                     0 -> 0           =
    RSS                39.5MB -> 39.5MB      =

Per-host changes

  api
    In-flight             3.0 -> 1.0         -66.7%

  app
    In-flight             1.0 -> 0           -100.0%

No regressions detected
```

## Keybindings

### Navigation

| Key | Action                                             |
|-----|----------------------------------------------------|
| `Up` / `Down` / `j` / `k` | Move cursor                                        |
| `Enter` / `Right` / `l` | Open detail panel / expand node (Caddy Config tab) |
| `Left` / `h` | Collapse node (Caddy Config tab)                   |
| `Esc` | Close panel / clear search / go back               |
| `Tab` / `Shift+Tab` | Switch tab                                         |
| `1` / `2` / `3` | Jump to tab                                        |
| `Home` / `End` | Jump to first / last item                          |
| `PgUp` / `PgDn` | Page up / page down                                |

### Actions

| Key | Action | Context |
|-----|--------|---------|
| `s` / `S` | Cycle sort field forward / backward | Caddy / FrankenPHP tabs |
| `p` | Pause / resume polling | Any view |
| `/` | Enter filter / search mode | Any tab |
| `e` / `E` | Expand / collapse all nodes | Config tab |
| `n` / `N` | Jump to next / previous search match | Config tab |
| `r` | Refresh config / restart workers | Config tab / FrankenPHP tab |
| `g` | Toggle full-screen graphs | Any view |
| `?` | Toggle help overlay | Any view |
| `q` | Quit | Any view |

## Shell Completions

### Bash

```bash
ember completion bash > /etc/bash_completion.d/ember
```

### Zsh

```bash
ember completion zsh > "${fpath[1]}/_ember"
```

### Fish

```bash
ember completion fish > ~/.config/fish/completions/ember.fish
```

## See Also

- [Getting Started](getting-started.md)
- [JSON Output](json-output.md)
- [Prometheus Export](prometheus-export.md)
