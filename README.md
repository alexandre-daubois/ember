# Ember - Real-time TUI dashboard for Caddy & FrankenPHP

[![CI](https://github.com/alexandre-daubois/ember/actions/workflows/ci.yml/badge.svg)](https://github.com/alexandre-daubois/ember/actions/workflows/ci.yml)
[![codecov](https://codecov.io/github/alexandre-daubois/ember/graph/badge.svg?token=3BG1TUO91L)](https://codecov.io/github/alexandre-daubois/ember)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/alexandre-daubois/ember)](https://goreportcard.com/report/github.com/alexandre-daubois/ember)
[![Go Reference](https://pkg.go.dev/badge/github.com/alexandre-daubois/ember.svg)](https://pkg.go.dev/github.com/alexandre-daubois/ember)

Monitor your Caddy server in real time: per-host traffic, latency percentiles, status codes, and more. When FrankenPHP is detected, unlock per-thread introspection, worker management, and memory tracking.

![ember screenshot](https://github.com/alexandre-daubois/ember/blob/main/assets/ember.gif?raw=true)

## Why Ember?

Caddy exposes rich metrics through its admin API and Prometheus endpoint, but reading raw Prometheus text or setting up a full Grafana stack just to glance at traffic is heavy. Ember gives you a zero-config, read-only terminal dashboard that connects to Caddy's admin API and works out of the box. No extra infrastructure, no YAML to write: just `ember` and you're monitoring.

## Features

**Caddy Monitoring**

- Per-host traffic table with RPS, average latency, status codes, and sparklines
- Latency percentiles (P50, P90, P99) per host in the detail panel
- Full-screen ASCII graphs (CPU, RPS, RSS)

**FrankenPHP Introspection**

- Per-thread state, method, URI, duration, and memory tracking
- Worker management with queue depth and crash monitoring
- Graphs for queue depth and busy threads

**Integration & Operations**

- Prometheus metrics export (`/metrics`) and health endpoint (`/healthz`)
- Daemon mode for headless operation
- JSON output mode for scripting, with `--once` for single snapshots
- Quick health check: `ember status` for a one-line Caddy summary
- Auto-detection of FrankenPHP and Caddy processes
- Lightweight: ~15 MB RSS, ~0.3 ms per poll cycle with 100 threads and 10 hosts ([benchmarks](internal/app/daemon_bench_test.go))
- Cross-platform binaries, Homebrew tap, and Docker image

## Install

```bash
brew install alexandre-daubois/tap/ember
```

Or with Go:

```bash
go install github.com/alexandre-daubois/ember/cmd/ember@latest
```

Or with Docker (runs in daemon mode by default, see [Docker docs](docs/docker.md)):

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember
```

## Quick Start

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

Ember connects to the Caddy admin API and auto-detects FrankenPHP if present.

For a quick one-line health check:

```bash
ember status
```

## How It Works

Ember polls the Caddy admin API and Prometheus metrics endpoint at a regular interval (default: 1s), computes deltas and derived metrics (RPS, percentiles, error rates), and renders everything in a [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI.

## Documentation

Full documentation is available in the [docs/](docs/index.md) directory:

- [Getting Started](docs/getting-started.md): Install and first run
- [Caddy Configuration](docs/caddy-configuration.md): Caddyfile requirements
- [Caddy Dashboard](docs/caddy-dashboard.md): Per-host traffic and latency
- [FrankenPHP Dashboard](docs/frankenphp-dashboard.md): Thread introspection and workers
- [CLI Reference](docs/cli-reference.md): Flags, keybindings, shell completions
- [JSON Output](docs/json-output.md): Streaming JSONL for scripting
- [Prometheus Export](docs/prometheus-export.md): Metrics, health checks, daemon mode
- [Docker](docs/docker.md): Container usage

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, architecture overview, and guidelines.

## License

MIT
