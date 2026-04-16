# Ember - Real-time monitoring for Caddy

[![CI](https://github.com/alexandre-daubois/ember/actions/workflows/ci.yml/badge.svg)](https://github.com/alexandre-daubois/ember/actions/workflows/ci.yml)
[![codecov](https://codecov.io/github/alexandre-daubois/ember/graph/badge.svg?token=3BG1TUO91L)](https://codecov.io/github/alexandre-daubois/ember)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Report Card](https://goreportcard.com/badge/github.com/alexandre-daubois/ember)](https://goreportcard.com/report/github.com/alexandre-daubois/ember)
[![Go Reference](https://pkg.go.dev/badge/github.com/alexandre-daubois/ember.svg)](https://pkg.go.dev/github.com/alexandre-daubois/ember)

Monitor your Caddy server in real time: per-host traffic, latency percentiles, status codes, and more. When FrankenPHP is detected, unlock per-thread introspection, worker management, and memory tracking. It is officially recommended by [FrankenPHP](https://frankenphp.dev/).

![ember banner](https://github.com/alexandre-daubois/ember/blob/main/assets/banner.webp?raw=true)

## Why Ember?

Caddy exposes rich metrics through its admin API and Prometheus endpoint, but reading raw Prometheus text or setting up a full Grafana stack just to glance at traffic is heavy. Ember gives you a zero-config, read-only terminal dashboard that connects to Caddy's admin API and works out of the box. No extra infrastructure, no YAML to write: just `ember` and you're monitoring.

![ember screenshot](https://github.com/alexandre-daubois/ember/blob/main/assets/ember.gif?raw=true)

## Features

**Caddy Monitoring**

- Per-host traffic table with RPS, average latency, status codes, and sparklines
- Latency percentiles (P50, P90, P95, P99) and Time-to-First-Byte per host
- Sorting, filtering, and full-screen ASCII graphs (CPU, RPS, RSS)
- Config Inspector tab: browse the live Caddy JSON config as a collapsible tree
- Certificates tab: TLS certificate monitoring with expiry tracking, color-coded warnings, and likely auto-renewal indication
- Upstreams tab: reverse proxy upstream health monitoring with per-upstream status, auto-detected when `reverse_proxy` is configured
- Automatic Caddy restart detection

**FrankenPHP Introspection**

- Per-thread state, method, URI, duration, and memory tracking (method, URI, duration, memory, and request count require FrankenPHP 1.12.2+)
- Worker management with queue depth and crash monitoring
- Graphs for queue depth and busy threads
- Automatic detection and recovery when FrankenPHP starts or stops

**Integration & Operations**

- Prometheus metrics export (`/metrics`) with optional basic auth and health endpoint (`/healthz`)
- Self-observability: `ember_*` metrics (build info, per-stage scrape totals, errors, durations, last success) so you can monitor the monitor
- Daemon mode for headless operation, with error throttling and TLS certificate reload via SIGHUP
- JSON output mode for scripting, with `--once` for single snapshots
- Quick health check: `ember status` (text or `--json`) for a one-line Caddy summary
- Readiness gate: `ember wait` blocks until Caddy is up (`-q` for silent scripting)
- Deployment validation: `ember diff before.json after.json` compares snapshots
- Zero-config setup: `ember init` checks Caddy, enables metrics, and warns about missing host matchers
- Unix socket support for Caddy admin APIs configured with `admin unix//path`
- TLS and mTLS support for secured Caddy admin APIs
- Environment variable configuration (`EMBER_ADDR`, `EMBER_EXPOSE`, ...) for container deployments
- `NO_COLOR` env var support ([no-color.org](https://no-color.org/))
- Lightweight: ~15 MB RSS, ~0.3 ms per poll cycle with 100 threads and 10 hosts ([benchmarks](internal/app/daemon_bench_test.go))
- Cross-platform binaries (Linux, macOS, Windows), Homebrew tap, and Docker image

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/alexandre-daubois/ember/main/install.sh | sh
```

Or with Homebrew:

```bash
brew install alexandre-daubois/tap/ember
```

> **macOS:** if Gatekeeper blocks the binary on first run, remove the quarantine attribute:
> `xattr -d com.apple.quarantine $(which ember)`, or allow it manually in
> System Settings → Privacy & Security.

Or with Go:

```bash
go install github.com/alexandre-daubois/ember/cmd/ember@latest
```

Or with Docker (runs in daemon mode by default, see [Docker docs](docs/docker.md)):

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember
```

You can also download the latest binaries from the [release page](https://github.com/alexandre-daubois/ember/releases). If you use this method, don't forget to check for updates regularly!

## Quick Start

Make sure Caddy is running with the admin API enabled (it is by default). Then:

```bash
ember init
```

This checks your Caddy setup and enables metrics via the admin API if needed (no restart required). Once ready:

```bash
ember
```

Ember connects to the Caddy admin API and auto-detects FrankenPHP if present.

For a quick one-line health check:

```bash
ember status
```

## How It Works

Ember polls the Caddy admin API and Prometheus metrics endpoint at a regular interval (default: 1s), computes deltas and derived metrics (RPS, percentiles, error rates), and renders them through one of several output modes: an interactive [Bubble Tea](https://github.com/charmbracelet/bubbletea) TUI (default), streaming JSONL, a headless daemon with Prometheus export, or a one-shot `status` command.

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
- [Agent Skills](docs/skills.md): Skills for AI coding agents
- [Troubleshooting](docs/troubleshooting.md): Common issues and solutions

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, architecture overview, and guidelines.

## License

MIT
