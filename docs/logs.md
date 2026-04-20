# Access Logs

The Access logs tab streams Caddy's HTTP access logs into the Ember TUI in real time,
with a free-text filter that matches case-insensitively across the status
code, method, host, URI and message. Each line is parsed on the fly and kept
in an in-memory ring buffer of the last 10 000 entries.

## How it works (zero-config)

At startup, Ember:

1. Binds a TCP listener (by default on a free loopback port).
2. Attempts to register the `ember` sink and enable access logging on every
   server (see details below). If Caddy is not yet reachable (e.g. Ember
   started before Caddy), the listener stays open and a background watchdog
   retries every 30 seconds until it succeeds: no restart required.
3. Starts parsing incoming lines into the Access logs tab.

At clean shutdown, Ember:

- Unregisters the `ember` sink.
- Restores the access-logs config only on the servers it modified (a GET
  check prevents clobbering config the user or another tool may have added in
  the meantime).

The end result: a stock Caddyfile with **no `log` directive** still produces
live logs in Ember's TUI, and Caddy's persistent config ends the session
exactly where it started.

## Local vs remote Caddy

| Scenario                                     | Command                                 |
|----------------------------------------------|-----------------------------------------|
| Caddy on the same host                       | `ember`                                 |
| Caddy over a Unix socket                     | `ember --addr unix//path/to/admin.sock` |
| Caddy on a remote host                       | `ember --addr http://remote:2019 --log-listen :9210` |
| Caddy in Docker (macOS/Windows)              | `ember --log-listen host.docker.internal:9210` |

When `--addr` points at a non-local host, Ember does **not** auto-bind a
listener: a `127.0.0.1:<port>` address would not be reachable from the remote
Caddy process. In that case, pass `--log-listen <addr>` with an address Caddy
can reach and the same behaviour applies. Set `EMBER_LOG_LISTEN` if you'd
rather configure it via environment.

When the hostname in `--log-listen` cannot be resolved locally (e.g.
`host.docker.internal`), Ember binds on `0.0.0.0:<port>` instead and
advertises the original address to Caddy. This lets a containerised Caddy
reach the host without extra networking setup.

## Hot-registered sink details

The sink Ember installs looks like this:

```jsonc
PUT /config/logging/logs/__ember__
{
  "writer":  { "output": "net", "address": "tcp/HOST:PORT", "soft_start": true },
  "encoder": { "format": "json" },
  "include": ["http.log.access"]
}
```

Notes:

- The sink is named `ember` so a stale registration left over from a prior
  crash gets overwritten on the next launch.
- `soft_start: true` means Caddy never blocks if the listener is briefly
  unavailable.
- `include: ["http.log.access"]` scopes the sink to access logs only; your
  other logs (errors, boot, TLS, modules) are untouched.
- The `net` writer reconnects on its own when Ember restarts.
- A background watchdog checks every 30 seconds whether the sink still exists
  in Caddy's config. If Caddy is reloaded (`caddy reload`, API-driven config
  push, etc.), the runtime-only sink and access-logs blocks are lost: the
  watchdog re-registers both automatically so log streaming resumes without
  user intervention.

## Reading the table

| Column   | Description                                                  |
|----------|--------------------------------------------------------------|
| Time     | Local time the log entry was emitted, millisecond precision  |
| Code     | HTTP status code, color-coded (green 2xx, orange 4xx, red 5xx) |
| Method   | HTTP method                                                  |
| Host     | Value of `request.host`                                      |
| Duration | Server-side processing time in milliseconds                  |
| URI      | Value of `request.uri`, truncated to fit                     |

Lines that fail to parse as JSON are still shown in grey, so corrupt or
mid-write lines never silently disappear.

## Scroll modes

The Access logs tab has two states:

- **Following** (default): the table pins the newest entry at the top and
  redraws as lines arrive.
- **Frozen**: the view stops sliding so you can read a specific line without
  having it pushed down by new arrivals. The full buffer at freeze time is
  available, so you can scroll well past the initial viewport to inspect older
  entries. A pill on the right of the column header shows `● PAUSED` plus how
  many new lines have been captured in the background.

Entering Frozen mode happens either implicitly when you scroll (`↑`, `↓`,
`PgUp`, `PgDn`, `End`) or explicitly by pressing `p`. Resume live follow with
`f` (or `Home`, or `p` again).

## Keybindings

| Key             | Action                                                   |
|-----------------|----------------------------------------------------------|
| `↑` / `↓`       | Navigate. Auto-freezes the view on first press from live |
| `PgUp` / `PgDn` | Page up/down (also auto-freezes)                         |
| `End`           | Jump to the oldest entry in the frozen snapshot          |
| `f` / `Home`    | Resume live follow                                       |
| `/`             | Filter: matches case-insensitively against status code, method, host, URI and message |
| `p`             | Toggle pause: freezes or resumes. Filter changes still re-slice the frozen window; new log lines are captured in the background |
| `c`             | Clear the in-memory buffer (also resumes live follow)    |
| `Tab`           | Switch tab                                               |
| `?`             | Help overlay                                             |
| `q`             | Quit                                                     |

The filter is the only text input on this tab: type `200` to see successful
requests, `GET` to see GET requests, `api.example.com` to focus on a host,
`/users` to narrow on a path.

## Jumping from the Caddy tab

While on the **Caddy** tab, press `l` on a host to switch to the Access logs tab
with the filter pre-set to that host. Clear it by pressing `/` and hitting
Enter on an empty input, or by entering any other search.

## What happens if Ember crashes or is killed

Ember removes both the sink and the access-logs config it added at clean
exit. Best-effort against unexpected exits:

| Event                            | Behaviour                                   |
|----------------------------------|---------------------------------------------|
| `q`, Ctrl+C, normal quit         | Deferred cleanup runs ✓                      |
| Go panic                         | Deferred cleanup runs (panics unwind defers) ✓ |
| `SIGTERM` (`systemctl stop`)     | Trapped, forwarded to clean shutdown ✓      |
| `caddy reload` / config push     | Watchdog re-registers sink + access-logs within 30 s ✓ |
| Caddy starts after Ember         | Watchdog activates streaming within 30 s ✓              |
| `SIGKILL`, OOM kill, power loss  | Sink and auto-enabled access-logs blocks remain: see below |

If a `SIGKILL`-class event leaves state behind, Caddy keeps the registration
but the writer uses `soft_start: true`, so it does not block reloads or spam
errors. To clean up:

- **Run `ember` again**: Ember uses an idempotent `PUT` to register its sink,
  which overwrites any stale entry left over from a prior crash. The next
  clean quit then removes everything.
- **Or remove it manually**:
  ```bash
  curl -X DELETE http://localhost:2019/config/logging/logs/__ember__
  # plus, for each server Ember may have touched:
  curl -X DELETE http://localhost:2019/config/apps/http/servers/<srv>/logs
  ```

## What this feature does NOT do

- It does not show anything except HTTP access logs. Runtime errors, TLS
  events, reverse-proxy failures, and module logs are intentionally excluded
  to keep the tabular view readable.
- It does not write logs to the JSON output, daemon mode, or Prometheus
  exporter. Log streaming is a TUI-only convenience.
