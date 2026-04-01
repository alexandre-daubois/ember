---
name: ember-setup
description: "Guide users through installing and configuring Ember, a real-time terminal dashboard for Caddy and FrankenPHP. Use this skill whenever someone asks about installing ember, setting up ember, configuring Caddy for ember, enabling Caddy metrics, running ember for the first time, connecting ember to a Caddy server, or mentions 'ember init'. Also trigger when users mention monitoring Caddy or FrankenPHP from the terminal and need help getting started, even if they don't mention ember by name but are clearly working with Caddy monitoring."
---

# Ember Setup Guide

Ember is a zero-config, read-only terminal dashboard that connects to Caddy's admin API to display real-time traffic metrics, latency percentiles, and FrankenPHP thread introspection. This skill walks through installation, Caddy configuration, and first launch.

## Prerequisites

- A running **Caddy** server (any recent version)
- For FrankenPHP thread-level metrics (method, URI, duration, memory): **FrankenPHP 1.13+**. Older versions only show thread index and state.

## Step 1: Install Ember

Offer the user the installation method that fits their environment:

### Homebrew (macOS / Linux)

```bash
brew install alexandre-daubois/tap/ember
```

On macOS, Gatekeeper may block the first run because the binary is not notarized. If this happens, remove the quarantine flag:

```bash
xattr -d com.apple.quarantine $(which ember)
```

### Go

```bash
go install github.com/alexandre-daubois/ember/cmd/ember@latest
```

### Docker

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember
```

The Docker image runs in daemon mode by default (headless, Prometheus metrics on `:9191`). See the ember-production skill for Docker deployment details.

## Step 2: Configure Caddy

Ember needs two things from Caddy: the **admin API** and the **metrics directive**. Both go in the Caddyfile global block.

### Minimal Caddyfile

```
{
    admin localhost:2019
    metrics
}

example.com {
    respond "Hello, world!" 200
}
```

**Why the admin API matters:** Ember connects to `localhost:2019` to read configuration, thread states, and Prometheus metrics. If the admin block is missing, Caddy disables the API entirely.

**Why the metrics directive matters:** Without it, Caddy doesn't expose `caddy_http_requests_total`, `caddy_http_request_duration_seconds`, or other HTTP counters. Ember's traffic table will be empty.

**Why explicit hostnames matter:** Use `example.com { ... }` rather than `:80 { ... }`. Caddy only adds the `host` label to metrics when the route has a host matcher. Without it, all traffic appears under a single `*` row instead of per-host breakdown.

### Quick verification

Run these to confirm Caddy is ready:

```bash
# Check admin API is reachable
curl -s http://localhost:2019/config/ | head -c 100

# Check metrics are flowing
curl -s http://localhost:2019/metrics | grep caddy_http_requests_total
```

If the first returns JSON, the API is up. If the second returns a line with `caddy_http_requests_total`, metrics are enabled.

## Step 3: Run `ember init`

This is the recommended way to validate the setup. It checks everything and can fix missing metrics automatically:

```bash
ember init
```

What it does:
1. Verifies the admin API is reachable
2. Checks that HTTP servers are configured
3. Enables the `metrics` directive via the API if missing (no Caddy restart needed)
4. Detects FrankenPHP presence, threads, and workers
5. Confirms metrics are actually flowing

Useful flags:
- `ember init -y` — skip confirmation prompts
- `ember init -yq` — skip prompts and suppress output (for scripting)
- `ember init --addr https://prod:2019 --ca-cert ca.pem` — remote/TLS setup

## Step 4: Launch Ember

```bash
ember
```

Ember connects to `http://localhost:2019` and starts polling every second. If FrankenPHP is detected, a second tab appears automatically.

### Connecting to a different address

```bash
ember --addr http://your-host:2019
```

Or via environment variable:

```bash
export EMBER_ADDR=http://your-host:2019
ember
```

### Adjusting the polling interval

```bash
ember --interval 2s
```

## TLS and mTLS

For production Caddy instances served over HTTPS:

### Custom CA certificate (private PKI, self-signed)

```bash
ember --addr https://caddy.internal:2019 --ca-cert /path/to/ca.pem
```

### Mutual TLS (mTLS)

When Caddy requires client certificates:

```bash
ember --addr https://caddy.internal:2019 \
  --ca-cert /path/to/ca.pem \
  --client-cert /path/to/client.pem \
  --client-key /path/to/client-key.pem
```

### Skip TLS verification (development only)

```bash
ember --addr https://localhost:2019 --insecure
```

This disables all certificate verification — never use in production.

## Basic Navigation

Once Ember is running:

| Key | Action |
|-----|--------|
| `Tab` / `1` / `2` | Switch between Caddy and FrankenPHP tabs |
| `Up` / `Down` / `j` / `k` | Navigate the list |
| `Enter` | Open detail panel (latency percentiles, status codes, TTFB) |
| `s` / `S` | Cycle sort field forward / backward |
| `/` | Filter by hostname |
| `g` | Toggle full-screen graphs (CPU, RPS, RSS) |
| `p` | Pause / resume polling |
| `?` | Help overlay |
| `q` | Quit |

## Environment Variables

All main flags have environment variable equivalents — useful for containers and scripts:

| Variable | Flag | Example |
|----------|------|---------|
| `EMBER_ADDR` | `--addr` | `http://caddy:2019` |
| `EMBER_INTERVAL` | `--interval` | `5s` |
| `EMBER_EXPOSE` | `--expose` | `:9191` |

Explicit flags always take precedence over environment variables.

## Shell Completions

Set up tab completion for your shell:

```bash
# Bash
ember completion bash > /etc/bash_completion.d/ember

# Zsh
ember completion zsh > "${fpath[1]}/_ember"

# Fish
ember completion fish > ~/.config/fish/completions/ember.fish
```

## What's Next

- If something isn't working, check the **ember-troubleshoot** skill
- For production deployment (daemon mode, Prometheus, Docker), see the **ember-production** skill
