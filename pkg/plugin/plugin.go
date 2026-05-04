// Package plugin defines the interfaces for building Ember plugins.
//
// EXPERIMENTAL: the plugin API is not yet stable. Interfaces, method
// signatures, and behavior may change in any future release. Feedback is
// very welcome; please open an issue on the Ember repository if something
// does not fit your use case so the API can evolve with real needs.
//
// Plugins extend Ember with custom TUI tabs, Prometheus metrics, or both.
// They are compiled into the binary using Go's blank import pattern (the same
// approach used by Caddy). There is no runtime plugin loading.
//
// A plugin must implement [Plugin] (Name + Provision). It can optionally implement
// any combination of [Fetcher], [Renderer]/[MultiRenderer], and [Exporter]:
//
//   - Fetcher + Renderer: custom TUI tab (data collection + visualization)
//   - Fetcher + MultiRenderer: multiple custom TUI tabs sharing one data source
//   - Fetcher + Exporter: headless metrics export on /metrics
//   - Fetcher + Renderer + Exporter: TUI tab + metrics export
//
// Register plugins from init() functions:
//
//	func init() {
//	    plugin.Register(&myPlugin{})
//	}
//
// See the Plugin Development Guide (docs/plugins.md) for a full walkthrough.
package plugin

import (
	"context"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alexandre-daubois/ember/pkg/metrics"
)

// Plugin is the minimal interface every plugin must implement.
// Name returns a unique identifier used in the tab bar and for environment
// variable configuration (EMBER_PLUGIN_{NAME}_{KEY}).
// Provision is called once before the TUI or daemon starts. If it returns
// an error, Ember logs a warning and disables the plugin, then continues
// without it; the rest of Ember and any other plugins keep running.
type Plugin interface {
	Name() string
	Provision(ctx context.Context, cfg PluginConfig) error
}

// PluginConfig carries configuration passed to a plugin during Provision.
// CaddyAddr is the Caddy admin API address Ember is connected to. In
// multi-instance mode it is set to the first instance's URL for backwards
// compatibility; multi-aware plugins should read Instances instead.
// Instances is populated only in multi-instance mode (--addr repeated) for
// plugins implementing [MultiInstancePlugin]; it is empty otherwise.
// Options contains environment variables matching the plugin's name
// (e.g., EMBER_PLUGIN_RATELIMIT_MAX_RPS=1000 becomes Options["max_rps"]="1000").
type PluginConfig struct {
	CaddyAddr string
	Instances []PluginInstance
	Options   map[string]string
}

// PluginInstance describes one Caddy instance Ember is monitoring. Multi-aware
// plugins receive the full list via [PluginConfig.Instances] at Provision time
// and one Fetch call per instance carrying the active PluginInstance on the
// context (see [InstanceFromContext]).
type PluginInstance struct {
	Name string
	Addr string
}

// MultiInstancePlugin is an opt-in marker for plugins that handle multi-instance
// mode. Without this marker, plugins are disabled when --addr is repeated and
// a warning is logged. Implementing the marker indicates the plugin handles
// per-instance Fetch calls (the active [PluginInstance] is carried on the
// context via [InstanceFromContext]) and emits per-instance data; Ember
// automatically labels emitted metrics with ember_instance="<name>".
//
// The EmberMultiInstance method is a no-op tag; it must exist so the interface
// stays distinct from [Plugin].
type MultiInstancePlugin interface {
	EmberMultiInstance()
}

type instanceCtxKey struct{}

// WithInstance returns a context carrying the given PluginInstance, so a
// [MultiInstancePlugin] Fetch can identify the instance it is being asked
// about. Used by Ember internals; plugins read it back via [InstanceFromContext].
func WithInstance(parent context.Context, inst PluginInstance) context.Context {
	return context.WithValue(parent, instanceCtxKey{}, inst)
}

// InstanceFromContext returns the PluginInstance attached by Ember when calling
// Fetch on a [MultiInstancePlugin]. It returns ok=false in single-instance mode
// or for plugins that do not implement MultiInstancePlugin.
func InstanceFromContext(ctx context.Context) (PluginInstance, bool) {
	v, ok := ctx.Value(instanceCtxKey{}).(PluginInstance)
	return v, ok
}

// Fetcher is implemented by plugins that collect data on every poll interval.
// The returned data is opaque to Ember core: only the plugin's own [Renderer]
// and [Exporter] interpret it.
//
// When Fetch returns an error:
//   - In TUI mode, the previous data is preserved and the error is shown
//     in the tab when View returns an empty string
//   - In daemon mode, the error is logged and previous data continues
//     to be exported on /metrics
//
// Ember recovers from panics in Fetch and converts them to errors.
// The context is cancelled when the application shuts down.
type Fetcher interface {
	Fetch(ctx context.Context) (any, error)
}

// Renderer is implemented by plugins that provide a TUI tab.
//
// Before the first [Fetcher.Fetch] completes, Ember calls View, HandleKey, etc.
// on the object that originally implements Renderer (typically the plugin struct
// itself). Make sure these methods handle the "no data yet" state gracefully.
// After the first successful Update call, the returned Renderer is used for all
// subsequent calls.
type Renderer interface {
	// Update receives the latest data from Fetch and the current terminal
	// dimensions. It returns an updated Renderer (Elm architecture).
	// Return nil to keep the current Renderer unchanged.
	Update(data any, width, height int) Renderer

	// View renders the tab content as a string.
	View(width, height int) string

	// HandleKey handles key presses when this plugin's tab is active.
	// Return true if the key was consumed.
	HandleKey(msg tea.KeyMsg) bool

	// StatusCount returns a short string shown as a badge in the tab bar
	// (e.g., "12 blocked"). Return "" for no badge.
	StatusCount() string

	// HelpBindings returns keybindings displayed in the ? help overlay.
	HelpBindings() []HelpBinding
}

// HelpBinding describes a single keybinding shown in the help overlay.
type HelpBinding struct {
	Key  string
	Desc string
}

// Exporter is implemented by plugins that contribute Prometheus metrics
// to Ember's /metrics endpoint.
//
// WriteMetrics is called on every /metrics HTTP request with the latest data
// from [Fetcher.Fetch]. Write Prometheus-format text lines to w.
// Use prefix to namespace metric names (e.g., prefix + "_my_metric").
// When prefix is empty, emit unqualified names.
//
// Ember recovers from panics in WriteMetrics and writes a comment line
// to the output instead of crashing the endpoint.
type Exporter interface {
	WriteMetrics(w io.Writer, data any, prefix string)
}

// Closer is optionally implemented by plugins that hold resources
// (connections, goroutines, file handles) requiring cleanup.
// Ember calls Close in reverse registration order at shutdown.
// If a plugin's Provision returns an error, Close is not called on it;
// other already-provisioned plugins implementing Closer are closed
// normally at shutdown.
type Closer interface {
	Close() error
}

// MetricsSubscriber is optionally implemented by plugins that want to receive
// the core metrics snapshot on every successful poll cycle. This avoids the
// need for plugins to make their own /metrics requests to Caddy.
//
// OnMetrics is called synchronously after each successful core fetch, before
// plugin Fetch calls begin. The snapshot must not be modified.
//
// In multi-instance mode, OnMetrics is invoked once per instance per tick, in
// parallel across instances, with each call carrying that instance's snapshot.
// The snapshot itself does not identify its source instance: a plugin that
// also implements [MultiInstancePlugin] and needs to react per-instance is
// expected to do its bookkeeping in [Fetcher.Fetch] (where [InstanceFromContext]
// is available) rather than in OnMetrics, and to guard any shared state
// against concurrent calls.
type MetricsSubscriber interface {
	OnMetrics(snap *metrics.Snapshot)
}

// TabDescriptor describes a single tab provided by a [MultiRenderer] plugin.
// Name is displayed in the tab bar. Key is a stable identifier used
// internally (e.g., "bouncer", "appsec"). Key must be unique within the plugin.
type TabDescriptor struct {
	Key  string
	Name string
}

// MultiRenderer is implemented by plugins that provide multiple TUI tabs.
// Each tab gets its own [Renderer], but all tabs share the same [Fetcher] data.
//
// Tabs returns the list of tabs this plugin provides. It is called once
// after Init. The order determines the tab order in the tab bar.
//
// RendererForTab returns the initial Renderer for the given tab key.
// It is called once per tab after Init.
//
// A plugin should implement either [Renderer] or MultiRenderer, not both.
// If both are present, MultiRenderer takes priority.
type MultiRenderer interface {
	Tabs() []TabDescriptor
	RendererForTab(key string) Renderer
}

// Availability is optionally implemented by plugins whose tab(s) should
// be shown or hidden based on runtime conditions. Ember calls Available
// after each successful Fetch. When Available returns false, the plugin's
// tab(s) are removed from the tab bar. When it returns true, they are
// re-added.
//
// Plugins that do not implement Availability are always visible.
type Availability interface {
	Available() bool
}

// TabAvailability is optionally implemented by [MultiRenderer] plugins that
// need per-tab visibility control. Ember calls TabAvailable for each tab key
// after every successful Fetch. When TabAvailable returns false for a key,
// that specific tab is removed from the tab bar. When it returns true, the
// tab is re-added.
//
// If a plugin also implements [Availability], it acts as a master switch:
// when Available returns false, all tabs are hidden regardless of
// TabAvailable results. When Available returns true, TabAvailable controls
// each tab individually.
//
// TabAvailability is ignored for single-Renderer plugins.
// If TabAvailable panics, the tab stays visible (fail-open).
type TabAvailability interface {
	TabAvailable(key string) bool
}

// PluginExport holds the data needed to export metrics for a single plugin.
// Used internally by Ember to pass plugin data to the /metrics handler.
type PluginExport struct {
	Exporter Exporter
	Data     any
}

// SafeFetch calls f.Fetch and recovers from panics, converting them to errors.
func SafeFetch(ctx context.Context, f Fetcher) (data any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugin panic during Fetch: %v", r)
		}
	}()
	return f.Fetch(ctx)
}

// SafeOnMetrics calls sub.OnMetrics and swallows any panic so a misbehaving
// subscriber cannot take Ember down. Errors are intentionally dropped because
// OnMetrics has no return value: the contract is fire-and-forget.
func SafeOnMetrics(sub MetricsSubscriber, snap *metrics.Snapshot) {
	defer func() {
		_ = recover()
	}()
	sub.OnMetrics(snap)
}
