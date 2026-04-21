package ui

import (
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/alexandre-daubois/ember/pkg/plugin"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type lifecyclePlugin struct {
	name       string
	fetchCount int
	viewData   string
}

func (p *lifecyclePlugin) Name() string                                             { return p.name }
func (p *lifecyclePlugin) Provision(_ context.Context, _ plugin.PluginConfig) error { return nil }

func (p *lifecyclePlugin) Fetch(_ context.Context) (any, error) {
	p.fetchCount++
	return fmt.Sprintf("fetch-%d", p.fetchCount), nil
}

func (p *lifecyclePlugin) Update(data any, _, _ int) plugin.Renderer {
	p.viewData = data.(string)
	return p
}

func (p *lifecyclePlugin) View(_, _ int) string {
	if p.viewData == "" {
		return "waiting..."
	}
	return "content: " + p.viewData
}

func (p *lifecyclePlugin) HandleKey(msg tea.KeyMsg) bool {
	return msg.String() == "x"
}

func (p *lifecyclePlugin) StatusCount() string {
	if p.viewData == "" {
		return ""
	}
	return p.viewData
}

func (p *lifecyclePlugin) HelpBindings() []plugin.HelpBinding {
	return []plugin.HelpBinding{{Key: "x", Desc: "custom action"}}
}

func (p *lifecyclePlugin) WriteMetrics(w io.Writer, data any, prefix string) {
	if data == nil {
		return
	}
	name := "lifecycle_ticks"
	if prefix != "" {
		name = prefix + "_" + name
	}
	fmt.Fprintf(w, "%s{plugin=\"%s\"} 1\n", name, p.name)
}

func TestIntegration_PluginFullLifecycle(t *testing.T) {
	p := &lifecyclePlugin{name: "lifecycle"}
	app := NewApp(nil, Config{Plugins: []plugin.Plugin{p}})
	app.width = 120
	app.height = 40

	// plugin tab is registered
	require.Len(t, app.pluginTabs, 1)
	require.Len(t, app.pluginGroups, 1)
	assert.Contains(t, app.tabs, app.pluginTabs[0].tabID)

	// before first fetch: view shows initial state
	app.switchTab(app.pluginTabs[0].tabID)
	view := safePluginView(app.pluginTabs[0].renderer, 80, 24)
	assert.Equal(t, "waiting...", view)

	// dispatch pluginFetchMsg through Update (simulates completed fetch)
	app.Update(pluginFetchMsg{groupIndex: 0, data: "fetch-1"})

	// data propagated to group
	assert.Equal(t, "fetch-1", app.pluginGroups[0].data)
	assert.NoError(t, app.pluginGroups[0].err)

	// renderer was updated
	view = safePluginView(app.pluginTabs[0].renderer, 80, 24)
	assert.Equal(t, "content: fetch-1", view)

	// StatusCount reflects new data
	count, err := safePluginStatusCount(app.pluginTabs[0].renderer)
	assert.NoError(t, err)
	assert.Equal(t, "fetch-1", count)

	// Exports include plugin data
	exports := app.pluginExports()
	require.Len(t, exports, 1)
	assert.Equal(t, "fetch-1", exports[0].Data)
	assert.NotNil(t, exports[0].Exporter)

	// second fetch: state updates correctly
	app.Update(pluginFetchMsg{groupIndex: 0, data: "fetch-2"})
	assert.Equal(t, "fetch-2", app.pluginGroups[0].data)
	view = safePluginView(app.pluginTabs[0].renderer, 80, 24)
	assert.Equal(t, "content: fetch-2", view)

	// key handling: plugin-consumed key
	consumed, err := safePluginHandleKey(app.pluginTabs[0].renderer, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	assert.NoError(t, err)
	assert.True(t, consumed)

	// key handling: non-consumed key
	consumed, err = safePluginHandleKey(app.pluginTabs[0].renderer, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	assert.NoError(t, err)
	assert.False(t, consumed)

	hb, err := safePluginHelpBindings(app.pluginTabs[0].renderer)
	assert.NoError(t, err)
	require.Len(t, hb, 1)
	assert.Equal(t, "x", hb[0].Key)

	// fetch error: previous data preserved
	app.Update(pluginFetchMsg{groupIndex: 0, err: assert.AnError})
	assert.Equal(t, "fetch-2", app.pluginGroups[0].data, "data should be preserved on error")
	assert.ErrorIs(t, app.pluginGroups[0].err, assert.AnError)
}

func TestIntegration_MultiRendererTabAvailability(t *testing.T) {
	p := &tabAvailMasterPlugin{
		tabAvailMultiPlugin: tabAvailMultiPlugin{
			multiRendererPlugin: multiRendererPlugin{
				stubPlugin: stubPlugin{name: "multi"},
				tabs: []plugin.TabDescriptor{
					{Key: "overview", Name: "Overview"},
					{Key: "details", Name: "Details"},
				},
			},
			tabAvail: map[string]bool{"overview": true, "details": true},
		},
		available: true,
	}
	app := NewApp(nil, Config{Plugins: []plugin.Plugin{p}})
	app.width = 120
	app.height = 40

	require.Len(t, app.pluginTabs, 2)
	overviewTabID := app.pluginTabs[0].tabID
	detailsTabID := app.pluginTabs[1].tabID

	// both tabs visible initially
	assert.Contains(t, app.tabs, overviewTabID)
	assert.Contains(t, app.tabs, detailsTabID)

	// fetch with details unavailable: dispatched through Update
	p.tabAvail["details"] = false
	app.Update(pluginFetchMsg{groupIndex: 0, data: "tick-1"})

	assert.Contains(t, app.tabs, overviewTabID, "overview stays visible")
	assert.NotContains(t, app.tabs, detailsTabID, "details hidden by TabAvailability")

	// details becomes available again
	p.tabAvail["details"] = true
	app.Update(pluginFetchMsg{groupIndex: 0, data: "tick-2"})

	assert.Contains(t, app.tabs, overviewTabID)
	assert.Contains(t, app.tabs, detailsTabID, "details re-shown")

	// master switch off: hides everything
	p.available = false
	app.Update(pluginFetchMsg{groupIndex: 0, data: "tick-3"})

	assert.NotContains(t, app.tabs, overviewTabID, "master off hides overview")
	assert.NotContains(t, app.tabs, detailsTabID, "master off hides details")

	// master switch on with details unavailable: only overview returns
	p.available = true
	p.tabAvail["details"] = false
	app.Update(pluginFetchMsg{groupIndex: 0, data: "tick-4"})

	assert.Contains(t, app.tabs, overviewTabID, "overview restored after master on")
	assert.NotContains(t, app.tabs, detailsTabID, "details stays hidden per-tab after master on")
}
