# Logs

The Logs tab streams Caddy's logs into the Ember TUI in real time. A left
sidepanel lets you switch between **Runtime** logs (startup, reloads, TLS,
admin API, modules) and **Access** logs (HTTP requests), and drill into a
specific host within the access view.

Each line is parsed on the fly and kept in an in-memory ring buffer of the
last 10 000 entries per stream (access and runtime are held in separate
buffers so a busy server's access traffic cannot evict rare runtime lines).

## How it works (zero-config)

At startup, Ember:

1. Binds a single TCP listener (by default on a free loopback port).
2. Registers two sinks in Caddy pointing at that listener:
   - `__ember__`, scoped to `http.log.access` (access logs);
   - `__ember_runtime__`, excluding `http.log.access` (everything else).
3. Enables access logging on every server that did not already have a `logs`
   block (runtime logs flow unconditionally, so no equivalent step is needed).
4. Starts parsing incoming lines, routing each entry to the matching buffer
   by its `logger` field.

If Caddy is not yet reachable at startup, the listener stays open and a
background watchdog retries every 30 seconds until it succeeds. The watchdog
also re-registers both sinks if Caddy is reloaded (`caddy reload`, API-driven
config push, etc.), so log streaming resumes without user intervention.

At clean shutdown, Ember unregisters both sinks and restores the access-logs
config only on the servers it modified (a GET check prevents clobbering
config the user or another tool may have added in the meantime).

The end result: a stock Caddyfile with **no `log` directive** still produces
live access and runtime logs in Ember's TUI, and Caddy's persistent config
ends the session exactly where it started.

## Local vs remote Caddy

| Scenario                                     | Command                                                 |
|----------------------------------------------|---------------------------------------------------------|
| Caddy on the same host                       | `ember`                                                 |
| Caddy over a Unix socket                     | `ember --addr unix//path/to/admin.sock`                 |
| Caddy on a remote host                       | `ember --addr http://remote:2019 --log-listen :9210`    |
| Caddy in Docker (macOS/Windows)              | `ember --log-listen host.docker.internal:9210`          |

When `--addr` points at a non-local host, Ember does **not** auto-bind a
listener: a `127.0.0.1:<port>` address would not be reachable from the remote
Caddy process. In that case, pass `--log-listen <addr>` with an address Caddy
can reach and the same behaviour applies. Set `EMBER_LOG_LISTEN` if you'd
rather configure it via environment.

When the hostname in `--log-listen` cannot be resolved locally (e.g.
`host.docker.internal`), Ember binds on `0.0.0.0:<port>` instead and
advertises the original address to Caddy. This lets a containerised Caddy
reach the host without extra networking setup.

## Hot-registered sinks

```jsonc
PUT /config/logging/logs/__ember__
{
  "writer":  { "output": "net", "address": "tcp/HOST:PORT", "soft_start": true },
  "encoder": { "format": "json" },
  "include": ["http.log.access"]
}

PUT /config/logging/logs/__ember_runtime__
{
  "writer":  { "output": "net", "address": "tcp/HOST:PORT", "soft_start": true },
  "encoder": { "format": "json" },
  "exclude": ["http.log.access"]
}
```

Notes:

- Both sinks push to the same TCP listener; Caddy opens one connection per
  sink and Ember routes the entries by `logger` name.
- `soft_start: true` means Caddy never blocks if the listener is briefly
  unavailable.
- `include` on `__ember__` and `exclude` on `__ember_runtime__` keep the
  routing symmetric: every log line reaches exactly one Ember buffer.
- The `net` writer reconnects on its own when Ember restarts.

## Sidepanel

The left column is a tree:

```
Runtime
Access
  api.example.com
  static.example.com
  ...
```

- **Runtime** shows everything Caddy logs that is not an HTTP access entry:
  startup, reload, TLS handshakes, admin API, plugin logs.
- **Access** shows all HTTP access logs across every host.
- The children under Access are the hosts actually seen in recent traffic,
  sorted alphabetically. Selecting one narrows the table to that host and
  drops the Host column so URIs get more room.

Selecting an entry resumes live-follow mode so you always see fresh data when
you drill in. The filter (typed with `/`) composes with the sidepanel: type
`500` while on `api.example.com` to see only 5xx responses for that host.

## Reading the table

**Access view** (Access aggregate or per-host):

| Column   | Description                                                  |
|----------|--------------------------------------------------------------|
| Time     | Local time the log entry was emitted, millisecond precision  |
| Code     | HTTP status code, color-coded (green 2xx, orange 4xx, red 5xx) |
| Method   | HTTP method                                                  |
| Host     | Value of `request.host` (hidden in per-host view)            |
| Duration | Server-side processing time in milliseconds                  |
| URI      | Value of `request.uri`, truncated to fit                     |

**Runtime view**:

| Column   | Description                                                  |
|----------|--------------------------------------------------------------|
| Time     | Local time the log entry was emitted, millisecond precision  |
| Level    | Log level, color-coded (red ERROR/FATAL, orange WARN). A textual prefix doubles the cue so ERROR rows start with `!` and WARN rows with `*`, keeping severity scannable when `NO_COLOR` is set |
| Logger   | Caddy logger name (`tls.handshake`, `admin.api`, ...)        |
| Message  | The log message                                              |

Lines that fail to parse as JSON are still shown in grey in the runtime view,
so corrupt or mid-write lines never silently disappear.

## Scroll modes

The table has two states:

- **Following** (default): pins the newest entry at the top and redraws as
  lines arrive.
- **Frozen**: stops sliding so you can read a specific line without having it
  pushed down. The full buffer at freeze time is available, so you can scroll
  well past the initial viewport to inspect older entries. A pill on the right
  of the column header shows `● PAUSED` plus how many new lines have been
  captured in the background.

Entering Frozen mode happens either implicitly when you scroll (`↑`, `↓`,
`PgUp`, `PgDn`, `End`) or explicitly by pressing `p`. Resume live follow with
`f` (or `Home`, or `p` again). Switching sidepanel selection also resumes live
mode so the frozen snapshot does not get out of sync with the visible buffer.

The header also surfaces a `dropped: N` chip once the in-memory ring buffer
wraps. It is a reminder that the tail window holds the most recent 10 000
entries per scope, not the full history: any older lines have been evicted to
keep the memory footprint bounded.

## Keybindings

Focus is on the sidepanel by default when entering the tab.

| Key             | Action                                                   |
|-----------------|----------------------------------------------------------|
| `←` / `h`       | Move focus to the sidepanel                              |
| `→` / `l` / `Enter` | Move focus back to the table                        |
| `↑` / `↓`       | Navigate the focused panel. On the table, auto-freezes on first press from live |
| `PgUp` / `PgDn` | Page up/down in the table (also auto-freezes)            |
| `End`           | Jump to the oldest entry in the frozen snapshot (table), or the last sidepanel item |
| `Home`          | First sidepanel item (sidepanel focus); resume follow (table focus) |
| `f`             | Resume live follow (table focus)                         |
| `/`             | Filter: matches case-insensitively across all visible columns |
| `p`             | Toggle pause: freezes or resumes the table               |
| `c`             | Clear the current buffer (also resumes live follow)      |
| `Tab`           | Switch tab                                               |
| `?`             | Help overlay                                             |
| `q`             | Quit                                                     |

## Jumping from the Caddy tab

While on the **Caddy** tab, press `l` on a host to switch to the Logs tab
with the sidepanel pre-selected on that host's access entries. Go back to the
aggregate view with `←` then `↑` to navigate the sidepanel.

## What happens if Ember crashes or is killed

Ember removes both sinks and the access-logs config it added at clean exit.
Best-effort against unexpected exits:

| Event                            | Behaviour                                   |
|----------------------------------|---------------------------------------------|
| `q`, Ctrl+C, normal quit         | Deferred cleanup runs ✓                      |
| Go panic                         | Deferred cleanup runs (panics unwind defers) ✓ |
| `SIGTERM` (`systemctl stop`)     | Trapped, forwarded to clean shutdown ✓      |
| `caddy reload` / config push     | Watchdog re-registers both sinks + access-logs within 30 s ✓ |
| Caddy starts after Ember         | Watchdog activates streaming within 30 s ✓              |
| `SIGKILL`, OOM kill, power loss  | Sinks and auto-enabled access-logs blocks remain: see below |

If a `SIGKILL`-class event leaves state behind, Caddy keeps the registrations
but the writers use `soft_start: true`, so they do not block reloads or spam
errors. To clean up:

- **Run `ember` again**: Ember uses idempotent `PUT`s to register its sinks,
  overwriting any stale entries. The next clean quit then removes everything.
- **Or remove them manually**:

  ```bash
  curl -X DELETE http://localhost:2019/config/logging/logs/__ember__
  curl -X DELETE http://localhost:2019/config/logging/logs/__ember_runtime__
  # plus, for each server Ember may have touched:
  curl -X DELETE http://localhost:2019/config/apps/http/servers/<srv>/logs
  ```

## What this feature does NOT do

- It does not write logs to the JSON output, daemon mode, or Prometheus
  exporter. Log streaming is a TUI-only convenience.
- The runtime view's INFO-and-above firehose can be noisy under high load.
  Use `/` to filter (for example to `error`) if the list moves faster than
  you can read.
