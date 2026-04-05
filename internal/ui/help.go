package ui

import (
	"slices"
	"strings"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

func renderHelp(sortBy model.SortField, hostSortBy model.HostSortField, certSortBy model.CertSortField, paused bool, width int, activeTab tab) string {
	pauseLabel := "pause"
	if paused {
		pauseLabel = "resume"
	}

	type binding struct {
		key  string
		desc string
	}

	var bindings []binding
	switch activeTab {
	case tabConfig:
		bindings = []binding{
			{"↑/↓", "navigate"},
			{"Enter", "expand"},
			{"←", "collapse"},
			{"e/E", "expand/collapse all"},
			{"/", "search"},
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

func renderHelpOverlay(base string, width, height int, hasFrankenPHP bool, pluginTabs []*pluginTab, visibleTabs []tab) string {
	type binding struct {
		key  string
		desc string
	}

	nav := []binding{
		{"↑/↓ j/k", "Move cursor"},
		{"Enter", "Open detail / expand node"},
		{"← / h", "Collapse node (Caddy Config tab)"},
		{"Esc", "Close / clear search"},
		{"Tab/S-Tab", "Switch tab"},
		{"1-9", "Jump to tab"},
		{"Home/End", "Jump to first/last"},
		{"PgUp/PgDn", "Page up/down"},
	}

	actions := []binding{
		{"s/S", "Cycle sort field"},
		{"p", "Pause / resume"},
		{"/", "Filter / search"},
		{"e/E", "Expand / collapse all (Caddy Config tab)"},
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
