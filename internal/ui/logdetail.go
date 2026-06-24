package ui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/charmbracelet/lipgloss"
)

func renderLogDetailPanel(e fetcher.LogEntry, width, height int) string {
	inner := width - 4
	if inner < 10 {
		inner = 10
	}

	var lines []string

	crumb := greyStyle.Render("Logs › ")
	lines = append(lines, crumb+titleStyle.Render("Details"))

	lines = append(lines, "")
	lines = append(lines, sectionHeader("Metadata", inner))
	lines = append(lines, detailKV("Time", e.Timestamp.Format("15:04:05.000")))
	lines = append(lines, detailKV("Level", strings.ToUpper(e.Level)))
	if e.Logger != "" {
		lines = append(lines, detailKV("Logger", e.Logger))
	}

	lines = append(lines, "")
	lines = append(lines, sectionHeader("Message", inner))
	msg := e.Message
	if msg == "" {
		msg = "—"
	}
	// Wrap message
	wrappedMsg := lipgloss.NewStyle().Width(inner).Render(msg)
	lines = append(lines, wrappedMsg)

	if len(e.Attributes) > 0 {
		lines = append(lines, "")
		lines = append(lines, sectionHeader("Attributes", inner))
		keys := slices.Sorted(maps.Keys(e.Attributes))
		for _, k := range keys {
			val := fmt.Sprintf("%v", e.Attributes[k])
			// If value is long, it will be wrapped by detailKV or we might need manual wrapping
			lines = append(lines, detailKV(k, val))
		}
	}

	lines = append(lines, "")
	lines = append(lines, helpStyle.Render("  "+helpKeyStyle.Render("Esc")+" close"))

	content := strings.Join(lines, "\n")

	contentHeight := lipgloss.Height(content)
	boxChrome := 2
	available := height - boxChrome
	if contentHeight < available {
		content += strings.Repeat("\n", available-contentHeight)
	}

	return boxStyle.Width(width - 2).Render(content)
}
