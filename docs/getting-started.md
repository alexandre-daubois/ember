# Getting Started

This guide covers installation, prerequisites, and your first run with Ember.

## Requirements

- A running [Caddy](https://caddyserver.com/) server with the **admin API** and **metrics** enabled. See [Caddy Configuration](caddy-configuration.md) for details.
- For FrankenPHP thread-level metrics (method, URI, duration, memory, request count): **FrankenPHP 1.12.2** or later. Older versions only expose thread index and state.

## Installation

### One-liner

```bash
curl -fsSL https://raw.githubusercontent.com/alexandre-daubois/ember/main/install.sh | sh
```

This detects your platform (Linux/macOS, amd64/arm64), downloads the latest release from GitHub, and installs the binary to `/usr/local/bin`. You can override the install directory with `EMBER_INSTALL_DIR`:

```bash
curl -fsSL https://raw.githubusercontent.com/alexandre-daubois/ember/main/install.sh | EMBER_INSTALL_DIR=~/.local/bin sh
```

### Homebrew

```bash
brew install alexandre-daubois/tap/ember
```

> **macOS:** the first time you run Ember, macOS Gatekeeper may block the binary because it is not notarized. If that happens, remove the quarantine attribute:
>
> ```bash
> xattr -d com.apple.quarantine $(which ember)
> ```
>
> Alternatively, you can allow it manually in **System Settings → Privacy & Security**.

### Go

```bash
go install github.com/alexandre-daubois/ember/cmd/ember@latest
```

### Docker

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember
```

> **Note:** The Docker image runs in daemon mode by default. See [Docker](docker.md) for details.

## Setup

Run `ember init` to check your Caddy configuration and enable metrics if needed:

```bash
ember init
```

This verifies that the admin API is reachable, enables the `metrics` directive via the API if missing (no restart required), and detects FrankenPHP.

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
