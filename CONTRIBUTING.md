# Contributing to Ember

Thanks for your interest in contributing to Ember! Here's everything you need to get started.

## Prerequisites

- Go 1.26+
- A running Caddy instance (optional, for manual testing)

## Getting Started

```bash
git clone https://github.com/alexandre-daubois/ember.git
cd ember
go build ./cmd/ember/
```

## Running Tests

```bash
go test ./... -race
```

All tests run against mocks/httptest servers and don't require a live Caddy instance.

## Linting

The project uses [golangci-lint](https://golangci-lint.run/):

```bash
golangci-lint run
```

CI runs the linter on every push and PR. Check `.golangci.yml` for the active rules.

## Code Style

- No obvious comments: explain the *why*, never the *what*
- Don't export symbols without a clear, immediate reason
- Test both happy and unhappy paths
- Keep changes focused, don't refactor adjacent code in a bugfix PR

## Submitting a Pull Request

1. Fork the repository and create a branch from `main`
2. Make your changes
3. Ensure tests pass (`go test ./... -race`) and the linter is clean
5. Open a PR with a clear description

## Reporting Bugs

Use the [bug report template](https://github.com/alexandre-daubois/ember/issues/new?template=bug_report.md). Include your OS, Go version, and Caddy/FrankenPHP version if relevant.

## Requesting Features

Open an issue using the [feature request template](https://github.com/alexandre-daubois/ember/issues/new?template=feature_request.md).

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
