# Plugin Development Guide

> **EXPERIMENTAL**: the plugin API is not yet stable. Interfaces, method signatures, and behavior may change in any future release.
>
> Feedback is very welcome: the plugin system will evolve to match real-world needs. If something does not fit your use case, feels clunky, or you wish an interface exposed more (or less), please [open an issue](https://github.com/alexandre-daubois/ember/issues) and tell us what you are trying to build.

Ember supports compile-time plugins that let you:

- Add **custom tabs** to the TUI for visualizing metrics from additional Caddy modules (rate limiters, WAF, cache, custom middleware)
- Contribute **Prometheus metrics** to Ember's `/metrics` endpoint
- Or both

Plugins follow the same pattern as Caddy modules: blank imports + `init()` registration. There is no runtime plugin loading. Users build a custom binary that includes the plugins they need.

## Quick Start

Here is a minimal plugin that adds a "stats" tab showing a tick counter:

```go
package stats

import (
	"context"
	"fmt"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alexandre-daubois/ember/pkg/plugin"
)

func init() {
	plugin.Register(&statsPlugin{})
}

type statsPlugin struct {
	counter atomic.Int64
	count   int64
}

func (p *statsPlugin) Name() string { return "stats" }

func (p *statsPlugin) Provision(_ context.Context, _ plugin.PluginConfig) error {
	return nil
}

// Fetcher: called on every tick.
func (p *statsPlugin) Fetch(_ context.Context) (any, error) {
	return p.counter.Add(1), nil
}

// Renderer: provides a TUI tab.
func (p *statsPlugin) Update(data any, _, _ int) plugin.Renderer {
	p.count = data.(int64)
	return p
}

func (p *statsPlugin) View(_, _ int) string {
	if p.count == 0 {
		return " Waiting for data..."
	}
	return fmt.Sprintf("\n  Tick counter: %d\n", p.count)
}

func (p *statsPlugin) HandleKey(_ tea.KeyMsg) bool    { return false }
func (p *statsPlugin) StatusCount() string             { return fmt.Sprintf("%d", p.count) }
func (p *statsPlugin) HelpBindings() []plugin.HelpBinding { return nil }
```

Notice how `View` handles the "no data yet" state: before the first `Fetch` completes, Ember already calls `View` on your plugin. Always handle zero/nil data gracefully.

### Build and run

Create a small main file that imports your plugin with a blank import, then build:

```go
package main

import (
    "fmt"
    "os"

    "github.com/alexandre-daubois/ember"

    _ "github.com/example/ember-stats" // your plugin
)

func main() {
    if err := ember.Run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
```

```bash
go mod init my-ember
go mod tidy
go build -o ember-custom .
./ember-custom
```

The "stats" tab appears alongside Caddy (and FrankenPHP if detected). Press `Tab` or the corresponding number key to switch to it.

## Configuration

Plugins receive configuration via environment variables:

```
EMBER_PLUGIN_{UPPERCASED_NAME}_{KEY}=value
```

For example, a plugin named "ratelimit":

```bash
export EMBER_PLUGIN_RATELIMIT_MAX_RPS=1000
export EMBER_PLUGIN_RATELIMIT_WINDOW=60s
```

These are passed to `Provision()` as `PluginConfig.Options` with lowercased keys:

```go
func (p *myPlugin) Provision(ctx context.Context, cfg plugin.PluginConfig) error {
    maxRPS := cfg.Options["max_rps"]
    window := cfg.Options["window"]
    // ...
}
```

`PluginConfig` also carries `CaddyAddr`, the Caddy admin API address Ember is connected to. In multi-instance mode (`--addr` repeated), `Instances` carries the full list of monitored instances; see [Multi-Instance Plugins](#multi-instance-plugins).

## Lifecycle

1. **Registration**: `plugin.Register()` is called from `init()` at import time
2. **Provisioning**: `Provision(ctx, cfg)` is called before the TUI or daemon starts. A plugin whose `Provision` returns an error is logged as a warning and disabled for the rest of the session; Ember keeps running
3. **Runtime**: `Fetch` is called on every tick with a cancellable context. In TUI mode, `Update`/`View`/`HandleKey` are called from the event loop. In daemon mode (`--daemon`), only `Fetch` and `WriteMetrics` are called
4. **Shutdown**: `Close()` is called on plugins that implement the `Closer` interface, in reverse registration order

## Error Handling

Ember handles plugin errors at every stage:

**`Provision` returns an error**: the plugin is disabled for this session. Ember logs a warning identifying the plugin and the error, then continues with the remaining plugins. The plugin's `Close` (if any) is not called since no resources were confirmed to be held.

**`Fetch` returns an error**: the previous data is preserved (Ember does not overwrite it with nil). In TUI mode, the error is displayed in the tab when `View` returns an empty string. In daemon mode, the error is logged and the previous data continues to be served on `/metrics`. Fetch will be retried on the next tick.

**`Update` or `View` panics**: Ember recovers from the panic and shows "plugin error: ..." in the tab instead of crashing. The same applies to `HandleKey`, `StatusCount`, and `HelpBindings`.

**`WriteMetrics` panics**: Ember recovers and writes a `# plugin WriteMetrics panic: ...` comment line to the output. The rest of the `/metrics` response (core metrics and other plugins) is unaffected.

## Adding Prometheus Export

To expose Prometheus metrics on the `/metrics` endpoint, implement the `Exporter` interface on your plugin:

```go
func (p *statsPlugin) WriteMetrics(w io.Writer, data any, prefix string) {
    if data == nil {
        return
    }
    count := data.(int64)
    name := "stats_tick_total"
    if prefix != "" {
        name = prefix + "_" + name
    }
    fmt.Fprintf(w, "# TYPE %s counter\n", name)
    fmt.Fprintf(w, "%s %d\n", name, count)
}
```

When `--expose :9191` is passed, `curl localhost:9191/metrics | grep stats` shows the exported metric. `WriteMetrics` is called on every `/metrics` HTTP request with the latest data from `Fetch`.

## Export-only Plugins

A plugin does not need to provide a TUI tab. A `Fetcher` + `Exporter` plugin (without `Renderer`) collects data and exposes Prometheus metrics without adding any tab to the interface:

```go
package cachemetrics

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/alexandre-daubois/ember/pkg/plugin"
)

func init() {
	plugin.Register(&cachePlugin{})
}

type cachePlugin struct {
	endpoint string
}

type cacheStats struct {
	Hits   int64
	Misses int64
}

func (p *cachePlugin) Name() string { return "cache" }

func (p *cachePlugin) Provision(_ context.Context, cfg plugin.PluginConfig) error {
	p.endpoint = cfg.Options["endpoint"]
	if p.endpoint == "" {
		p.endpoint = "http://localhost:6379/stats"
	}
	return nil
}

func (p *cachePlugin) Fetch(_ context.Context) (any, error) {
	resp, err := http.Get(p.endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Parse response into cacheStats...
	return &cacheStats{Hits: 1000, Misses: 50}, nil
}

func (p *cachePlugin) WriteMetrics(w io.Writer, data any, prefix string) {
	stats, ok := data.(*cacheStats)
	if !ok || stats == nil {
		return
	}
	name := "cache_hits_total"
	if prefix != "" {
		name = prefix + "_" + name
	}
	fmt.Fprintf(w, "# TYPE %s counter\n", name)
	fmt.Fprintf(w, "%s %d\n", name, stats.Hits)

	name = "cache_misses_total"
	if prefix != "" {
		name = prefix + "_" + name
	}
	fmt.Fprintf(w, "# TYPE %s counter\n", name)
	fmt.Fprintf(w, "%s %d\n", name, stats.Misses)
}
```

## Separating the Renderer

The Quick Start example keeps everything in one struct. For plugins with complex state, you can return a separate `Renderer` from `Update` (Elm architecture):

```go
func (p *statsPlugin) Update(data any, _, _ int) plugin.Renderer {
	return &statsRenderer{count: data.(int64), ts: time.Now()}
}

// Initial View, before the first Fetch completes
func (p *statsPlugin) View(_, _ int) string { return " Waiting for data..." }

type statsRenderer struct {
	count int64
	ts    time.Time
}

func (r *statsRenderer) Update(data any, _, _ int) plugin.Renderer {
	return &statsRenderer{count: data.(int64), ts: time.Now()}
}

func (r *statsRenderer) View(_, _ int) string {
	return fmt.Sprintf("\n  Tick counter:  %d\n  Last update:   %s\n",
		r.count, r.ts.Format("15:04:05"))
}

func (r *statsRenderer) HandleKey(msg tea.KeyMsg) bool { return msg.String() == "x" }

func (r *statsRenderer) StatusCount() string {
	return fmt.Sprintf("%d ticks", r.count)
}

func (r *statsRenderer) HelpBindings() []plugin.HelpBinding {
	return []plugin.HelpBinding{{Key: "x", Desc: "Example action"}}
}
```

When you use this pattern, Ember calls `View`, `HandleKey`, etc. on the plugin struct itself until the first `Update` returns a new `Renderer`. After that, all calls go to the returned `Renderer`. This is why the plugin struct has a simple `View` that handles the "no data yet" state.

## Releasing Resources

If your plugin holds resources (connections, goroutines, file handles), implement the `Closer` interface:

```go
type Closer interface {
    Close() error
}
```

Ember calls `Close()` in reverse registration order when the application exits. If a plugin fails during initialization, already-initialized plugins that implement `Closer` are closed automatically.

## Interface Reference

All plugin interfaces live in `pkg/plugin/`.

### Plugin (required)

```go
type Plugin interface {
    Name() string
    Provision(ctx context.Context, cfg PluginConfig) error
}
```

### PluginConfig

```go
type PluginConfig struct {
    CaddyAddr string
    Instances []PluginInstance
    Options   map[string]string
}

type PluginInstance struct {
    Name string
    Addr string
}
```

`Instances` is empty in single-instance mode and populated only for plugins that opt in to [multi-instance mode](#multi-instance-plugins) via [`MultiInstancePlugin`](#multiinstanceplugin-optional). `CaddyAddr` is always set: in multi-instance mode it points to the first instance for backwards compatibility.

### Fetcher (optional)

```go
type Fetcher interface {
    Fetch(ctx context.Context) (any, error)
}
```

Called on every poll interval. The returned data is opaque to Ember core: only your own `Renderer` and `Exporter` interpret it.

### Renderer (optional)

```go
type Renderer interface {
    Update(data any, width, height int) Renderer
    View(width, height int) string
    HandleKey(msg tea.KeyMsg) bool
    StatusCount() string
    HelpBindings() []HelpBinding
}
```

- `Update`: receives the latest data from `Fetch` and terminal dimensions. Returns an updated `Renderer`
- `View`: renders the tab content as a string
- `HandleKey`: handles key presses when the plugin tab is active. Return `true` if the key was consumed
- `StatusCount`: returns a string shown as a badge in the tab bar (e.g., "12 blocked"). Empty string means no badge
- `HelpBindings`: returns keybindings shown in the `?` help overlay

### Exporter (optional)

```go
type Exporter interface {
    WriteMetrics(w io.Writer, data any, prefix string)
}
```

Writes Prometheus-format metric lines.

### HelpBinding

```go
type HelpBinding struct {
    Key  string
    Desc string
}
```

### Closer (optional)

```go
type Closer interface {
    Close() error
}
```

### MetricsSubscriber (optional)

```go
type MetricsSubscriber interface {
    OnMetrics(snap *metrics.Snapshot)
}
```

Called synchronously after each successful core fetch, before plugin `Fetch` calls. The snapshot contains Caddy and FrankenPHP metrics already parsed by Ember. The snapshot must not be modified.

Import `"github.com/alexandre-daubois/ember/pkg/metrics"` to access the `Snapshot` and `MetricsSnapshot` types.

This avoids the need for plugins to make their own `/metrics` requests to Caddy when they need access to the same core metrics Ember already collects.

#### Accessing custom metrics

`MetricsSnapshot.Extra` contains all Prometheus metric families from the `/metrics` endpoint that Ember's core parser did not consume. If your Caddy module registers custom metrics with Caddy's Prometheus collector, they will be available here as `*dto.MetricFamily` values (from `github.com/prometheus/client_model/go`):

```go
func (p *myPlugin) OnMetrics(snap *metrics.Snapshot) {
    fam, ok := snap.Metrics.Extra["mymodule_requests_total"]
    if !ok {
        return
    }
    for _, m := range fam.GetMetric() {
        // extract label values and counters as needed
    }
}
```

When there are no extra metrics, `Extra` is nil.

### MultiRenderer (optional)

```go
type TabDescriptor struct {
    Key  string
    Name string
}

type MultiRenderer interface {
    Tabs() []TabDescriptor
    RendererForTab(key string) Renderer
}
```

Implement `MultiRenderer` instead of `Renderer` when your plugin needs multiple TUI tabs. Each tab gets its own `Renderer`, but all tabs share a single `Fetch` call. `Tabs()` is called once after `Provision()` to determine the number and order of tabs. `RendererForTab` is called once per tab to create its initial `Renderer`.

If a plugin implements both `Renderer` and `MultiRenderer`, `MultiRenderer` takes priority.

### Availability (optional)

```go
type Availability interface {
    Available() bool
}
```

Implement `Availability` when your plugin's tab(s) should be shown or hidden based on runtime conditions. For example, a plugin compiled into a custom build but talking to a Caddy instance that does not have the corresponding module enabled.

`Available()` is checked after each successful `Fetch`. When it returns `false`, the plugin's tab(s) are removed from the tab bar. When it returns `true`, they are re-added. If `Available()` panics, the tab stays visible (fail-open).

### MultiInstancePlugin (optional)

```go
type MultiInstancePlugin interface {
    EmberMultiInstance()
}
```

Opt-in marker for plugins that handle Ember's multi-instance mode (`--addr` repeated). Without this marker, plugins are disabled when more than one `--addr` is passed and a warning is logged. See [Multi-Instance Plugins](#multi-instance-plugins) below for the full contract.

### TabAvailability (optional)

```go
type TabAvailability interface {
    TabAvailable(key string) bool
}
```

Implement `TabAvailability` in a `MultiRenderer` plugin when individual tabs should be shown or hidden independently. For example, a WAF plugin with "Rules" and "Analytics" tabs can hide the "Analytics" tab when the analytics module is not active on the Caddy instance.

`TabAvailable(key)` is checked after each successful `Fetch` for every tab key returned by `Tabs()`. When it returns `false` for a key, that tab is removed from the tab bar. When it returns `true`, the tab is re-added. If `TabAvailable` panics, the tab stays visible (fail-open).

If a plugin also implements `Availability`, it acts as a master switch: when `Available()` returns `false`, all tabs are hidden regardless of `TabAvailable` results. When `Available()` returns `true`, `TabAvailable` controls each tab individually.

`TabAvailability` is ignored for single-Renderer plugins (there is only one tab, so `Availability` is sufficient).

## Multi-Instance Plugins

When Ember is launched with several `--addr` flags (`--daemon` or `--json` modes only), it polls and aggregates metrics from multiple Caddy instances behind a single endpoint. By default, plugins are disabled in this mode with a warning, since most are written assuming a single Caddy. To opt in, add the `EmberMultiInstance` marker:

```go
type myPlugin struct{}

func (p *myPlugin) EmberMultiInstance() {} // marker only, never called
```

### What changes for the plugin

- **Provision** is still called once per session. `PluginConfig.Instances` now lists every monitored instance (`Name`, `Addr`); `CaddyAddr` is set to the first instance for backwards compatibility but multi-aware plugins should read `Instances`.
- **Fetch** is called **once per instance, per tick**. The active instance is carried on the context. Read it with `plugin.InstanceFromContext`:

  ```go
  func (p *myPlugin) Fetch(ctx context.Context) (any, error) {
      inst, _ := plugin.InstanceFromContext(ctx)
      // inst.Name, inst.Addr identify the instance being polled right now
      return queryMyBackend(inst.Addr)
  }
  ```

  Per-instance fetches run in parallel; the plugin must not assume a single shared `Fetch` state. Each instance gets its own data passed to `WriteMetrics`.

- **WriteMetrics** is called once per instance with that instance's data. Emit metrics as if the plugin only knew about one Caddy: Ember automatically injects `ember_instance="<name>"` into every emitted line on the way out. `# HELP` and `# TYPE` directives are also deduplicated across instances so the resulting `/metrics` text stays valid.

  ```go
  func (p *myPlugin) WriteMetrics(w io.Writer, data any, prefix string) {
      // Just write your metrics naked; Ember adds ember_instance="..." for you.
      fmt.Fprintln(w, "# HELP my_plugin_requests_total ...")
      fmt.Fprintln(w, "# TYPE my_plugin_requests_total counter")
      fmt.Fprintf(w, "my_plugin_requests_total %d\n", count)
  }
  ```

- **OnMetrics** (if the plugin implements `MetricsSubscriber`) is also called once per instance per tick, in parallel across instances. The snapshot passed in does **not** carry the source instance: there is no instance identity available in `OnMetrics`. If you need a per-instance reaction to core metrics, do it in `Fetch` (where `InstanceFromContext` is available) rather than in `OnMetrics`, and protect any shared state with a mutex since calls overlap.

### Single-instance behaviour is unchanged

Without the marker, the plugin is disabled in multi-instance mode (warning logged) and behaves exactly as before in single-instance mode. With the marker, single-instance mode is also unchanged: `Instances` is empty, the `Fetch` context carries no `PluginInstance`, and emitted metrics are not labelled.

## Reusing Prometheus Parsing

The `pkg/metrics` package exposes the same Prometheus text parser that Ember uses internally:

```go
import "github.com/alexandre-daubois/ember/pkg/metrics"

snap, err := metrics.ParsePrometheus(reader)
```

This returns a `MetricsSnapshot` with all Caddy and FrankenPHP metrics parsed. The package also exposes all the data types (`Snapshot`, `MetricsSnapshot`, `HostMetrics`, `WorkerMetrics`, etc.) for plugins that need to work with core metrics.

## Reserved Keybindings

The following keys are handled by Ember core and **never** reach your plugin's `HandleKey`:

| Key                      | Action                |
|--------------------------|-----------------------|
| `q`, `Ctrl+C`           | Quit                  |
| `Tab`                    | Switch tab            |
| `1`-`9`                 | Jump to tab by number |
| `?`                      | Toggle help overlay   |
| `g`                      | Toggle graphs         |
| `p`                      | Pause / resume        |

The following keys are used by core tabs (Caddy, FrankenPHP) but **forwarded** to your plugin when its tab is active:

| Key                      | Core tab behavior                |
|--------------------------|----------------------------------|
| `↑` / `↓` / `j` / `k`  | Navigate list                    |
| `Home` / `End`           | Jump to first / last             |
| `PgUp` / `PgDn`         | Page navigation                  |
| `s` / `S`               | Cycle sort field                 |
| `Enter`                  | Open detail panel                |
| `/`                      | Open filter                      |
| `r`                      | Restart workers (FrankenPHP)     |

All other keys reach your plugin's `HandleKey` directly.

## Plugin Name Rules

Plugin names must:
- Not be empty
- Not contain whitespace (spaces, tabs, newlines)
- Not contain underscores (use hyphens instead)
- Be unique across all registered plugins
- Be distinguishable after hyphen removal: names like `my-plugin` and `myplugin` map to the same environment variable prefix (`EMBER_PLUGIN_MYPLUGIN_`), so they must not coexist

`Register()` panics at startup if any of these rules are violated.

## Panic Safety

Ember wraps all plugin calls (`Fetch`, `Update`, `View`, `HandleKey`, `StatusCount`, `HelpBindings`, `WriteMetrics`) with panic recovery. If your plugin panics, Ember displays an error in the tab instead of crashing. For `WriteMetrics`, a comment line is written to the output instead of crashing the `/metrics` endpoint.
