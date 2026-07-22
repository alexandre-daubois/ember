# Changelog

All notable changes to Ember are documented here.

## Unreleased

### Added

- **Avg Mem** and **Max Mem** columns in the Logs tab's By Route view: per-route PHP memory usage sampled from busy FrankenPHP threads, sortable like the latency columns. Helps spot memory-hungry routes, detect leaks in worker mode, and size servers from the max footprint (#73).

## 1.5.0 - 2026-07-13

### Added

- Config file support (`.ember.toml`): declare a fleet of named endpoints once instead of repeating `--addr`. New `-f`/`--config` flag and `EMBER_CONFIG` env var; `--daemon`, `--json`, `status`, `wait` and `init` fan out across all endpoints; the TUI connects to the saved `default` or shows an interactive picker. `ember config use <name>` sets the TUI default.

### Changed

- Malformed `EMBER_*` environment variable values now abort startup with an explicit error instead of being silently ignored.
- Multi-instance `/healthz` returns 503 when no instance has reported data yet.
- `ember status` no longer scans local processes when the target address is remote.
- Bump dependencies

### Fixed

- Corrupted per-host latency percentiles in `--json` streaming output.
- In-flight requests are summed across all server series instead of reading a single one.
- The per-endpoint `,interval=` suffix is honored in TUI mode, including the `/healthz` staleness threshold when combined with `--expose`.
- `--timeout` applies to each fetch in `ember status`, so a timeout equal to the polling interval works.
- Buffered log lines are no longer lost when the log connection drops.
- The dropped-entries baseline resets when the log buffer is cleared.
- Multi-instance daemon keeps exporting the last known plugin data when a fetch fails, and subscriber-only plugins now receive updates in daemon mode.
- The dashboard stays visible when only part of a fetch fails.
- Ember excludes its own process when scanning for FrankenPHP.
- Global TLS flags are accepted alongside Unix socket endpoints in mixed fleets.
- The Docker image reports its actual version.
- Assorted TUI fixes: cell-aware truncation of host, URI and address columns (wide characters), panel heights clamped to the terminal, Graphs view fits the available height and honors `NO_COLOR`, non-ASCII input accepted in the main filter, logs side panel keeps the selection visible, `Enter` is ignored on empty lists, help overlay counts plugin tabs and shows the `n`/`N` config search shortcuts.

### Security

- `--metrics-auth` credentials are compared in constant time regardless of username validity or credential length, closing a timing side channel on the metrics endpoint.
- Terminal escape sequences are neutralized in the fetch error shown on the connection-error screen.
- `install.sh` verifies the downloaded release archive against the published `checksums.txt` before installing.

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
- `-i` short flag as an alias for `--interval`.

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
