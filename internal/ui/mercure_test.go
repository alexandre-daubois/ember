package ui

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func hubSnap(connected, subsTotal, updatesTotal float64) *fetcher.Snapshot {
	return &fetcher.Snapshot{
		FetchedAt: time.Now(),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Mercure: &fetcher.MercureMetrics{SubscribersConnected: connected, SubscribersTotal: subsTotal, UpdatesTotal: updatesTotal},
		},
	}
}

func newBareApp() *App {
	return &App{
		activeTab: tabCaddy,
		tabs:      []tab{tabCaddy, tabLogs, tabConfig, tabCertificates},
		tabStates: map[tab]*tabState{tabCaddy: {}, tabLogs: {}, tabConfig: {}, tabCertificates: {}},
		history:   newHistoryStore(),
		viewTime:  time.Now(),
		downSince: make(map[string]time.Time),
		width:     120,
		height:    40,
	}
}

func TestInsertTab_KeepsCoreOrderWhateverTheDetectionOrder(t *testing.T) {
	app := newBareApp()
	app.enableMercure()
	app.enableUpstreams()
	app.enableFrankenPHP()

	assert.Equal(t, []tab{tabCaddy, tabFrankenPHP, tabMercure, tabUpstreams, tabLogs, tabConfig, tabCertificates}, app.tabs)
}

func TestInsertTab_StaysAheadOfPluginTabs(t *testing.T) {
	app := newBareApp()
	app.tabs = append(app.tabs, tabPluginBase)
	app.tabStates[tabPluginBase] = &tabState{}
	app.enableMercure()

	assert.Equal(t, []tab{tabCaddy, tabMercure, tabLogs, tabConfig, tabCertificates, tabPluginBase}, app.tabs)
}

func TestInsertTab_Idempotent(t *testing.T) {
	app := newBareApp()
	app.enableMercure()
	app.enableMercure()

	assert.Equal(t, 1, countTab(app.tabs, tabMercure))
}

func countTab(tabs []tab, target tab) int {
	n := 0
	for _, t := range tabs {
		if t == target {
			n++
		}
	}
	return n
}

// A hub-only Caddy (no `metrics` global option) drops the Caddy tab and lands
// on Mercure; the tab comes back as soon as HTTP metrics appear.
func TestFetchMsg_HubOnlyHidesCaddyTabUntilHTTPMetricsShowUp(t *testing.T) {
	app := newBareApp()
	app.Update(fetchMsg{snap: hubSnap(3, 10, 100)})

	assert.True(t, app.hasMercure)
	assert.Equal(t, []tab{tabMercure, tabLogs, tabConfig, tabCertificates}, app.tabs)
	assert.Equal(t, tabMercure, app.activeTab)
	assert.True(t, app.mercureOnly())
	_, hasCaddyState := app.tabStates[tabCaddy]
	assert.False(t, hasCaddyState)

	withHTTP := hubSnap(3, 10, 100)
	withHTTP.Metrics.HasHTTPMetrics = true
	app.Update(fetchMsg{snap: withHTTP})

	assert.Equal(t, []tab{tabCaddy, tabMercure, tabLogs, tabConfig, tabCertificates}, app.tabs)
	assert.Equal(t, tabMercure, app.activeTab, "bringing the Caddy tab back must not steal focus")
	assert.False(t, app.mercureOnly())
}

func TestFetchMsg_HubBehindFrankenPHPKeepsCaddyTab(t *testing.T) {
	app := newBareApp()
	snap := hubSnap(3, 10, 100)
	snap.HasFrankenPHP = true
	app.Update(fetchMsg{snap: snap})

	assert.Equal(t, []tab{tabCaddy, tabFrankenPHP, tabMercure, tabLogs, tabConfig, tabCertificates}, app.tabs)
	assert.Equal(t, tabCaddy, app.activeTab)
	assert.False(t, app.mercureOnly())
}

func TestFetchMsg_FailedScrapeDoesNotTouchTheCaddyTab(t *testing.T) {
	app := newBareApp()
	withHTTP := hubSnap(3, 10, 100)
	withHTTP.Metrics.HasHTTPMetrics = true
	app.Update(fetchMsg{snap: withHTTP})
	require.Contains(t, app.tabs, tabCaddy)

	app.Update(fetchMsg{snap: &fetcher.Snapshot{MetricsFailed: true, Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}}}})

	assert.Contains(t, app.tabs, tabCaddy)
}

func TestFetchMsg_PlainCaddyWithoutMetricsKeepsCaddyTab(t *testing.T) {
	app := newBareApp()
	app.Update(fetchMsg{snap: &fetcher.Snapshot{Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}}}})

	assert.Equal(t, []tab{tabCaddy, tabLogs, tabConfig, tabCertificates}, app.tabs)
	assert.False(t, app.hasMercure)
}

func TestFetchMsg_HubAloneCountsAsFreshData(t *testing.T) {
	app := newBareApp()
	app.Update(fetchMsg{snap: hubSnap(3, 10, 100)})
	app.Update(fetchMsg{snap: hubSnap(4, 12, 130)})

	assert.False(t, app.stale)
	assert.Equal(t, []float64{3, 4}, app.history.subscribers)
	assert.Len(t, app.history.updates, 2)
}

func TestFetchMsg_HubDetectionSchedulesConfigFetch(t *testing.T) {
	app := newBareApp()
	_, cmd := app.Update(fetchMsg{snap: hubSnap(3, 10, 100)})

	assert.NotNil(t, cmd, "detecting the hub must kick off the config fetch")
}

func TestView_HubPresentSuppressesConnectionError(t *testing.T) {
	app := newBareApp()
	snap := hubSnap(3, 10, 100)
	snap.Errors = []string{"read process stats: permission denied"}
	app.Update(fetchMsg{snap: snap})

	out := stripANSI(app.View())
	assert.NotContains(t, out, "Cannot reach the Caddy admin API")
	assert.Contains(t, out, "Mercure (3 subs)")
}

func TestView_MercureTab_ShowsCountersAndConfig(t *testing.T) {
	app := newBareApp()
	app.Update(fetchMsg{snap: hubSnap(3, 10, 100)})
	app.Update(mercureConfigFetchMsg{configs: []fetcher.MercureConfig{{
		Name: "hub-1", Transport: "bolt", Anonymous: true, Playground: true, Debugger: true,
		Issuers: []string{"https://issuer.example"}, Heartbeat: "40s",
	}}})

	out := stripANSI(app.View())
	assert.Contains(t, out, "Hub activity")
	assert.Contains(t, out, "Subscribers total")
	assert.Contains(t, out, "hub-1")
	assert.Contains(t, out, "bolt")
	assert.Contains(t, out, "dev only, insecure")
	assert.Contains(t, out, "https://issuer.example")
	assert.Contains(t, out, "600s (default)")
	assert.NotContains(t, out, "No metrics", "the metrics hint is about HTTP traffic, which a hub does not need")
	assert.Contains(t, out, "Subscribers 3")
	assert.NotContains(t, out, "RPS ")
}

func TestRenderMercureView_States(t *testing.T) {
	app := newBareApp()
	assert.Contains(t, renderMercureView(&app.state, nil, nil, nil, 80, 20, false), "Waiting for Mercure metrics")

	app.state.Update(hubSnap(1, 1, 1))
	assert.Contains(t, stripANSI(renderMercureView(&app.state, nil, nil, nil, 80, 20, false)), "Loading hub config")
	assert.Contains(t, stripANSI(renderMercureView(&app.state, []fetcher.MercureConfig{}, nil, nil, 80, 20, false)), "No mercure handler found")

	two := []fetcher.MercureConfig{{Name: "a"}, {Name: "b"}}
	out := stripANSI(renderMercureView(&app.state, two, nil, nil, 80, 60, false))
	assert.Contains(t, out, "Hub configuration 1/2")
	assert.Contains(t, out, "Hub configuration 2/2")
}

func TestRenderMercureView_ClampsToHeight(t *testing.T) {
	app := newBareApp()
	app.state.Update(hubSnap(1, 1, 1))
	out := renderMercureView(&app.state, []fetcher.MercureConfig{{}}, nil, nil, 80, 4, false)
	assert.Equal(t, 4, lipgloss.Height(out))
}

func TestRenderMercureView_NoColorEmitsNoANSI(t *testing.T) {
	prev := lipgloss.ColorProfile()
	defer lipgloss.SetColorProfile(prev)
	lipgloss.SetColorProfile(termenv.Ascii)

	app := newBareApp()
	app.state.Update(hubSnap(1, 1, 1))
	out := renderMercureView(&app.state, []fetcher.MercureConfig{{Playground: true}}, []float64{1, 2}, []float64{1, 2}, 80, 40, false)
	assert.NotContains(t, out, "\x1b[")
	assert.Contains(t, out, "dev only, insecure", "the playground warning must survive without colour")
}

type stubConfigFetcher struct {
	fetcher.Fetcher
	raw json.RawMessage
}

func (s stubConfigFetcher) FetchConfig(context.Context) (json.RawMessage, error) {
	return s.raw, nil
}

func TestMercureConfigFetch_RoundTrip(t *testing.T) {
	app := newBareApp()
	app.fetcher = stubConfigFetcher{raw: json.RawMessage(`{"apps":{"http":{"servers":{"srv0":{"routes":[{"handle":[{"handler":"mercure","name":"x"}]}]}}}}}`)}

	msg := app.doFetchMercureConfig()()
	app.Update(msg)

	require.Len(t, app.mercureConfigs, 1)
	assert.Equal(t, "x", app.mercureConfigs[0].Name)
}

func TestMercureConfigFetch_NoHandlerYieldsEmptyNotNil(t *testing.T) {
	app := newBareApp()
	app.fetcher = stubConfigFetcher{raw: json.RawMessage(`{}`)}

	app.Update(app.doFetchMercureConfig()())

	assert.NotNil(t, app.mercureConfigs, "nil would keep the view on 'Loading'")
	assert.Empty(t, app.mercureConfigs)
}

func TestMercureConfigFetch_UnsupportedFetcher(t *testing.T) {
	app := newBareApp()
	msg := app.doFetchMercureConfig()()
	app.Update(msg)

	assert.Contains(t, app.status, "mercure config fetch failed")
	assert.Nil(t, app.mercureConfigs)
}

func TestMercureKeys(t *testing.T) {
	app := newBareApp()
	app.Update(fetchMsg{snap: hubSnap(3, 10, 100)})
	app.mercureConfigs = []fetcher.MercureConfig{{}}
	require.Equal(t, tabMercure, app.activeTab)

	_, cmd := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Nil(t, app.mercureConfigs, "r drops the cached config")
	assert.NotNil(t, cmd, "r schedules a refetch")

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	assert.Equal(t, viewGraph, app.mode)
	app.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, viewList, app.mode)

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.True(t, app.paused)

	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, viewList, app.mode, "no filter on the Mercure tab")

	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tabLogs, app.activeTab)
	assert.Nil(t, cmd)

	_, cmd = app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, cmd)
}

func TestTabLabel_Mercure(t *testing.T) {
	assert.Equal(t, "Mercure", tabLabel(tabMercure, nil))
}

func TestMercureTabHelpBindings(t *testing.T) {
	app := newBareApp()
	out := stripANSI(renderBindings(app.mercureTabHelpBindings(), 120))
	assert.NotContains(t, out, "m messages", "no feed, no key to advertise")
	assert.Contains(t, out, "refresh config")
	assert.NotContains(t, out, "filter")

	app.config.MercureFeed = model.NewMercureFeed(1)
	app.paused = true
	out = stripANSI(renderBindings(app.mercureTabHelpBindings(), 120))
	assert.Contains(t, out, "m messages")
	assert.Contains(t, out, "p resume")
}

func TestRenderGraphPanels_MercurePanels(t *testing.T) {
	data := []float64{1, 2, 3}
	out := renderGraphPanels(120, 50, graphSeries{cpu: data, rps: data, rss: data, subscribers: data, updates: data, hasMercure: true})
	for _, title := range []string{"CPU", "RPS", "RSS", "Subscribers", "Updates"} {
		assert.Contains(t, out, title)
	}

	out = renderGraphPanels(120, 50, graphSeries{cpu: data, rps: data, rss: data, subscribers: data, updates: data, hasMercure: true, mercureOnly: true})
	assert.NotContains(t, out, "RPS", "a hub without HTTP metrics has no request rate to draw")
	assert.Contains(t, out, "Subscribers")
}

func TestRenderDashboard_MercureOnlyLine(t *testing.T) {
	app := newBareApp()
	app.state.Update(hubSnap(7, 42, 100))
	app.state.Derived.MercureUpdatesPerSec = 12.5

	out := stripANSI(renderDashboard(&app.state, 120, "v0.1", dashboardOpts{mercureOnly: true, subsHistory: []float64{5, 7}}))
	assert.Contains(t, out, "Subscribers 7")
	assert.Contains(t, out, "Updates/s 12.5")
	assert.Contains(t, out, "Total subs 42")
	assert.NotContains(t, out, "RPS")
	assert.NotContains(t, out, "No metrics")
}

func TestRenderDashboard_MercureOnlyBeforeFirstScrape(t *testing.T) {
	app := newBareApp()
	app.state.Update(&fetcher.Snapshot{Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}}})
	out := stripANSI(renderDashboard(&app.state, 120, "v0.1", dashboardOpts{mercureOnly: true}))
	assert.Contains(t, out, "Waiting for Mercure metrics")
}
