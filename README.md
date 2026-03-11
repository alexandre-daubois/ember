# Ember - Real-time terminal dashboard for FrankenPHP

Understand why your app is on fire by monitoring threads, workers, memory, request rates and more.

![ember screenshot](https://github.com/alexandre-daubois/ember/blob/main/assets/img.png?raw=true)

## Features

- Per-thread introspection (URI, HTTP method, duration, memory)
- Memory leak detection via linear regression on idle snapshots
- Live RPS and average response time with sparkline history
- Latency percentiles (P50, P95, P99) over a rolling 5-minute window
- Full-screen graphs (CPU, RPS, RSS, queue depth, busy threads)
- Sorting, live filtering, and detail panel
- Graceful worker restart from the TUI
- Stale data and connection loss detection
- JSON output mode for scripting
- Auto-detection of the FrankenPHP process
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

Make sure FrankenPHP is running with the admin API enabled:

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

Ember auto-detects the FrankenPHP process and connects to the Caddy admin API.

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
| `↑` `↓` | Navigate threads |
| `Enter` | Thread detail panel |
| `s` / `S` | Cycle sort field |
| `p` | Pause / resume |
| `l` | Toggle leak watcher |
| `r` | Restart workers |
| `/` | Filter threads |
| `g` | Full-screen graphs |
| `q` | Quit |

## License

MIT
