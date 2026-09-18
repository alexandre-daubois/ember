# Mercure Dashboard

The Mercure tab appears when Ember detects a [Mercure](https://mercure.rocks) hub on the monitored Caddy. The official Mercure.rocks hub is a Caddy build with the `mercure` module, so Ember talks to it exactly as it talks to any Caddy: through the admin API (`localhost:2019` by default). Nothing to configure on the hub side.

Detection relies on the `mercure_*` families the module registers on Caddy's `/metrics` endpoint. They are present as soon as the hub is loaded, even before the first subscriber connects.

## Hub-only mode

A hub deployed on its own usually runs without the `metrics` global option, so Caddy exposes no HTTP traffic metrics. In that case Ember trims the interface:

- The Caddy tab is removed (it would only show zeroes) and Ember lands on the Mercure tab.
- The dashboard header replaces the RPS / Avg / In-flight line with the hub counters: connected subscribers with a sparkline, new subscribers per second, updates per second, and the total subscriber count.
- The full-screen graphs (`g`) drop the RPS panel and add **Subscribers** and **Updates** panels.
- The Caddy Config, Certificates and Logs tabs stay available: they work on a hub like on any Caddy.

Add `metrics` to `GLOBAL_OPTIONS` (or to the Caddyfile global block) and the Caddy tab comes back on the next poll, with the hub's own HTTP traffic (SSE connections show up as in-flight requests). When FrankenPHP is detected alongside Mercure, the Caddy tab is always kept.

## Hub activity

| Row | Source |
|-----|--------|
| **Subscribers** | `mercure_subscribers_connected`, with a sparkline of the last samples |
| **New subscribers/s** | Rate derived from `mercure_subscribers_total` between two polls |
| **Updates/s** | Rate derived from `mercure_updates_total` between two polls, with a sparkline |
| **Subscribers total** | `mercure_subscribers_total` |
| **Updates total** | `mercure_updates_total` |

A hub restart resets its counters. Ember detects the backwards step and drops the previous sample instead of showing a negative rate.

## Hub configuration

The lower section is read from the `mercure` handler in the live Caddy config (`GET /config/`), so it needs no token. One block is shown per handler when several hubs are configured.

| Row | Description |
|-----|-------------|
| **Name** | The hub's `name` directive (hidden when unset) |
| **Transport** | Transport module (`bolt`, `local`, or a third-party name). Defaults to `bolt` when nothing is configured |
| **Anonymous** | Whether subscribers without a JWT are accepted |
| **Subscriptions API** | Whether the hub dispatches subscription events |
| **Debugger** | Whether the debugger UI is enabled (implied by the playground) |
| **Playground** | Whether the insecure playground is on. Flagged as dev only, since it mints an all-access token |
| **Heartbeat**, **Write timeout**, **Dispatch timeout** | Configured values, or the hub defaults tagged `(default)` |
| **Protocol compat** | The `protocol_version_compatibility` value when set |
| **Issuers** | Trusted issuer identifiers. Hubs still on the deprecated `publisher_jwt` / `subscriber_jwt` directives show `legacy` with the signing algorithms. Keys are never displayed |
| **CORS origins**, **Publish origins**, **Public URLs** | The configured lists, `—` when empty |

Press `r` to re-read the config from Caddy.

## Following the updates

The three counters say how much the hub works, not what goes through it. Press `m` on the Mercure tab (or select **Mercure** in the Logs sidepanel) to land on **Logs › Mercure**: one row per update the hub dispatched, newest first.

| Column | Description |
|--------|-------------|
| **Time** | When the hub published the update |
| **Topic** | The canonical topic, with `+n` when the update carries alternate topics |
| **Type** | The SSE event type, `-` when the publisher set none |
| **Priv** | `yes` for a private update |
| **Size** | Payload size |
| **Sent** | How many subscribers received it so far |
| **Data** | The payload on one line, line breaks shown as `⏎` |

`Enter` opens the detail panel with every topic, the update ID and the whole payload, re-indented when it is JSON. The list behaves like the other log views: `↑`/`↓` freezes it, `f` or `p` resume the live tail, `/` filters on topics, type and payload, `c` clears it.

**By Topic**, the child row, aggregates the session by topic pattern. Per-resource topics fold together the way By Route folds URIs: `https://example.com/books/12` counts under `https://example.com/books/:id`, and the **Topics** column tells how many distinct topics sit behind the pattern. `s`/`S` sort by updates, pattern, recipients or last activity.

### How the capture works

The hub only logs the updates it dispatches at DEBUG level, so while one of the two Mercure rows is selected Ember hot-registers a third log sink, `__ember_mercure__`, limited to the `http.handlers.mercure` logger at that level, and removes it as soon as you move to another row, another tab, or quit. The header shows `● capturing` while the sink is in place. Nothing is captured, and nothing is asked of the hub, the rest of the time.

Two consequences worth knowing:

- The hub writes one line per update and one per recipient while the capture is on. On a hub with thousands of subscribers this is real work: leave the row when you are done reading.
- Private updates are shown with their payload. The capture needs the same admin API access as the rest of Ember, but the payloads may hold personal data, so mind who reads your terminal.

The capture rides on the log listener, so it needs the same conditions as the Logs tab: a local Caddy, or `--log-listen` reachable from Caddy. With `--stdin-logs`, Ember cannot install the sink, but it still shows any `http.handlers.mercure` DEBUG lines it finds in the piped stream.

## Keybindings

| Key | Action |
|-----|--------|
| `m` | Jump to Logs › Mercure, the live list of updates |
| `p` | Pause / resume polling |
| `r` | Refresh the hub configuration |
| `g` | Toggle full-screen graphs |
| `?` | Help overlay |
| `Tab` / `Shift+Tab` | Switch tab |

## Other modes

- `ember status` appends `Mercure <n> subscribers | <rate> updates/s`, and the `--json` form gains a `mercure` object. A hub without HTTP metrics counts as reachable.
- `ember --json` carries the raw counters under `metrics.mercure` and the rates under `derived.mercure`. See [JSON Output](json-output.md).
- `--expose` / `--daemon` re-export the three hub counters and the two derived rates. See [Prometheus Export](prometheus-export.md).

## See Also

- [Caddy Dashboard](caddy-dashboard.md)
- [Caddy Configuration](caddy-configuration.md)
- [CLI Reference](cli-reference.md)
