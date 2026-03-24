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
| `--slow-threshold` | int | `500` | Slow request threshold in milliseconds. Requests above this are highlighted yellow; above 2x are red. |
| `--frankenphp-pid` | int | `0` (auto) | FrankenPHP PID. Auto-detected if not set. |
| `--json` | bool | `false` | JSON output mode (streaming JSONL). See [JSON Output](json-output.md). |
| `--once` | bool | `false` | Output a single snapshot and exit. Requires `--json`. See [JSON Output](json-output.md). |
| `--expose` | string | _(none)_ | Start Prometheus metrics endpoint on this address (e.g. `:9191`). See [Prometheus Export](prometheus-export.md). |
| `--daemon` | bool | `false` | Headless mode: no TUI. Requires `--expose`. See [Prometheus Export](prometheus-export.md). |
| `--metrics-prefix` | string | _(none)_ | Prefix for exported Prometheus metric names. See [Prometheus Export](prometheus-export.md). |
| `--no-color` | bool | `false` | Disable colors |
| `--version` | | | Print version and exit |

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
ember status --addr http://prod:2019
ember status --interval 2s
```

The `--addr`, `--interval`, and `--frankenphp-pid` flags are available.

### `ember wait`

Blocks until the Caddy admin API is reachable, then exits with code 0. If `--timeout` is set and Caddy is still unreachable, exits with code 1.

**Examples:**

```bash
ember wait
ember wait --timeout 30s
ember wait --addr http://prod:2019 && ember status
docker compose up -d && ember wait && ./deploy.sh
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--timeout` | duration | `0` (forever) | Maximum time to wait |

The `--addr` and `--interval` flags are available.

## Keybindings

### Navigation

| Key | Action |
|-----|--------|
| `Up` / `Down` / `j` / `k` | Move cursor |
| `Enter` | Open detail panel |
| `Esc` | Close panel / clear filter / go back |
| `Tab` | Switch tab |
| `1` / `2` | Jump to tab |
| `Home` / `End` | Jump to first / last item |
| `PgUp` / `PgDn` | Page up / page down |

### Actions

| Key | Action | Context |
|-----|--------|---------|
| `s` / `S` | Cycle sort field forward / backward | List view |
| `p` | Pause / resume polling | Any view |
| `/` | Enter filter mode | List view |
| `g` | Toggle full-screen graphs | Any view |
| `r` | Restart workers | FrankenPHP tab only |
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
