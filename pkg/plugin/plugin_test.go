package plugin_test

import (
	"bytes"
	"context"
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/alexandre-daubois/ember/pkg/plugin"
)

type fakePlugin struct {
	name    string
	initErr error
	initCfg plugin.PluginConfig
}

func (p *fakePlugin) Name() string { return p.name }
func (p *fakePlugin) Init(_ context.Context, cfg plugin.PluginConfig) error {
	p.initCfg = cfg
	return p.initErr
}

type fakeFullPlugin struct {
	fakePlugin
	fetchData any
	fetchErr  error
}

func (p *fakeFullPlugin) Fetch(_ context.Context) (any, error) {
	return p.fetchData, p.fetchErr
}

func (p *fakeFullPlugin) Update(data any, _, _ int) plugin.Renderer { return p }
func (p *fakeFullPlugin) View(_, _ int) string                      { return "view" }
func (p *fakeFullPlugin) HandleKey(_ tea.KeyMsg) bool               { return false }
func (p *fakeFullPlugin) StatusCount() string                       { return "42" }
func (p *fakeFullPlugin) HelpBindings() []plugin.HelpBinding {
	return []plugin.HelpBinding{{Key: "x", Desc: "do something"}}
}
func (p *fakeFullPlugin) WriteMetrics(w io.Writer, _ any, prefix string) {
	name := prefix + "_fake_metric"
	if prefix == "" {
		name = "fake_metric"
	}
	_, _ = io.WriteString(w, name+" 1\n")
}

func TestRegisterAndAll(t *testing.T) {
	plugin.Reset()

	assert.Empty(t, plugin.All())

	p1 := &fakePlugin{name: "alpha"}
	p2 := &fakePlugin{name: "beta"}
	plugin.Register(p1)
	plugin.Register(p2)

	all := plugin.All()
	require.Len(t, all, 2)
	assert.Equal(t, "alpha", all[0].Name())
	assert.Equal(t, "beta", all[1].Name())
}

func TestAllReturnsCopy(t *testing.T) {
	plugin.Reset()

	plugin.Register(&fakePlugin{name: "one"})
	first := plugin.All()
	first[0] = nil

	second := plugin.All()
	assert.NotNil(t, second[0])
}

func TestReset(t *testing.T) {
	plugin.Reset()

	plugin.Register(&fakePlugin{name: "temp"})
	assert.Len(t, plugin.All(), 1)

	plugin.Reset()
	assert.Empty(t, plugin.All())
}

func TestRegisterDuplicatePanics(t *testing.T) {
	plugin.Reset()

	plugin.Register(&fakePlugin{name: "dup"})
	assert.Panics(t, func() {
		plugin.Register(&fakePlugin{name: "dup"})
	})
}

func TestRegisterDuplicatePanicsMessage(t *testing.T) {
	plugin.Reset()

	plugin.Register(&fakePlugin{name: "myplug"})
	assert.PanicsWithValue(t, "ember: duplicate plugin name: myplug", func() {
		plugin.Register(&fakePlugin{name: "myplug"})
	})
}

func TestRegisterDifferentNamesOK(t *testing.T) {
	plugin.Reset()

	plugin.Register(&fakePlugin{name: "a"})
	plugin.Register(&fakePlugin{name: "b"})
	assert.Len(t, plugin.All(), 2)
}

func TestPluginInit(t *testing.T) {
	p := &fakePlugin{name: "test"}
	cfg := plugin.PluginConfig{
		CaddyAddr: "http://localhost:2019",
		Options:   map[string]string{"key": "value"},
	}
	err := p.Init(context.Background(), cfg)

	assert.NoError(t, err)
	assert.Equal(t, cfg, p.initCfg)
}

func TestPluginInitError(t *testing.T) {
	p := &fakePlugin{name: "broken", initErr: assert.AnError}

	err := p.Init(context.Background(), plugin.PluginConfig{})
	assert.ErrorIs(t, err, assert.AnError)
}

func TestFetcherInterface(t *testing.T) {
	p := &fakeFullPlugin{
		fakePlugin: fakePlugin{name: "full"},
		fetchData:  "hello",
	}
	data, err := p.Fetch(context.Background())

	assert.NoError(t, err)
	assert.Equal(t, "hello", data)
}

func TestFetcherError(t *testing.T) {
	p := &fakeFullPlugin{
		fakePlugin: fakePlugin{name: "broken-fetch"},
		fetchErr:   assert.AnError,
	}
	_, err := p.Fetch(context.Background())
	assert.ErrorIs(t, err, assert.AnError)
}

func TestRendererInterface(t *testing.T) {
	p := &fakeFullPlugin{fakePlugin: fakePlugin{name: "renderer"}}

	updated := p.Update("data", 80, 24)
	assert.NotNil(t, updated)
	assert.Equal(t, "view", updated.View(80, 24))
	assert.False(t, updated.HandleKey(tea.KeyMsg{}))
	assert.Equal(t, "42", updated.StatusCount())
	assert.Len(t, updated.HelpBindings(), 1)
}

func TestExporterInterface(t *testing.T) {
	p := &fakeFullPlugin{fakePlugin: fakePlugin{name: "exporter"}}

	var buf bytes.Buffer
	p.WriteMetrics(&buf, nil, "ember")
	assert.Equal(t, "ember_fake_metric 1\n", buf.String())
}

func TestExporterNoPrefix(t *testing.T) {
	p := &fakeFullPlugin{fakePlugin: fakePlugin{name: "exporter"}}

	var buf bytes.Buffer
	p.WriteMetrics(&buf, nil, "")
	assert.Equal(t, "fake_metric 1\n", buf.String())
}

func TestRegisterEmptyNamePanics(t *testing.T) {
	plugin.Reset()

	assert.PanicsWithValue(t, "ember: plugin name must not be empty", func() {
		plugin.Register(&fakePlugin{name: ""})
	})
}

func TestRegisterWhitespaceNamePanics(t *testing.T) {
	plugin.Reset()

	assert.PanicsWithValue(t, "ember: plugin name must not contain whitespace: my plugin", func() {
		plugin.Register(&fakePlugin{name: "my plugin"})
	})
}

func TestRegisterTabInNamePanics(t *testing.T) {
	plugin.Reset()

	assert.PanicsWithValue(t, "ember: plugin name must not contain whitespace: my\tplugin", func() {
		plugin.Register(&fakePlugin{name: "my\tplugin"})
	})
}

type fakeCloserPlugin struct {
	fakePlugin
	closed bool
}

func (p *fakeCloserPlugin) Close() error {
	p.closed = true
	return nil
}

func TestRegisterUnderscoreNamePanics(t *testing.T) {
	plugin.Reset()

	assert.PanicsWithValue(t, "ember: plugin name must not contain underscores (use hyphens instead): my_plugin", func() {
		plugin.Register(&fakePlugin{name: "my_plugin"})
	})
}

func TestRegisterNormalizedCollisionPanics(t *testing.T) {
	plugin.Reset()

	plugin.Register(&fakePlugin{name: "my-plugin"})
	assert.PanicsWithValue(t, "ember: plugin name collision after normalization: myplugin vs my-plugin", func() {
		plugin.Register(&fakePlugin{name: "myplugin"})
	})
}

func TestRegisterNormalizedCollisionReversePanics(t *testing.T) {
	plugin.Reset()

	plugin.Register(&fakePlugin{name: "myplugin"})
	assert.PanicsWithValue(t, "ember: plugin name collision after normalization: my-plugin vs myplugin", func() {
		plugin.Register(&fakePlugin{name: "my-plugin"})
	})
}

func TestRegisterDistinctNormalizedNamesOK(t *testing.T) {
	plugin.Reset()

	plugin.Register(&fakePlugin{name: "rate-limit"})
	plugin.Register(&fakePlugin{name: "rate-limiter"})
	assert.Len(t, plugin.All(), 2)
}

func TestCloserInterface(t *testing.T) {
	p := &fakeCloserPlugin{fakePlugin: fakePlugin{name: "closable"}}

	closer, ok := any(p).(plugin.Closer)
	require.True(t, ok)
	assert.NoError(t, closer.Close())
	assert.True(t, p.closed)
}

func TestNonCloserPlugin(t *testing.T) {
	p := &fakePlugin{name: "simple"}

	_, ok := any(p).(plugin.Closer)
	assert.False(t, ok)
}

func TestSafeFetch(t *testing.T) {
	p := &fakeFullPlugin{
		fakePlugin: fakePlugin{name: "safe"},
		fetchData:  "result",
	}
	data, err := plugin.SafeFetch(context.Background(), p)
	assert.NoError(t, err)
	assert.Equal(t, "result", data)
}

func TestSafeFetch_Error(t *testing.T) {
	p := &fakeFullPlugin{
		fakePlugin: fakePlugin{name: "fail"},
		fetchErr:   assert.AnError,
	}
	_, err := plugin.SafeFetch(context.Background(), p)
	assert.ErrorIs(t, err, assert.AnError)
}

type panicFetcher struct {
	fakePlugin
}

func (p *panicFetcher) Fetch(_ context.Context) (any, error) { panic("boom") }

func TestSafeFetch_RecoversPanic(t *testing.T) {
	p := &panicFetcher{fakePlugin: fakePlugin{name: "panic"}}
	data, err := plugin.SafeFetch(context.Background(), p)
	assert.Nil(t, data)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugin panic during Fetch")
}
