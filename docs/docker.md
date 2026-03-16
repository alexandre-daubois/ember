# Docker

Ember is available as a container image built from scratch (no OS, no shell: just the static binary and CA certificates).

## Quick Start

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember
```

## Default Behavior

The image defaults to `--daemon --expose :9191`. This means:

- No TUI (headless mode)
- Prometheus metrics available on port `9191` (`/metrics` and `/healthz`)
- Connects to the Caddy admin API at `http://localhost:2019`

## Network Configuration

Ember needs to reach the Caddy admin API. Two options:

### Host Network

Use `--network host` so the container shares the host's network stack:

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember
```

### Custom Address

Point Ember to the Caddy service by name or IP:

```bash
docker run --rm ghcr.io/alexandre-daubois/ember --daemon --expose :9191 --addr http://caddy:2019
```

## Custom Flags

Override the default `CMD` by appending flags:

```bash
docker run --rm --network host ghcr.io/alexandre-daubois/ember \
  --daemon --expose :9191 --interval 2s --metrics-prefix myapp
```

## Docker Compose

A minimal setup with Caddy and Ember as a sidecar:

```yaml
services:
  caddy:
    image: caddy:latest
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile

  ember:
    image: ghcr.io/alexandre-daubois/ember
    network_mode: "service:caddy"
    depends_on:
      - caddy
```

With this setup, Ember runs in the same network namespace as Caddy and can reach `localhost:2019` directly.

> **Caution:** The image is built from `scratch`: there is no shell, no `exec`, and no debugging tools inside the container. Use `docker logs` to read Ember's stderr output.

## See Also

- [Getting Started](getting-started.md)
- [Prometheus Export](prometheus-export.md)
