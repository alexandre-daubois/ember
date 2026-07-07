package ui

import (
	"testing"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/alexandre-daubois/ember/pkg/plugin"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

// footerRendererStub satisfies plugin.Renderer + plugin.FooterRenderer with a
// configurable FooterText return. Used by the plugin-footer-override tests
// below.
type footerRendererStub struct {
	footer string
}

func (s *footerRendererStub) Update(_ any, _, _ int) plugin.Renderer { return s }
func (s *footerRendererStub) View(_, _ int) string                   { return "" }
func (s *footerRendererStub) HandleKey(_ tea.KeyMsg) bool            { return false }
func (s *footerRendererStub) StatusCount() string                    { return "" }
func (s *footerRendererStub) HelpBindings() []plugin.HelpBinding     { return nil }
func (s *footerRendererStub) FooterText(_ int) string                { return s.footer }

func TestRenderHelp_ContainsAllBindings(t *testing.T) {
	out := renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabFrankenPHP, false, false, false, nil)
	plain := stripANSI(out)

	assert.Contains(t, plain, "navigate")
	assert.Contains(t, plain, "sort(index)")
	assert.Contains(t, plain, "pause")
	assert.Contains(t, plain, "restart")
	assert.Contains(t, plain, "filter")
	assert.Contains(t, plain, "quit")
}

func TestRenderHelp_ShowsCurrentSortField(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByMemory, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabFrankenPHP, false, false, false, nil))
	assert.Contains(t, out, "sort(memory)")
}

func TestRenderHelp_LogsTab_RoutesViewShowsSort(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteAvg, false, 120, tabLogs, false, true, false, nil))
	assert.Contains(t, out, "sort(avg)")
}

func TestRenderHelp_LogsTab_LogsViewHidesSort(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabLogs, false, false, false, nil))
	assert.NotContains(t, out, "sort(")
}

func TestRenderHelp_PausedShowsResume(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, true, 120, tabFrankenPHP, false, false, false, nil))
	assert.Contains(t, out, "resume")
	assert.NotContains(t, out, "pause")
}

func TestRenderHelp_RespectsWidth(t *testing.T) {
	out := renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 200, tabFrankenPHP, false, false, false, nil)
	assert.Equal(t, 200, lipgloss.Width(out))
}

func TestRenderHelp_CaddyTab(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabCaddy, false, false, false, nil))
	assert.Contains(t, out, "sort(host)")
	assert.NotContains(t, out, "restart")
	assert.Contains(t, out, "navigate")
	assert.Contains(t, out, "filter")
	assert.Contains(t, out, "quit")
}

func TestRenderHelp_ConfigTab(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabConfig, false, false, false, nil))
	assert.Contains(t, out, "navigate")
	assert.Contains(t, out, "expand")
	assert.Contains(t, out, "collapse")
	assert.Contains(t, out, "expand/collapse all")
	assert.Contains(t, out, "search")
	assert.Contains(t, out, "help")
	assert.Contains(t, out, "n/N")
	assert.Contains(t, out, "next/prev match")
	assert.NotContains(t, out, "sort")
	assert.NotContains(t, out, "detail")
	assert.NotContains(t, out, "refresh")
	assert.Contains(t, out, "quit")
}

func TestRenderHelp_CertificatesTab(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabCertificates, false, false, false, nil))
	assert.Contains(t, out, "sort(domain)")
	assert.Contains(t, out, "refresh")
	assert.Contains(t, out, "filter")
	assert.Contains(t, out, "quit")
	assert.NotContains(t, out, "restart")
	assert.NotContains(t, out, "detail")
}

func TestRenderHelp_LogsTab(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabLogs, false, false, false, nil))
	assert.Contains(t, out, "navigate")
	assert.Contains(t, out, "filter")
	assert.Contains(t, out, "pause")
	assert.Contains(t, out, "clear")
	assert.Contains(t, out, "quit")
	assert.NotContains(t, out, "sort(")
	assert.NotContains(t, out, "restart")
}

func TestRenderHelp_LogsTab_PausedShowsResume(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabLogs, true, false, false, nil))
	assert.Contains(t, out, "resume")
}

func TestRenderHelp_SeparatorsPresent(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabFrankenPHP, false, false, false, nil))
	assert.Contains(t, out, "·")
}

func TestRenderHelpOverlay_ContainsBindings(t *testing.T) {
	tabs := []tab{tabCaddy, tabFrankenPHP, tabLogs, tabConfig, tabCertificates}
	out := stripANSI(renderHelpOverlay(120, 40, true, nil, tabs))

	assert.Contains(t, out, "Navigation")
	assert.Contains(t, out, "Actions")
	assert.Contains(t, out, "Move cursor")
	assert.Contains(t, out, "Open detail / expand node")
	assert.Contains(t, out, "Cycle sort field")
	assert.Contains(t, out, "Filter / search")
	assert.Contains(t, out, "Toggle graphs")
	assert.Contains(t, out, "Expand / collapse all")
	assert.Contains(t, out, "Clear log buffer")
	assert.Contains(t, out, "Jump to Logs for selected host")
	assert.Contains(t, out, "Quit")
	assert.Contains(t, out, "Toggle this help")
	assert.Contains(t, out, "1/2/3/4/5")
	assert.Contains(t, out, "Refresh config/certs / restart workers")
}

func TestRenderHelpOverlay_WithoutFrankenPHP(t *testing.T) {
	tabs := []tab{tabCaddy, tabLogs, tabConfig, tabCertificates}
	out := stripANSI(renderHelpOverlay(120, 40, false, nil, tabs))

	assert.Contains(t, out, "Navigation")
	assert.Contains(t, out, "Toggle graphs")
	assert.Contains(t, out, "Quit")
	assert.Contains(t, out, "1/2/3/4")
	assert.NotContains(t, out, "1/2/3/4/5")
	assert.Contains(t, out, "Refresh config/certs")
	assert.NotContains(t, out, "restart workers")
}

func TestRenderHelpOverlay_JumpHintCountsPluginTabs(t *testing.T) {
	// Six core tabs plus two plugin tabs: the jump hint must span all of them
	// (1-8), not cap at 6.
	tabs := []tab{tabCaddy, tabFrankenPHP, tabUpstreams, tabLogs, tabConfig, tabCertificates, tab(100), tab(101)}
	out := stripANSI(renderHelpOverlay(120, 40, true, nil, tabs))

	assert.Contains(t, out, "1/2/3/4/5/6/7/8")
}

// Plugin tab is active and the plugin returns a non-empty FooterText hint.
// Expectation: footer rendered by the plugin replaces the default core
// footer (no "navigate"/"sort" leftover).
func TestPluginFooterOverride_NonEmpty(t *testing.T) {
	pt := &pluginTab{
		renderer: &footerRendererStub{footer: "custom hint"},
		tabID:    100,
	}
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tab(100), false, false, false, pt))
	assert.Contains(t, out, "custom hint")
	// Default Caddy/FrankenPHP footer hints must not bleed through.
	assert.NotContains(t, out, "navigate")
	assert.NotContains(t, out, "sort(")
}

// Plugin returns empty string from FooterText: should fall back to the
// default core footer for the active tab. The default footer carries
// "navigate"/"quit" entries.
func TestPluginFooterOverride_EmptyFallback(t *testing.T) {
	pt := &pluginTab{
		renderer: &footerRendererStub{footer: ""},
		tabID:    100,
	}
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabFrankenPHP, false, false, false, pt))
	assert.Contains(t, out, "navigate")
	assert.Contains(t, out, "quit")
}

// pendingTabSelect is treated as a global-mode hint that wins over any
// plugin override. Even with a plugin returning a non-empty FooterText, the
// "Tab select:" hint must take priority.
func TestPluginFooterOverride_TabSelectWins(t *testing.T) {
	pt := &pluginTab{
		renderer: &footerRendererStub{footer: "should not appear"},
		tabID:    100,
	}
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tab(100), false, false, true, pt))
	assert.Contains(t, out, "Tab select:")
	assert.NotContains(t, out, "should not appear")
}
