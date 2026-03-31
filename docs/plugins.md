# Plugin Development Guide

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

func (p *statsPlugin) Init(_ context.Context, _ plugin.PluginConfig) error {
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

    "github.com/alexandre-daubois/ember/internal/app"

    _ "github.com/example/ember-stats" // your plugin
)

func main() {
    if err := app.Run(os.Args[1:], "custom"); err != nil {
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

These are passed to `Init()` as `PluginConfig.Options` with lowercased keys:

```go
func (p *myPlugin) Init(ctx context.Context, cfg plugin.PluginConfig) error {
    maxRPS := cfg.Options["max_rps"]
    window := cfg.Options["window"]
    // ...
}
```

`PluginConfig` also carries `CaddyAddr`, the Caddy admin API address Ember is connected to.

## Lifecycle

1. **Registration**: `plugin.Register()` is called from `init()` at import time
2. **Initialization**: `Init(ctx, cfg)` is called before the TUI or daemon starts. If it fails, already-initialized plugins that implement `Closer` are closed in reverse order
3. **Runtime**: `Fetch` is called on every tick with a cancellable context. In TUI mode, `Update`/`View`/`HandleKey` are called from the event loop. In daemon mode (`--daemon`), only `Fetch` and `WriteMetrics` are called
4. **Shutdown**: `Close()` is called on plugins that implement the `Closer` interface, in reverse registration order

## Error Handling

Ember handles plugin errors at every stage:

**`Init` returns an error**: startup aborts. Already-initialized plugins that implement `Closer` are closed in reverse order. The error is printed to stderr.

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

func (p *cachePlugin) Init(_ context.Context, cfg plugin.PluginConfig) error {
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
    Init(ctx context.Context, cfg PluginConfig) error
}
```

### PluginConfig

```go
type PluginConfig struct {
    CaddyAddr string
    Options   map[string]string
}
```

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
