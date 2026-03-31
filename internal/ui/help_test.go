package ui

import (
	"testing"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestRenderHelp_ContainsAllBindings(t *testing.T) {
	out := renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, false, 120, tabFrankenPHP, false)
	plain := stripANSI(out)

	assert.Contains(t, plain, "navigate")
	assert.Contains(t, plain, "sort(index)")
	assert.Contains(t, plain, "pause")
	assert.Contains(t, plain, "restart")
	assert.Contains(t, plain, "filter")
	assert.Contains(t, plain, "quit")
}

func TestRenderHelp_ShowsCurrentSortField(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByMemory, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, false, 120, tabFrankenPHP, false))
	assert.Contains(t, out, "sort(memory)")
}

func TestRenderHelp_PausedShowsResume(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, true, 120, tabFrankenPHP, false))
	assert.Contains(t, out, "resume")
	assert.NotContains(t, out, "pause")
}

func TestRenderHelp_RespectsWidth(t *testing.T) {
	out := renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, false, 200, tabFrankenPHP, false)
	assert.Equal(t, 200, lipgloss.Width(out))
}

func TestRenderHelp_CaddyTab(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, false, 120, tabCaddy, false))
	assert.Contains(t, out, "sort(host)")
	assert.NotContains(t, out, "restart")
	assert.Contains(t, out, "navigate")
	assert.Contains(t, out, "filter")
	assert.Contains(t, out, "quit")
}

func TestRenderHelp_ConfigTab(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, false, 120, tabConfig, false))
	assert.Contains(t, out, "navigate")
	assert.Contains(t, out, "expand")
	assert.Contains(t, out, "collapse")
	assert.Contains(t, out, "expand/collapse all")
	assert.Contains(t, out, "search")
	assert.Contains(t, out, "help")
	assert.NotContains(t, out, "sort")
	assert.NotContains(t, out, "detail")
	assert.NotContains(t, out, "next/prev match")
	assert.NotContains(t, out, "refresh")
	assert.Contains(t, out, "quit")
}

func TestRenderHelp_CertificatesTab(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, false, 120, tabCertificates, false))
	assert.Contains(t, out, "sort(domain)")
	assert.Contains(t, out, "refresh")
	assert.Contains(t, out, "filter")
	assert.Contains(t, out, "quit")
	assert.NotContains(t, out, "restart")
	assert.NotContains(t, out, "detail")
}

func TestRenderHelp_LogsTab(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, false, 120, tabLogs, false))
	assert.Contains(t, out, "navigate")
	assert.Contains(t, out, "filter")
	assert.Contains(t, out, "pause")
	assert.Contains(t, out, "clear")
	assert.Contains(t, out, "quit")
	assert.NotContains(t, out, "sort(")
	assert.NotContains(t, out, "restart")
}

func TestRenderHelp_LogsTab_PausedShowsResume(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, false, 120, tabLogs, true))
	assert.Contains(t, out, "resume")
}

func TestRenderHelp_SeparatorsPresent(t *testing.T) {
	out := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, false, 120, tabFrankenPHP, false))
	assert.Contains(t, out, "·")
}

func TestRenderHelpOverlay_ContainsBindings(t *testing.T) {
	out := stripANSI(renderHelpOverlay(120, 40, false, true, nil))

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
	out := stripANSI(renderHelpOverlay(120, 40, false, false, nil))

	assert.Contains(t, out, "Navigation")
	assert.Contains(t, out, "Toggle graphs")
	assert.Contains(t, out, "Quit")
	assert.Contains(t, out, "1/2/3/4")
	assert.NotContains(t, out, "1/2/3/4/5")
	assert.Contains(t, out, "Refresh config/certs")
	assert.NotContains(t, out, "restart workers")
}
