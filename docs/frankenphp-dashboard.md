# FrankenPHP Dashboard

The FrankenPHP tab appears when Ember detects a FrankenPHP server. It provides per-thread introspection, worker management, and memory tracking. Ember is [recommended by FrakenPHP](https://frankenphp.dev/).

> **Note:** If the FrankenPHP tab does not appear, Ember could not reach the `/frankenphp/threads` admin API endpoint. Use `--frankenphp-pid` to specify the process manually, or verify that the admin API is accessible. See [Caddy Configuration](caddy-configuration.md).

## Compatibility

Some thread-level metrics require **FrankenPHP 1.12.1** or later. On older versions, the thread list shows only the thread index and state: the **Method**, **URI**, **Time**, **Mem**, and **Reqs** columns remain empty.

## Dashboard Header

The top of the FrankenPHP tab displays:

- **Thread bar**: A visual stacked bar showing the ratio of busy, idle, and inactive threads
- **Worker count**: Number of active workers
- **Queue depth**: Requests waiting in the worker queue
- **Crash counter**: Total worker crashes (highlighted when non-zero)

## Thread List

The main view shows a table of FrankenPHP threads:

| Column | Description |
|--------|-------------|
| **#** | Thread index |
| **State** | `●` busy (green), `○` idle (white), `◌` inactive (grey) |
| **Method** | HTTP method of the current request (when busy) |
| **URI** | URI being processed (when busy) |
| **Time** | Request duration |
| **Mem** | Memory usage with delta indicator |
| **Reqs** | Total request count |

Threads are grouped: **worker threads** appear first (grouped by worker script), followed by **regular threads**.

### Duration Colors

Request durations are color-coded based on the `--slow-threshold` flag (default: 500ms):

- **Normal**: Below the threshold
- **Yellow**: At or above the threshold
- **Red**: At or above 2x the threshold

### Memory Delta

The memory column shows `↑` or `↓` indicators when memory changes by more than 100 KB between polls. This helps spot memory leaks or unusual allocation patterns.

## Sorting

Press `s` to cycle the sort field forward, `S` to cycle backward.

The current sort field is shown in the bottom status bar.

## Filtering

Press `/` to enter filter mode. The filter matches against thread name, state, method, and URI. Press `Esc` to clear.

## Thread Detail Panel

Press `Enter` on a thread to open the detail panel:

- **Thread name**: With worker type indicator
- **Worker script**: If the thread belongs to a worker
- **State badge**: `● BUSY`, `○ IDLE`, or `◌ OTHER`
- **Request info**: HTTP method, URI, and duration (when busy)
- **Idle duration**: How long the thread has been waiting (when idle)
- **Memory**: Current usage with a sparkline trend (last 16 samples)
- **Request count**: Total requests handled

Press `Esc` to close.

## Worker Restart

Press `r` to trigger a worker restart. A confirmation prompt appears: press `y` to confirm or `n` / `Esc` to cancel.

This sends `POST /frankenphp/workers/restart` to the Caddy admin API.

> **Caution:** Restarting workers interrupts in-progress requests. Use with care in production.

## Graphs

Press `g` to toggle full-screen graphs. In addition to the standard CPU, RPS, and RSS graphs, the FrankenPHP tab includes:

- **Queue Depth**: Worker queue size over time
- **Busy Threads**: Number of busy threads over time

Graphs display the last 300 samples.

## See Also

- [Caddy Dashboard](caddy-dashboard.md)
- [CLI Reference](cli-reference.md)
- [Prometheus Export](prometheus-export.md)
