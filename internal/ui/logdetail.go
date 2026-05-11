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

	if e.IsAccessLog() {
		lines = append(lines, "")
		lines = append(lines, sectionHeader("Request", inner))
		if e.Host != "" {
			lines = append(lines, detailKV("Host", e.Host))
		}
		if e.Method != "" {
			lines = append(lines, detailKV("Method", e.Method))
		}
		if e.URI != "" {
			lines = append(lines, detailKV("URI", e.URI))
		}
		if e.Status != 0 {
			lines = append(lines, detailKV("Status", fmt.Sprintf("%d", e.Status)))
		}
		if e.Duration > 0 {
			lines = append(lines, detailKV("Duration", fmt.Sprintf("%.3fs", e.Duration)))
		}
		if e.Size > 0 {
			lines = append(lines, detailKV("Size", formatBytes(e.Size)))
		}
		if e.RemoteIP != "" {
			lines = append(lines, detailKV("Remote IP", e.RemoteIP))
		}
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

		if width > 80 {
			// Two columns for attributes when we have space (e.g. bottom panel)
			colWidth := (inner / 2) - 2
			var leftCol, rightCol []string
			for i, k := range keys {
				val := fmt.Sprintf("%v", e.Attributes[k])
				kv := detailKV(k, val)
				if i%2 == 0 {
					leftCol = append(leftCol, kv)
				} else {
					rightCol = append(rightCol, kv)
				}
			}
			for i := 0; i < len(leftCol); i++ {
				line := lipgloss.NewStyle().Width(colWidth).Render(leftCol[i])
				if i < len(rightCol) {
					line = lipgloss.JoinHorizontal(lipgloss.Top, line, "  ", rightCol[i])
				}
				lines = append(lines, line)
			}
		} else {
			for _, k := range keys {
				val := fmt.Sprintf("%v", e.Attributes[k])
				lines = append(lines, detailKV(k, val))
			}
		}
	}

	lines = append(lines, "")
	lines = append(lines, helpStyle.Render("  "+helpKeyStyle.Render("Esc")+" close"))

	content := fitPanelContent(strings.Join(lines, "\n"), height)
	return boxStyle.Width(width - 2).Height(height - 2).Render(content)
}
