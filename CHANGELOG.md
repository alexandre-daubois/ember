# Changelog

All notable changes to Ember are documented here.

## 1.4.0-dev

### Added

- Multi-instance monitoring: poll several Caddy/FrankenPHP instances at once, with per-instance TLS, polling interval, health check and PID resolution (#51-#59, #62).
- `ember init`, `ember diff`, `ember wait`, `ember status` now support multi-instance setups (#52-#55).
- `ember wait --any`: return as soon as any instance is reachable instead of waiting for all (#53).
- `MultiInstancePlugin` opt-in marker so plugins can receive per-instance `Fetch` calls; plugins without the marker are disabled in multi-instance mode with a warning (#62).
- `ember_instance="<name>"` label on every Prometheus metric (except `ember_build_info`) when `--addr` is repeated; single-instance output is unchanged.
- Access-by-route view (#34).

### Changed

- TUI mode refuses repeated `--addr` with an explicit error pointing at `--daemon` / `--json`; multi-instance is supported only in non-interactive modes.

### Fixed

- `ember status` with multi-instance now distinguishes TLS misconfiguration (`Caddy TLS configuration failed (...)` and a JSON `error` field) from a network outage, instead of reporting both as `UNREACHABLE`.

## 1.3.0 - 2026-04-22

### Added

- Plugin system for extending Ember with custom collectors and views (#5).
- Log streaming and runtime logs tab with filtering and breadcrumb navigation (#21, #33).
- DockerHub image distribution.

## 1.2.0 - 2026-04-16

### Added

- Upstreams tab (#15).
- Ember self-metrics so operators can monitor the monitor (#30).
- P90 in JSON streaming output derived metrics (#32).
- Shortcut `i` to change the polling interval at runtime (#16).

### Changed

- **[BC BREAK]** Stop mirroring metrics already exposed by Caddy (#22).
- FrankenPHP tab is always rendered in second position when enabled (#20).
- Better rendering when many PHP threads are present (#29).

### Fixed

- Reject `--timeout` shorter than `--interval` at startup (#31).
- Reject invalid Prometheus metric prefixes at startup (#27).
- Refuse percentiles without a baseline snapshot instead of returning cumulative counts (#26).
- Detect FrankenPHP worker counter reset independently from Caddy (#25).
- Race on `HTTPFetcher` transport during TLS reload (#23).
- Viewport clipping on all tabs.

## 1.1.0 - 2026-04-10

### Added

- Caddy Config tab with reload status badge (#7, #8).
- Certificates tab (#9).
- Waterfall graph for TTFB/transfer timings (#10).
- Unix socket connection support (#11).
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

- AI agent skills shipped with the project (#6).
- `Shift+Tab` cycles to the previous tab.

### Changed

- Improved graph rendering for float values.

### Fixed

- Documentation polishes around macOS quarantine and FrankenPHP compatibility.

## 1.0.0 - 2026-03-26

Initial public release of Ember: real-time TUI for monitoring Caddy and FrankenPHP, JSON streaming mode, Prometheus exporter, and core dashboard tabs.
