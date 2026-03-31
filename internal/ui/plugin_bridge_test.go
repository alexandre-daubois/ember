package ui

import (
	"context"
	"io"
	"testing"

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

func TestNewPluginTab(t *testing.T) {
	p := &stubPlugin{name: "test"}
	pt := newPluginTab(p, 100)

	assert.Equal(t, tab(100), pt.tabID)
	assert.Equal(t, "test", pt.p.Name())
	assert.NotNil(t, pt.fetcher)
	assert.NotNil(t, pt.renderer)
}

func TestNewPluginTabMinimalPlugin(t *testing.T) {
	p := &minimalPlugin{name: "minimal"}
	pt := newPluginTab(p, 100)

	assert.NotNil(t, pt.p)
	assert.Nil(t, pt.fetcher)
	assert.Nil(t, pt.renderer)
	assert.Nil(t, pt.exporter)
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
	assert.Equal(t, 0, fm.index)
	assert.Equal(t, "data", fm.data)
	assert.NoError(t, fm.err)
}

func TestDoPluginFetchCmdPanic(t *testing.T) {
	p := &panicPlugin{stubPlugin: stubPlugin{name: "panic"}}
	cmd := doPluginFetch(context.Background(), 1, p)
	msg := cmd()
	fm, ok := msg.(pluginFetchMsg)
	require.True(t, ok)
	assert.Equal(t, 1, fm.index)
	assert.Error(t, fm.err)
}

type exporterOnlyStub struct {
	name string
}

func (p *exporterOnlyStub) Name() string                                        { return p.name }
func (p *exporterOnlyStub) Init(_ context.Context, _ plugin.PluginConfig) error { return nil }
func (p *exporterOnlyStub) Fetch(_ context.Context) (any, error)                { return "metrics-data", nil }
func (p *exporterOnlyStub) WriteMetrics(_ io.Writer, _ any, _ string)           {}

func TestNewPluginTab_ExporterOnly(t *testing.T) {
	p := &exporterOnlyStub{name: "exporter-only"}
	pt := newPluginTab(p, 100)

	assert.NotNil(t, pt.fetcher)
	assert.Nil(t, pt.renderer)
	assert.NotNil(t, pt.exporter)
}

func TestNewApp_IncludesExporterOnlyPlugins(t *testing.T) {
	renderer := &stubPlugin{name: "with-renderer"}
	exporterOnly := &exporterOnlyStub{name: "exporter-only"}

	cfg := Config{
		Plugins: []plugin.Plugin{renderer, exporterOnly},
	}

	app := NewApp(nil, cfg)

	assert.Len(t, app.pluginTabs, 2, "both plugins should be in pluginTabs")
	assert.Len(t, app.tabs, 5, "Caddy + Config + Certificates + Logs + renderer plugin should be in tabs")
	assert.Equal(t, tabCaddy, app.tabs[0])
	assert.Equal(t, tabConfig, app.tabs[1])
	assert.Equal(t, tabCertificates, app.tabs[2])
	assert.Equal(t, tabLogs, app.tabs[3])
	assert.Equal(t, tabPluginBase, app.tabs[4])
}

func TestPluginExports_IncludesExporterOnly(t *testing.T) {
	exporterOnly := &exporterOnlyStub{name: "exporter-only"}

	cfg := Config{
		Plugins: []plugin.Plugin{exporterOnly},
	}

	app := NewApp(nil, cfg)
	app.pluginTabs[0].data = "some-data"

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
	app.pluginTabs[0].data = "renderer-data"
	app.pluginTabs[1].data = "exporter-data"

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
	app.pluginTabs[0].fetching = false

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
