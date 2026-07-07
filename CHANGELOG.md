# Changelog

All notable changes to Ember are documented here.

## Unreleased

### Added

- Config file support (`.ember.toml`): declare a fleet of named endpoints once instead of repeating `--addr`. New `-f`/`--config` flag and `EMBER_CONFIG` env var; `--daemon`, `--json`, `status`, `wait` and `init` fan out across all endpoints; the TUI connects to the saved `default` or shows an interactive picker. `ember config use <name>` sets the TUI default.

### Changed

- Bump dependencies

## 1.4.3 - 2026-06-22

### Changed

- Bump dependencies

## 1.4.2 - 2026-06-22

### Security

- Neutralize terminal control and escape sequences (CWE-150) in attacker-controlled fields rendered by the TUI (request URI, host and method, raw log lines, in-flight FrankenPHP requests, certificate subject and issuer), so untrusted log and metric values can no longer inject ANSI/OSC/CSI sequences into the operator's terminal.

## 1.4.1 - 2026-05-25

### Added

- Vim-style tab-select mode: press `t` then `1`-`9` to jump to a tab, `Esc` to cancel.
- `FooterRenderer` plugin interface: plugins can override Ember's global footer hint line while their tab is active by implementing `FooterText(width int) string`.
- Right-of-refusal for `1`-`9` and `t` keys on plugin tabs: the active plugin's `HandleKey` sees them first and can consume them before the default tab-switch.
- Community Plugins section in the plugins documentation!

### Changed

- Bump dependencies

## 1.4.0 - 2026-05-11

### Added

- Multi-instance monitoring: poll several Caddy/FrankenPHP instances at once, with per-instance TLS, polling interval, health check and PID resolution.
- `ember init`, `ember diff`, `ember wait`, `ember status` now support multi-instance setups.
- `ember wait --any`: return as soon as any instance is reachable instead of waiting for all.
- `MultiInstancePlugin` opt-in marker so plugins can receive per-instance `Fetch` calls; plugins without the marker are disabled in multi-instance mode with a warning.
- `ember_instance="<name>"` label on every Prometheus metric (except `ember_build_info`) when `--addr` is repeated; single-instance output is unchanged.
- Access-by-route view.

### Changed

- TUI mode refuses repeated `--addr` with an explicit error pointing at `--daemon` / `--json`; multi-instance is supported only in non-interactive modes.

### Fixed

- `ember status` with multi-instance now distinguishes TLS misconfiguration (`Caddy TLS configuration failed (...)` and a JSON `error` field) from a network outage, instead of reporting both as `UNREACHABLE`.

## 1.3.0 - 2026-04-22

### Added

- Plugin system for extending Ember with custom collectors and views.
- Log streaming and runtime logs tab with filtering and breadcrumb navigation.
- DockerHub image distribution.

## 1.2.0 - 2026-04-16

### Added

- Upstreams tab.
- Ember self-metrics so operators can monitor the monitor.
- P90 in JSON streaming output derived metrics.
- Shortcut `i` to change the polling interval at runtime.

### Changed

- **[BC BREAK]** Stop mirroring metrics already exposed by Caddy.
- FrankenPHP tab is always rendered in second position when enabled.
- Better rendering when many PHP threads are present.

### Fixed

- Reject `--timeout` shorter than `--interval` at startup.
- Reject invalid Prometheus metric prefixes at startup.
- Refuse percentiles without a baseline snapshot instead of returning cumulative counts.
- Detect FrankenPHP worker counter reset independently from Caddy.
- Race on `HTTPFetcher` transport during TLS reload.
- Viewport clipping on all tabs.

## 1.1.0 - 2026-04-10

### Added

- Caddy Config tab with reload status badge.
- Certificates tab.
- Waterfall graph for TTFB/transfer timings.
- Unix socket connection support.
- One-liner installation script.
- P90 in the Prometheus exporter.

### Changed

- Dashboard bar styling refresh.
- Expand all config sections by default.

### Fixed

- Window resizing glitches.
- Rare panic in long-running processes.

## 1.0.1 - 2026-04-03

### Added

- AI agent skills shipped with the project.
- `Shift+Tab` cycles to the previous tab.

### Changed

- Improved graph rendering for float values.

### Fixed

- Documentation polishes around macOS quarantine and FrankenPHP compatibility.

## 1.0.0 - 2026-03-26

Initial public release of Ember: real-time TUI for monitoring Caddy and FrankenPHP, JSON streaming mode, Prometheus exporter, and core dashboard tabs.
