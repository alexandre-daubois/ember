package ui

import (
	"strings"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

func renderHelp(sortBy model.SortField, hostSortBy model.HostSortField, paused bool, width int, activeTab Tab) string {
	pauseLabel := "pause"
	if paused {
		pauseLabel = "resume"
	}

	type binding struct {
		key  string
		desc string
	}

	var sortLabel string
	if activeTab == TabCaddy {
		sortLabel = hostSortBy.String()
	} else {
		sortLabel = sortBy.String()
	}

	bindings := []binding{
		{"↑/↓", "navigate"},
		{"Enter", "detail"},
		{"s/S", "sort(" + sortLabel + ")"},
		{"p", pauseLabel},
	}
	if activeTab == TabFrankenPHP {
		bindings = append(bindings, binding{"r", "restart"})
	}
	bindings = append(bindings,
		binding{"g", "graphs"},
		binding{"/", "filter"},
		binding{"Tab", "switch"},
		binding{"q", "quit"},
	)

	var parts []string
	for _, b := range bindings {
		parts = append(parts, helpKeyStyle.Render(b.key)+helpStyle.Render(" "+b.desc))
	}
	content := " " + strings.Join(parts, helpStyle.Render("  ·  "))

	return helpStyle.Width(width).Render(content)
}

func renderHelpOverlay(base string, width, height int, hasFrankenPHP bool) string {
	type binding struct {
		key  string
		desc string
	}

	nav := []binding{
		{"↑/↓ j/k", "Move cursor"},
		{"Enter", "Open detail panel"},
		{"Esc", "Close / go back"},
		{"Tab", "Switch tab"},
		{"1/2", "Jump to tab"},
		{"Home/End", "Jump to first/last"},
		{"PgUp/PgDn", "Page up/down"},
	}

	actions := []binding{
		{"s/S", "Cycle sort field"},
		{"p", "Pause / resume"},
		{"/", "Filter list"},
		{"g", "Toggle graphs"},
	}
	if hasFrankenPHP {
		actions = append(actions, binding{"r", "Restart workers (FrankenPHP)"})
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
	popup := boxStyle.Render(body)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup)
}
