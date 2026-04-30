# Docker

Ember is available as a container image built from scratch (no OS, no shell: just the static binary and CA certificates).

The image is published to both registries with identical content:

- Docker Hub: `alexandredaubois/ember`
- GitHub Container Registry: `ghcr.io/alexandre-daubois/ember`

The examples below use the Docker Hub reference; substitute the GHCR one if you prefer.

## Quick Start

```bash
docker run --rm --network host alexandredaubois/ember
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
docker run --rm --network host alexandredaubois/ember
```

### Custom Address

Point Ember to the Caddy service by name or IP:

```bash
docker run --rm alexandredaubois/ember --daemon --expose :9191 --addr http://caddy:2019
```

## Custom Flags

Override the default `CMD` by appending flags:

```bash
docker run --rm --network host alexandredaubois/ember \
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
    image: alexandredaubois/ember
    network_mode: "service:caddy"
    depends_on:
      - caddy
```

With this setup, Ember runs in the same network namespace as Caddy and can reach `localhost:2019` directly.

> **Caution:** The image is built from `scratch`: there is no shell, no `exec`, and no debugging tools inside the container. Use `docker logs` to read Ember's stderr output.

## Unix Socket

If Caddy's admin API is configured to listen on a Unix socket, mount the socket into the Ember container:

```bash
docker run --rm \
  -v /run/caddy/admin.sock:/run/caddy/admin.sock \
  alexandredaubois/ember \
  --daemon --expose :9191 --addr unix//run/caddy/admin.sock
```

Or with Docker Compose:

```yaml
services:
  caddy:
    image: caddy:latest
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
      - caddy-admin:/run/caddy

  ember:
    image: alexandredaubois/ember
    environment:
      EMBER_ADDR: unix//run/caddy/admin.sock
    volumes:
      - caddy-admin:/run/caddy
    depends_on:
      - caddy

volumes:
  caddy-admin:
```

## Multi-instance sidecar

A single Ember container can scrape several Caddy instances and aggregate them behind one Prometheus endpoint:

```yaml
services:
  caddy-blue:
    image: caddy:latest
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile

  caddy-green:
    image: caddy:latest
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile

  ember:
    image: alexandredaubois/ember
    environment:
      EMBER_ADDR: blue=http://caddy-blue:2019,green=http://caddy-green:2019
    ports:
      - "9191:9191"
    depends_on:
      - caddy-blue
      - caddy-green
```

Every emitted Prometheus metric (except `ember_build_info`) carries an `ember_instance="blue"` or `ember_instance="green"` label. See [Prometheus Export](prometheus-export.md#multi-instance-label) for details and a recommended `metric_relabel_configs` snippet.

## See Also

- [Getting Started](getting-started.md)
- [Prometheus Export](prometheus-export.md)
