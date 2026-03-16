# Getting Started

This guide covers installation, prerequisites, and your first run with Ember.

## Requirements

- A running [Caddy](https://caddyserver.com/) server with the **admin API** and **metrics** enabled. See [Caddy Configuration](caddy-configuration.md) for details.

## Installation

### Homebrew

```bash
brew install alexandre-daubois/tap/ember
```

### Go

```bash
go install github.com/alexandre-daubois/ember/cmd/ember@latest
```

### Docker

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember
```

> **Note:** The Docker image runs in daemon mode by default. See [Docker](docker.md) for details.

## First Run

Start Ember:

```bash
ember
```

Ember connects to the Caddy admin API at `http://localhost:2019` and begins polling every second. If FrankenPHP is detected, a second tab appears automatically.

> **Tip:** To connect to a remote Caddy instance, use `ember --addr http://your-host:2019`.

## Basic Navigation

| Key | Action |
|-----|--------|
| `Tab` / `1` / `2` | Switch between Caddy and FrankenPHP tabs |
| `Up` / `Down` | Navigate the list |
| `Enter` | Open detail panel |
| `?` | Help overlay |
| `q` | Quit |

For the full keybinding reference, see [CLI Reference](cli-reference.md).

## What's Next?

- [Caddy Dashboard](caddy-dashboard.md): Understand the Caddy tab
- [FrankenPHP Dashboard](frankenphp-dashboard.md): Explore thread-level introspection
- [CLI Reference](cli-reference.md): All available flags and options
