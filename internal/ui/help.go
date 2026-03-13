package ui

import (
	"strings"

	"github.com/alexandredaubois/ember/internal/model"
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
