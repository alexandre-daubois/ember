package ui

import (
	"context"
	"io"
	"testing"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/pkg/plugin"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubPlugin struct {
	name string
}

func (p *stubPlugin) Name() string                                        { return p.name }
func (p *stubPlugin) Init(_ context.Context, _ plugin.PluginConfig) error { return nil }
func (p *stubPlugin) Fetch(_ context.Context) (any, error)                { return "data", nil }
func (p *stubPlugin) Update(data any, _, _ int) plugin.Renderer           { return p }
func (p *stubPlugin) View(w, _ int) string                                { return "rendered" }
func (p *stubPlugin) HandleKey(_ tea.KeyMsg) bool                         { return false }
func (p *stubPlugin) StatusCount() string                                 { return "5 items" }
func (p *stubPlugin) HelpBindings() []plugin.HelpBinding                  { return nil }
func (p *stubPlugin) WriteMetrics(_ io.Writer, _ any, _ string)           {}

type panicPlugin struct {
	stubPlugin
}

func (p *panicPlugin) Fetch(_ context.Context) (any, error) { panic("fetch boom") }
func (p *panicPlugin) Update(_ any, _, _ int) plugin.Renderer {
	panic("update boom")
}
func (p *panicPlugin) View(_, _ int) string        { panic("view boom") }
func (p *panicPlugin) HandleKey(_ tea.KeyMsg) bool { panic("key boom") }
func (p *panicPlugin) StatusCount() string         { panic("count boom") }
func (p *panicPlugin) HelpBindings() []plugin.HelpBinding {
	panic("help boom")
}

func TestNewPluginTabs_SingleRenderer(t *testing.T) {
	p := &stubPlugin{name: "test"}
	pts, g := newPluginTabs(p, 100)

	require.Len(t, pts, 1)
	assert.Equal(t, tab(100), pts[0].tabID)
	assert.Equal(t, "test", pts[0].tabName)
	assert.NotNil(t, pts[0].renderer)
	assert.NotNil(t, g.fetcher)
	assert.NotNil(t, g.exporter)
	assert.Same(t, g, pts[0].group)
}

func TestNewPluginTabs_MinimalPlugin(t *testing.T) {
	p := &minimalPlugin{name: "minimal"}
	pts, g := newPluginTabs(p, 100)

	assert.Empty(t, pts)
	assert.Nil(t, g.fetcher)
	assert.Nil(t, g.exporter)
}

type minimalPlugin struct{ name string }

func (p *minimalPlugin) Name() string                                        { return p.name }
func (p *minimalPlugin) Init(_ context.Context, _ plugin.PluginConfig) error { return nil }

func TestSafePluginFetch(t *testing.T) {
	p := &stubPlugin{name: "ok"}
	data, err := plugin.SafeFetch(context.Background(), p)
	assert.NoError(t, err)
	assert.Equal(t, "data", data)
}

func TestSafePluginFetchPanic(t *testing.T) {
	p := &panicPlugin{stubPlugin: stubPlugin{name: "panic"}}
	data, err := plugin.SafeFetch(context.Background(), p)
	assert.Nil(t, data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin panic during Fetch")
}

func TestSafePluginUpdate(t *testing.T) {
	p := &stubPlugin{name: "ok"}
	r, err := safePluginUpdate(p, "data", 80, 24)
	assert.NoError(t, err)
	assert.NotNil(t, r)
}

func TestSafePluginUpdatePanic(t *testing.T) {
	p := &panicPlugin{stubPlugin: stubPlugin{name: "panic"}}
	_, err := safePluginUpdate(p, "data", 80, 24)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin panic during Update")
}

func TestSafePluginView(t *testing.T) {
	p := &stubPlugin{name: "ok"}
	s := safePluginView(p, 80, 24)
	assert.Equal(t, "rendered", s)
}

func TestSafePluginViewPanic(t *testing.T) {
	p := &panicPlugin{stubPlugin: stubPlugin{name: "panic"}}
	s := safePluginView(p, 80, 24)
	assert.Contains(t, s, "plugin error")
}

func TestSafePluginHandleKey(t *testing.T) {
	p := &stubPlugin{name: "ok"}
	consumed, err := safePluginHandleKey(p, tea.KeyMsg{})
	assert.NoError(t, err)
	assert.False(t, consumed)
}

func TestSafePluginHandleKeyPanic(t *testing.T) {
	p := &panicPlugin{stubPlugin: stubPlugin{name: "panic"}}
	_, err := safePluginHandleKey(p, tea.KeyMsg{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin panic during HandleKey")
}

func TestSafePluginStatusCount(t *testing.T) {
	p := &stubPlugin{name: "ok"}
	s, err := safePluginStatusCount(p)
	assert.NoError(t, err)
	assert.Equal(t, "5 items", s)
}

func TestSafePluginStatusCountPanic(t *testing.T) {
	p := &panicPlugin{stubPlugin: stubPlugin{name: "panic"}}
	s, err := safePluginStatusCount(p)
	assert.Equal(t, "", s)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin panic during StatusCount")
}

func TestSafePluginHelpBindings(t *testing.T) {
	p := &stubPlugin{name: "ok"}
	hb, err := safePluginHelpBindings(p)
	assert.NoError(t, err)
	assert.Nil(t, hb)
}

func TestSafePluginHelpBindingsPanic(t *testing.T) {
	p := &panicPlugin{stubPlugin: stubPlugin{name: "panic"}}
	hb, err := safePluginHelpBindings(p)
	assert.Nil(t, hb)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin panic during HelpBindings")
}

func TestDoPluginFetchCmd(t *testing.T) {
	p := &stubPlugin{name: "ok"}
	cmd := doPluginFetch(context.Background(), 0, p)
	msg := cmd()
	fm, ok := msg.(pluginFetchMsg)
	require.True(t, ok)
	assert.Equal(t, 0, fm.groupIndex)
	assert.Equal(t, "data", fm.data)
	assert.NoError(t, fm.err)
}

func TestDoPluginFetchCmdPanic(t *testing.T) {
	p := &panicPlugin{stubPlugin: stubPlugin{name: "panic"}}
	cmd := doPluginFetch(context.Background(), 1, p)
	msg := cmd()
	fm, ok := msg.(pluginFetchMsg)
	require.True(t, ok)
	assert.Equal(t, 1, fm.groupIndex)
	assert.Error(t, fm.err)
}

type exporterOnlyStub struct {
	name string
}

func (p *exporterOnlyStub) Name() string                                        { return p.name }
func (p *exporterOnlyStub) Init(_ context.Context, _ plugin.PluginConfig) error { return nil }
func (p *exporterOnlyStub) Fetch(_ context.Context) (any, error)                { return "metrics-data", nil }
func (p *exporterOnlyStub) WriteMetrics(_ io.Writer, _ any, _ string)           {}

func TestNewPluginTabs_ExporterOnly(t *testing.T) {
	p := &exporterOnlyStub{name: "exporter-only"}
	pts, g := newPluginTabs(p, 100)

	assert.Empty(t, pts)
	assert.NotNil(t, g.fetcher)
	assert.NotNil(t, g.exporter)
}

func TestNewApp_IncludesExporterOnlyPlugins(t *testing.T) {
	renderer := &stubPlugin{name: "with-renderer"}
	exporterOnly := &exporterOnlyStub{name: "exporter-only"}

	cfg := Config{
		Plugins: []plugin.Plugin{renderer, exporterOnly},
	}

	app := NewApp(nil, cfg)

	assert.Len(t, app.pluginTabs, 1, "only renderer plugin should be in pluginTabs")
	assert.Len(t, app.pluginGroups, 2, "both plugins should be in pluginGroups")
	assert.Len(t, app.tabs, 4, "Caddy + Config + Certificates + renderer plugin should be in tabs")
	assert.Equal(t, tabCaddy, app.tabs[0])
	assert.Equal(t, tabConfig, app.tabs[1])
	assert.Equal(t, tabCertificates, app.tabs[2])
	assert.Equal(t, tabPluginBase, app.tabs[3])
}

func TestPluginExports_IncludesExporterOnly(t *testing.T) {
	exporterOnly := &exporterOnlyStub{name: "exporter-only"}

	cfg := Config{
		Plugins: []plugin.Plugin{exporterOnly},
	}

	app := NewApp(nil, cfg)
	app.pluginGroups[0].data = "some-data"

	exports := app.pluginExports()
	require.Len(t, exports, 1)
	assert.Equal(t, "some-data", exports[0].Data)
	assert.NotNil(t, exports[0].Exporter)
}

func TestPluginExports_MixedPlugins(t *testing.T) {
	renderer := &stubPlugin{name: "with-renderer"}
	exporterOnly := &exporterOnlyStub{name: "exporter-only"}

	cfg := Config{
		Plugins: []plugin.Plugin{renderer, exporterOnly},
	}

	app := NewApp(nil, cfg)
	app.pluginGroups[0].data = "renderer-data"
	app.pluginGroups[1].data = "exporter-data"

	exports := app.pluginExports()
	require.Len(t, exports, 2)
}

func TestPluginExports_Empty(t *testing.T) {
	app := NewApp(nil, Config{})
	exports := app.pluginExports()
	assert.Nil(t, exports)
}

func TestDoPluginFetches_SkipsWhenAlreadyFetching(t *testing.T) {
	p := &stubPlugin{name: "test"}
	cfg := Config{Plugins: []plugin.Plugin{p}}
	app := NewApp(nil, cfg)

	cmds := app.doPluginFetches()
	assert.Len(t, cmds, 1, "first call should return a fetch cmd")

	cmds = app.doPluginFetches()
	assert.Empty(t, cmds, "second call should return nothing while still fetching")
}

func TestDoPluginFetches_ResumesAfterFetchComplete(t *testing.T) {
	p := &stubPlugin{name: "test"}
	cfg := Config{Plugins: []plugin.Plugin{p}}
	app := NewApp(nil, cfg)

	app.doPluginFetches()
	app.pluginGroups[0].fetching = false

	cmds := app.doPluginFetches()
	assert.Len(t, cmds, 1, "should fetch again after previous fetch completed")
}

func TestDoPluginFetches_IncludesExporterOnly(t *testing.T) {
	exporterOnly := &exporterOnlyStub{name: "exporter-only"}

	cfg := Config{
		Plugins: []plugin.Plugin{exporterOnly},
	}

	app := NewApp(nil, cfg)
	cmds := app.doPluginFetches()
	assert.Len(t, cmds, 1, "exporter-only plugin with Fetcher should produce a fetch cmd")
}

func TestSafePluginAvailable(t *testing.T) {
	t.Run("returns true", func(t *testing.T) {
		a := &availPlugin{available: true}
		assert.True(t, safePluginAvailable(a))
	})

	t.Run("returns false", func(t *testing.T) {
		a := &availPlugin{available: false}
		assert.False(t, safePluginAvailable(a))
	})

	t.Run("panic returns true (fail-open)", func(t *testing.T) {
		a := &panicAvailPlugin{}
		assert.True(t, safePluginAvailable(a))
	})
}

type availPlugin struct {
	available bool
}

func (a *availPlugin) Available() bool { return a.available }

type panicAvailPlugin struct{}

func (p *panicAvailPlugin) Available() bool { panic("avail boom") }

type multiRendererPlugin struct {
	stubPlugin
	tabs []plugin.TabDescriptor
}

func (p *multiRendererPlugin) Tabs() []plugin.TabDescriptor { return p.tabs }
func (p *multiRendererPlugin) RendererForTab(key string) plugin.Renderer {
	return &stubPlugin{name: key}
}

func TestNewPluginTabs_MultiRenderer(t *testing.T) {
	p := &multiRendererPlugin{
		stubPlugin: stubPlugin{name: "multi"},
		tabs: []plugin.TabDescriptor{
			{Key: "overview", Name: "My Module Overview"},
			{Key: "details", Name: "My Module Details"},
		},
	}

	pts, g := newPluginTabs(p, 100)

	require.Len(t, pts, 2)
	assert.Equal(t, tab(100), pts[0].tabID)
	assert.Equal(t, "My Module Overview", pts[0].tabName)
	assert.Equal(t, tab(101), pts[1].tabID)
	assert.Equal(t, "My Module Details", pts[1].tabName)
	assert.Same(t, g, pts[0].group)
	assert.Same(t, g, pts[1].group)
}

func TestNewPluginTabs_MultiRendererPriority(t *testing.T) {
	// MultiRenderer takes priority over Renderer
	p := &multiRendererPlugin{
		stubPlugin: stubPlugin{name: "both"},
		tabs: []plugin.TabDescriptor{
			{Key: "tab1", Name: "Tab 1"},
		},
	}

	pts, _ := newPluginTabs(p, 100)
	require.Len(t, pts, 1)
	assert.Equal(t, "Tab 1", pts[0].tabName)
}

func TestNewPluginTabs_AvailabilityDetected(t *testing.T) {
	p := &availRendererPlugin{
		stubPlugin: stubPlugin{name: "avail"},
		available:  true,
	}
	_, g := newPluginTabs(p, 100)
	assert.NotNil(t, g.avail)
	assert.True(t, g.wasAvail)
}

type availRendererPlugin struct {
	stubPlugin
	available bool
}

func (p *availRendererPlugin) Available() bool { return p.available }

func TestSafeOnMetrics(t *testing.T) {
	t.Run("normal call", func(t *testing.T) {
		sub := &metricsSubPlugin{}
		snap := &fetcher.Snapshot{}
		assert.NotPanics(t, func() { safeOnMetrics(sub, snap) })
		assert.True(t, sub.called)
	})

	t.Run("panic recovery", func(t *testing.T) {
		sub := &panicMetricsSubPlugin{}
		snap := &fetcher.Snapshot{}
		assert.NotPanics(t, func() { safeOnMetrics(sub, snap) })
	})
}

type metricsSubPlugin struct {
	called bool
}

func (p *metricsSubPlugin) OnMetrics(_ *fetcher.Snapshot) { p.called = true }

type panicMetricsSubPlugin struct{}

func (p *panicMetricsSubPlugin) OnMetrics(_ *fetcher.Snapshot) { panic("onmetrics boom") }

func TestUpdatePluginTabVisibility(t *testing.T) {
	p := &stubPlugin{name: "vis"}
	cfg := Config{Plugins: []plugin.Plugin{p}}
	app := NewApp(nil, cfg)

	require.Contains(t, app.tabs, app.pluginTabs[0].tabID)

	// hide
	app.updatePluginTabVisibility(app.pluginGroups[0], false)
	assert.NotContains(t, app.tabs, app.pluginTabs[0].tabID)

	// show
	app.updatePluginTabVisibility(app.pluginGroups[0], true)
	assert.Contains(t, app.tabs, app.pluginTabs[0].tabID)
}

func TestUpdatePluginTabVisibility_SwitchesActiveTab(t *testing.T) {
	p := &stubPlugin{name: "active"}
	cfg := Config{Plugins: []plugin.Plugin{p}}
	app := NewApp(nil, cfg)

	app.switchTab(app.pluginTabs[0].tabID)
	assert.Equal(t, app.pluginTabs[0].tabID, app.activeTab)

	app.updatePluginTabVisibility(app.pluginGroups[0], false)
	assert.Equal(t, tabCaddy, app.activeTab, "should switch to Caddy when active tab is hidden")
}
