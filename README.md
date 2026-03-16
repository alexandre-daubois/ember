# Ember - Real-time TUI dashboard for Caddy & FrankenPHP

[![CI](https://github.com/alexandre-daubois/ember/actions/workflows/ci.yml/badge.svg)](https://github.com/alexandre-daubois/ember/actions/workflows/ci.yml)
[![codecov](https://codecov.io/github/alexandre-daubois/ember/graph/badge.svg?token=3BG1TUO91L)](https://codecov.io/github/alexandre-daubois/ember)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

Monitor your Caddy server in real time: per-host traffic, latency percentiles, status codes, and more. When FrankenPHP is detected, unlock per-thread introspection, worker management, and memory tracking.

![ember screenshot](https://github.com/alexandre-daubois/ember/blob/main/assets/img.png?raw=true)

## Features

- Per-host traffic table with RPS, latency percentiles (P50–P99), status codes, and sparklines
- FrankenPHP thread introspection with memory tracking and worker management
- Full-screen ASCII graphs (CPU, RPS, RSS, queue depth, busy threads)
- Prometheus metrics export (`/metrics`) and health endpoint (`/healthz`)
- Daemon mode for headless operation
- JSON output mode for scripting
- Auto-detection of FrankenPHP and Caddy processes
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

## License

MIT
