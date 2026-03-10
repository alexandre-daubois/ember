package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

func renderDetail(t fetcher.ThreadDebugState, leakStatus model.LeakStatus, width, height int) string {
	w := width - 10
	if w > 70 {
		w = 70
	}
	if w < 40 {
		w = 40
	}

	title := fmt.Sprintf("Thread #%d — %s", t.Index, t.Name)

	var lines []string
	lines = append(lines, stateDetailLine(t))

	if t.IsBusy {
		lines = append(lines, fmt.Sprintf("  Current: %s %s", t.CurrentMethod, t.CurrentURI))
		if t.RequestStartedAt > 0 {
			elapsed := time.Since(time.UnixMilli(t.RequestStartedAt))
			lines = append(lines, fmt.Sprintf("  Duration: %dms", elapsed.Milliseconds()))
		}
	} else if t.IsWaiting && t.WaitingSinceMilliseconds > 0 {
		d := time.Duration(t.WaitingSinceMilliseconds) * time.Millisecond
		lines = append(lines, fmt.Sprintf("  Idle for: %.1fs", d.Seconds()))
	}

	if t.MemoryUsage > 0 {
		lines = append(lines, fmt.Sprintf("  Memory: %s", formatBytes(t.MemoryUsage)))
	}
	if t.RequestCount > 0 {
		lines = append(lines, fmt.Sprintf("  Requests handled: %d", t.RequestCount))
	}

	if len(leakStatus.Samples) > 0 {
		var memStrs []string
		for _, s := range leakStatus.Samples {
			memStrs = append(memStrs, formatBytes(s))
		}
		lines = append(lines, fmt.Sprintf("  Idle memory history: %s", strings.Join(memStrs, " → ")))
		if leakStatus.Leaking {
			lines = append(lines, leakStyle.Render("  ⚠ Possible memory leak detected"))
		}
	}

	lines = append(lines, "")
	lines = append(lines, helpStyle.Render("  [r] restart all workers   [Esc] back"))

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)

	popup := boxStyle.
		Width(w).
		Render(lipgloss.JoinVertical(lipgloss.Left, titleStyle.Render(title), "", content))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup)
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
