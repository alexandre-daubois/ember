# Contributing to Ember

Thanks for your interest in contributing to Ember! Here's everything you need to get started.

## Prerequisites

- Go 1.26+
- [golangci-lint](https://golangci-lint.run/) (for linting)
- A running Caddy instance (optional, for manual testing)

## Getting Started

```bash
git clone https://github.com/alexandre-daubois/ember.git
cd ember
make build
```

Run `make help` to see all available targets:

```
make build       Build the binary
make test        Run tests with race detector
make lint        Run golangci-lint
make bench       Run benchmarks
make check       Run lint + test (same as CI)
make clean       Remove build artifacts
```

## Project Architecture

```
cmd/ember/           Entrypoint
internal/
  app/               CLI (Cobra), run modes (TUI, JSON, daemon)
  fetcher/           HTTP client for Caddy admin API, Prometheus metrics parsing
  model/             State management, derived metrics, percentiles
  ui/                Bubble Tea TUI components (dashboard, tables, graphs)
  exporter/          Prometheus metrics export
local/
  caddy/             Exhaustive Caddy setup (TLS, upstreams, multiple hosts)
  frankenphp/        Minimal FrankenPHP setup
```

> [!NOTE]
> You can start a local instance for fast testing. Two setups are available:
>
> ```bash
> cd local/frankenphp && make local   # minimal FrankenPHP (worker, metrics)
> cd local/caddy && make local        # exhaustive Caddy (TLS, upstreams, hosts)
> ```

## Running Tests

```bash
make test
```

All tests run against mocks/httptest servers and don't require a live Caddy instance.

For benchmarks:

```bash
make bench
```

## Linting

The project uses [golangci-lint](https://golangci-lint.run/):

```bash
make lint
```

CI runs the linter on every push and PR. Check `.golangci.yml` for the active rules.

## Before You Push

Run `make check` to replicate what CI does:

```bash
make check
```

This runs linting and tests with the race detector.

## Code Style

- No obvious comments: explain the *why*, never the *what*
- Don't export symbols without a clear, immediate reason
- Test both happy and unhappy paths
- Keep changes focused, don't refactor adjacent code in a bugfix PR

## Submitting a Pull Request

1. Fork the repository and create a branch from `main`
2. Make your changes
3. Run `make check` to ensure tests pass and the linter is clean
4. Open a PR with a clear description

## Reporting Bugs

Use the [bug report template](https://github.com/alexandre-daubois/ember/issues/new?template=bug_report.md). Include your OS, Go version, and Caddy/FrankenPHP version if relevant.

## Requesting Features

Open an issue using the [feature request template](https://github.com/alexandre-daubois/ember/issues/new?template=feature_request.md).

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
