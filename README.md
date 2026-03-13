# Ember - Real-time TUI dashboard for Caddy & FrankenPHP

Monitor your Caddy server in real time: per-host traffic, latency percentiles, status codes, and more. When FrankenPHP is detected, unlock per-thread introspection, worker management, and memory tracking.

![ember screenshot](https://github.com/alexandre-daubois/ember/blob/main/assets/img.png?raw=true)

## Features

### Caddy dashboard (always available)

- Per-host traffic table with RPS, average response time, and in-flight requests
- Latency percentiles (P50, P90, P95, P99) from Prometheus histogram buckets
- Status code breakdown (2xx/s, 4xx/s, 5xx/s) per host
- Sorting and live filtering by hostname
- Live sparkline graphs (CPU, RPS, RSS)
- JSON output mode for scripting

### FrankenPHP dashboard (when detected)

- Per-thread introspection (URI, HTTP method, duration, memory)
- Worker queue depth and busy thread tracking
- Full-screen graphs (CPU, RPS, RSS, queue depth, busy threads)
- Graceful worker restart from the TUI
- Detail panel with per-thread info and memory sparkline trend
- Memory delta indicators (↑/↓) per thread between polls

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

Or with Docker:

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
--addr string    Caddy admin API address (default "http://localhost:2019")
--interval dur   Polling interval (default 1s)
--pid int        FrankenPHP PID (auto-detected if not set)
--json           JSON output mode (for scripting)
--no-color       Disable colors
```

### Keybindings

| Key | Action |
|-----|--------|
| `Tab` | Switch between Caddy / FrankenPHP tabs |
| `1` / `2` | Jump to tab |
| `↑` `↓` | Navigate list |
| `Enter` | Thread detail panel (FrankenPHP) |
| `s` / `S` | Cycle sort field |
| `p` | Pause / resume |
| `r` | Restart workers (FrankenPHP) |
| `/` | Filter |
| `g` | Full-screen graphs |
| `q` | Quit |

## License

MIT
