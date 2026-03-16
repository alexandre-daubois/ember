# Ember - Real-time TUI dashboard for Caddy & FrankenPHP

[![CI](https://github.com/alexandre-daubois/ember/actions/workflows/ci.yml/badge.svg)](https://github.com/alexandre-daubois/ember/actions/workflows/ci.yml)
[![codecov](https://codecov.io/github/alexandre-daubois/ember/graph/badge.svg?token=3BG1TUO91L)](https://codecov.io/github/alexandre-daubois/ember)
[![Go Report Card](https://goreportcard.com/badge/github.com/alexandre-daubois/ember)](https://goreportcard.com/report/github.com/alexandre-daubois/ember)
[![Go Reference](https://pkg.go.dev/badge/github.com/alexandre-daubois/ember.svg)](https://pkg.go.dev/github.com/alexandre-daubois/ember)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Monitor your Caddy server in real time: per-host traffic, latency percentiles, status codes, and more. When FrankenPHP is detected, unlock per-thread introspection, worker management, and memory tracking.

![ember screenshot](https://github.com/alexandre-daubois/ember/blob/main/assets/img.png?raw=true)

## Features

### Caddy dashboard (always available)

- Per-host traffic table with RPS, per-host RPS sparklines, average response time, and in-flight requests
- Latency percentiles (P50, P90, P95, P99) from Prometheus histogram buckets
- Status code breakdown (2xx/s, 4xx/s, 5xx/s) per host
- Sorting and live filtering by hostname
- Live sparkline graphs (CPU, RPS, RSS)
- JSON output mode for scripting (includes per-host metrics)

### FrankenPHP dashboard (when detected)

- Per-thread introspection (URI, HTTP method, duration, memory)
- Worker queue depth and busy thread tracking
- Full-screen graphs (CPU, RPS, RSS, queue depth, busy threads)
- Graceful worker restart from the TUI
- Detail panel with per-thread info and memory sparkline trend
- Memory delta indicators (↑/↓) per thread between polls

### Cloud Ready with Prometheus Export & Daemon Mode

- `--expose=:9191` starts a `/metrics` endpoint exposing Caddy and FrankenPHP metrics in Prometheus format
- `--daemon` runs headless (no TUI)
- `/healthz` endpoint returns `200 OK` when data is fresh, `503` when stale or unavailable (useful for k8s liveness probes)
- Exposes per-host RPS/latency/in-flight/status rates, thread state, per-thread memory, worker crashes/restarts/queue, request duration percentiles, and process CPU/RSS
- Works alongside the TUI (`--expose` without `--daemon`) or standalone

### General

- Tab-based navigation between Caddy and FrankenPHP views
- Auto-detection of FrankenPHP and Caddy processes
- Stale data and connection loss detection
- Cross-platform binaries, Homebrew tap, and Docker image

## Install

```bash
brew install alexandre-daubois/tap/ember
```

Or with Go:

```bash
go install github.com/alexandre-daubois/ember/cmd/ember@latest
```

Or with Docker (runs in daemon mode with Prometheus export on `:9191` by default):

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember
```

## Usage

Make sure Caddy is running with the admin API and metrics enabled:

```
{
    admin localhost:2019
    metrics
}
```

Then:

```bash
ember
```

Ember connects to the Caddy admin API and auto-detects FrankenPHP if present. In Caddy-only mode, the dashboard shows per-host HTTP metrics. When FrankenPHP is detected, a second tab provides thread-level introspection.

### Options

```
--addr string        Caddy admin API address (default "http://localhost:2019")
--interval dur       Polling interval (default 1s)
--slow-threshold ms  Slow request threshold (default 500)
--pid int            FrankenPHP PID (auto-detected if not set)
--json               JSON output mode (streaming JSONL)
--expose addr        Expose Prometheus metrics (e.g. --expose=:9191)
--daemon             Headless mode (requires --expose)
--metrics-prefix str Prefix for exported Prometheus metric names
--no-color           Disable colors
```

### Keybindings

| Key | Action |
|-----|--------|
| `Tab` | Switch between Caddy / FrankenPHP tabs |
| `1` / `2` | Jump to tab |
| `↑` `↓` `j` `k` | Navigate list |
| `Home` / `End` | Jump to first / last item |
| `PgUp` / `PgDn` | Page navigation |
| `Enter` | Detail panel |
| `s` / `S` | Cycle sort field |
| `p` | Pause / resume |
| `r` | Restart workers (FrankenPHP) |
| `/` | Filter |
| `g` | Full-screen graphs |
| `?` | Help overlay |
| `q` | Quit |

### Shell Completions

```bash
# Bash
ember completion bash > /etc/bash_completion.d/ember

# Zsh
ember completion zsh > "${fpath[1]}/_ember"

# Fish
ember completion fish > ~/.config/fish/completions/ember.fish
```

## License

MIT
