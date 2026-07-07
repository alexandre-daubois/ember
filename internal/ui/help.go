package ui

import (
	"slices"
	"strconv"
	"strings"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/alexandre-daubois/ember/pkg/plugin"
	"github.com/charmbracelet/lipgloss"
)

type binding struct {
	key  string
	desc string
}

func renderHelp(sortBy model.SortField, hostSortBy model.HostSortField, certSortBy model.CertSortField, upstreamSortBy model.UpstreamSortField, routeSortBy model.RouteSortField, paused bool, width int, activeTab tab, logFrozen, routesView, tabSelect bool, activePlugin *pluginTab) string {
	if tabSelect {
		hint := helpKeyStyle.Render("1-9") + helpStyle.Render(" switch tab") +
			helpStyle.Render("  ·  ") +
			helpKeyStyle.Render("Esc") + helpStyle.Render(" cancel")
		return helpStyle.Width(width).Render(" Tab select: " + hint)
	}

	// Plugin-Override: while a plugin tab is active and the plugin implements
	// FooterRenderer, give it first refusal on the footer line. Empty return
	// falls through to the default footer below.
	if activePlugin != nil && activePlugin.renderer != nil {
		if fr, ok := activePlugin.renderer.(plugin.FooterRenderer); ok {
			if hint := safePluginFooterText(fr, width); hint != "" {
				return helpStyle.Width(width).Render(" " + hint)
			}
		}
	}

	pauseLabel := "pause"
	if paused {
		pauseLabel = "resume"
	}

	var bindings []binding
	switch activeTab {
	case tabLogs:
		bindings = logsHelpBindings(logFrozen, routesView, routeSortBy.String())
	case tabConfig:
		bindings = []binding{
			{"↑/↓", "navigate"},
			{"Enter", "expand"},
			{"←", "collapse"},
			{"e/E", "expand/collapse all"},
			{"/", "search"},
			{"n/N", "next/prev match"},
			{"?", "help"},
			{"Tab/S-Tab", "switch"},
			{"q", "quit"},
		}
	case tabCertificates:
		bindings = []binding{
			{"↑/↓", "navigate"},
			{"s/S", "sort(" + certSortBy.String() + ")"},
			{"p", pauseLabel},
			{"r", "refresh"},
			{"g", "graphs"},
			{"/", "filter"},
			{"Tab/S-Tab", "switch"},
			{"q", "quit"},
		}
	case tabUpstreams:
		bindings = []binding{
			{"↑/↓", "navigate"},
			{"s/S", "sort(" + upstreamSortBy.String() + ")"},
			{"p", pauseLabel},
			{"r", "refresh config"},
			{"g", "graphs"},
			{"/", "filter"},
			{"Tab/S-Tab", "switch"},
			{"q", "quit"},
		}
	default:
		var sortLabel string
		if activeTab == tabCaddy {
			sortLabel = hostSortBy.String()
		} else {
			sortLabel = sortBy.String()
		}

		bindings = []binding{
			{"↑/↓", "navigate"},
			{"Enter", "detail"},
			{"s/S", "sort(" + sortLabel + ")"},
			{"p", pauseLabel},
		}
		if activeTab == tabFrankenPHP {
			bindings = append(bindings, binding{"r", "restart"})
		}
		bindings = append(bindings,
			binding{"g", "graphs"},
			binding{"/", "filter"},
			binding{"Tab/S-Tab", "switch"},
			binding{"q", "quit"},
		)
	}

	var parts []string
	for _, b := range bindings {
		parts = append(parts, helpKeyStyle.Render(b.key)+helpStyle.Render(" "+b.desc))
	}
	content := " " + strings.Join(parts, helpStyle.Render("  ·  "))

	return helpStyle.Width(width).Render(content)
}

func renderHelpOverlay(width, height int, hasFrankenPHP bool, pluginTabs []*pluginTab, visibleTabs []tab) string {
	// Number keys 1-9 jump to visibleTabs[n-1], so the hint must count every
	// visible tab (core and plugin), capped at the 9 available number keys.
	tabCount := min(len(visibleTabs), 9)
	nums := make([]string, tabCount)
	for i := range nums {
		nums[i] = strconv.Itoa(i + 1)
	}
	tabHint := strings.Join(nums, "/")

	nav := []binding{
		{"↑/↓ j/k", "Move cursor"},
		{"Enter", "Open detail / expand node"},
		{"← / h", "Collapse node (Caddy Config tab)"},
		{"Esc", "Close / clear search"},
		{"Tab/S-Tab", "Switch tab"},
		{tabHint, "Jump to tab"},
		{"t", "Enter tab-select mode (then 1-9 to switch)"},
		{"Home/End", "Jump to first/last"},
		{"PgUp/PgDn", "Page up/down"},
	}

	actions := []binding{
		{"s/S", "Cycle sort field"},
		{"p", "Pause / resume"},
		{"/", "Filter / search"},
		{"n/N", "Next / previous search match (Caddy Config tab)"},
		{"e/E", "Expand / collapse all (Caddy Config tab)"},
		{"c", "Clear log buffer (Logs tab)"},
		{"←/→", "Focus sidepanel / table (Logs tab)"},
		{"l", "Jump to Logs for selected host (Caddy tab)"},
		{"g", "Toggle graphs"},
	}
	if hasFrankenPHP {
		actions = append(actions, binding{"r", "Refresh config/certs / restart workers"})
	} else {
		actions = append(actions, binding{"r", "Refresh config/certs"})
	}
	actions = append(actions,
		binding{"?", "Toggle this help"},
		binding{"q", "Quit"},
	)

	render := func(title string, bindings []binding) string {
		var lines []string
		lines = append(lines, titleStyle.Render(title))
		for _, b := range bindings {
			lines = append(lines, "  "+helpKeyStyle.Render(b.key)+"  "+b.desc)
		}
		return strings.Join(lines, "\n")
	}

	body := render("Navigation", nav) + "\n\n" + render("Actions", actions)

	for _, pt := range pluginTabs {
		if pt.renderer == nil || !slices.Contains(visibleTabs, pt.tabID) {
			continue
		}
		hb, _ := safePluginHelpBindings(pt.renderer)
		if len(hb) == 0 {
			continue
		}
		var pluginBindings []binding
		for _, b := range hb {
			pluginBindings = append(pluginBindings, binding{b.Key, b.Desc})
		}
		body += "\n\n" + render(pt.tabName, pluginBindings)
	}

	popup := boxStyle.Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup)
}
