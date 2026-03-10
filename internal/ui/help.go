package ui

import (
	"strings"

	"github.com/alexandredaubois/ember/internal/model"
)

func renderHelp(sortBy model.SortField, paused bool, leakEnabled bool, width int) string {
	pauseLabel := "pause"
	if paused {
		pauseLabel = "resume"
	}

	leakLabel := "leak:on"
	if !leakEnabled {
		leakLabel = "leak:off"
	}

	type binding struct {
		key  string
		desc string
	}
	bindings := []binding{
		{"↑/↓", "navigate"},
		{"s/S", "sort(" + sortBy.String() + ")"},
		{"p", pauseLabel},
		{"l", leakLabel},
		{"r", "restart"},
		{"/", "filter"},
		{"q", "quit"},
	}

	var parts []string
	for _, b := range bindings {
		parts = append(parts, helpKeyStyle.Render(b.key)+helpStyle.Render(" "+b.desc))
	}
	content := " " + strings.Join(parts, helpStyle.Render("  ·  "))

	return helpStyle.Width(width).Render(content)
}
