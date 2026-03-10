package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

const (
	detailPanelWidth    = 38
	detailPanelHeight   = 10
	detailSideThreshold = 115
)

func renderDetailPanel(t fetcher.ThreadDebugState, leakStatus model.LeakStatus, width, height int) string {
	inner := width - 4 // border + padding
	if inner < 10 {
		inner = 10
	}

	title := fmt.Sprintf("Thread #%d", t.Index)
	name := t.Name
	if len(name) > inner {
		name = name[:inner-1] + "…"
	}

	var lines []string
	lines = append(lines, titleStyle.Render(title))
	lines = append(lines, greyStyle.Render(name))
	lines = append(lines, "")
	lines = append(lines, stateDetailLine(t))

	if t.IsBusy {
		if t.CurrentMethod != "" {
			lines = append(lines, fmt.Sprintf("  %s %s", t.CurrentMethod, truncateURI(t.CurrentURI, inner-2)))
		}
		if t.RequestStartedAt > 0 {
			elapsed := time.Since(time.UnixMilli(t.RequestStartedAt))
			lines = append(lines, fmt.Sprintf("  Duration: %dms", elapsed.Milliseconds()))
		}
	} else if t.IsWaiting && t.WaitingSinceMilliseconds > 0 {
		d := time.Duration(t.WaitingSinceMilliseconds) * time.Millisecond
		lines = append(lines, fmt.Sprintf("  Idle: %.1fs", d.Seconds()))
	}

	if t.MemoryUsage > 0 {
		lines = append(lines, fmt.Sprintf("  Memory: %s", formatBytes(t.MemoryUsage)))
	}
	if t.RequestCount > 0 {
		lines = append(lines, fmt.Sprintf("  Requests: %s", formatNumber(t.RequestCount)))
	}

	if leakStatus.Leaking {
		lines = append(lines, "")
		lines = append(lines, leakStyle.Render("  ⚠ Possible leak"))
	}

	lines = append(lines, "")
	lines = append(lines, helpStyle.Render("  [r] restart  [Esc] close"))

	content := strings.Join(lines, "\n")

	contentHeight := lipgloss.Height(content)
	boxChrome := 2 // top + bottom border
	available := height - boxChrome
	if contentHeight < available {
		content += strings.Repeat("\n", available-contentHeight)
	}

	return boxStyle.Width(width - 2).Render(content)
}

func truncateURI(uri string, maxLen int) string {
	if len(uri) <= maxLen {
		return uri
	}
	if maxLen < 4 {
		return uri[:maxLen]
	}
	return uri[:maxLen-1] + "…"
}

func stateDetailLine(t fetcher.ThreadDebugState) string {
	switch {
	case t.IsBusy:
		return "  State: " + busyStyle.Render("● busy")
	case t.IsWaiting:
		return "  State: " + idleStyle.Render("○ idle")
	default:
		return "  State: " + greyStyle.Render("◌ "+t.State)
	}
}

func formatBytes(b int64) string {
	mb := float64(b) / 1024 / 1024
	if mb >= 1 {
		return fmt.Sprintf("%.0f MB", mb)
	}
	kb := float64(b) / 1024
	return fmt.Sprintf("%.0f KB", kb)
}
