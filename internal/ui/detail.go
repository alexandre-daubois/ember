package ui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/charmbracelet/lipgloss"
)

const (
	detailPanelWidth    = 44
	detailPanelHeight   = 14
	detailSideThreshold = 120
)

var (
	detailLabelStyle = lipgloss.NewStyle().Foreground(subtle).Width(10)
	detailValStyle   = lipgloss.NewStyle()
	sectionStyle     = lipgloss.NewStyle().Foreground(subtle)
)

func renderDetailPanel(t fetcher.ThreadDebugState, width, height int, memSamples []int64, now time.Time) string {
	inner := width - 4
	if inner < 10 {
		inner = 10
	}

	var lines []string

	title := fmt.Sprintf("Thread #%d", t.Index)
	lines = append(lines, titleStyle.Render(title))

	if script := workerScript(t.Name); script != "" {
		lines = append(lines, greyStyle.Render("worker"))
		if len(script) > inner {
			script = script[:inner-1] + "…"
		}
		lines = append(lines, greyStyle.Render(script))
	} else {
		name := t.Name
		if len(name) > inner {
			name = name[:inner-1] + "…"
		}
		lines = append(lines, greyStyle.Render(name))
	}

	lines = append(lines, "")
	lines = append(lines, renderStateBadge(t))

	if t.IsBusy && (t.CurrentMethod != "" || t.RequestStartedAt > 0) {
		lines = append(lines, "")
		lines = append(lines, sectionHeader("Request", inner))
		if t.CurrentMethod != "" {
			lines = append(lines, detailKV("Method", t.CurrentMethod))
		}
		if t.CurrentURI != "" {
			uri := truncateURI(t.CurrentURI, inner-10)
			lines = append(lines, detailKV("URI", uri))
		}
		if t.RequestStartedAt > 0 {
			elapsed := now.Sub(time.UnixMilli(t.RequestStartedAt))
			lines = append(lines, detailKV("Duration", formatDuration(elapsed)))
		}
	} else if t.IsWaiting && t.WaitingSinceMilliseconds > 0 {
		lines = append(lines, "")
		d := time.Duration(t.WaitingSinceMilliseconds) * time.Millisecond
		lines = append(lines, detailKV("Idle for", formatDuration(d)))
	}

	if t.MemoryUsage > 0 || t.RequestCount > 0 {
		lines = append(lines, "")
		lines = append(lines, sectionHeader("Resources", inner))
		if t.MemoryUsage > 0 {
			lines = append(lines, detailKV("Memory", formatBytes(t.MemoryUsage)))
			if spark := renderMemSparkline(memSamples, inner-12); spark != "" {
				lines = append(lines, detailKV("", spark))
			}
		}
		if t.RequestCount > 0 {
			lines = append(lines, detailKV("Requests", formatNumber(t.RequestCount)))
		}
	}

	lines = append(lines, "")
	lines = append(lines, helpStyle.Render("  "+helpKeyStyle.Render("r")+" restart  "+helpKeyStyle.Render("Esc")+" close"))

	content := strings.Join(lines, "\n")

	contentHeight := lipgloss.Height(content)
	boxChrome := 2
	available := height - boxChrome
	if contentHeight < available {
		content += strings.Repeat("\n", available-contentHeight)
	}

	return boxStyle.Width(width - 2).Render(content)
}

func renderStateBadge(t fetcher.ThreadDebugState) string {
	switch {
	case t.IsBusy:
		return "  " + busyStyle.Render("● BUSY")
	case t.IsWaiting:
		return "  " + idleStyle.Render("○ IDLE")
	default:
		return "  " + greyStyle.Render("◌ "+strings.ToUpper(t.State))
	}
}

func sectionHeader(label string, width int) string {
	prefix := " ── " + label + " "
	remaining := width - utf8.RuneCountInString(prefix)
	if remaining > 0 {
		prefix += strings.Repeat("─", remaining)
	}
	return sectionStyle.Render(prefix)
}

func detailKV(label, value string) string {
	return "  " + detailLabelStyle.Render(label) + detailValStyle.Render(value)
}

func formatDuration(d time.Duration) string {
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	case d >= time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%.1fm", d.Minutes())
	case d >= 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
}

func renderMemSparkline(samples []int64, maxWidth int) string {
	if len(samples) < 2 || maxWidth < 2 {
		return ""
	}
	if maxWidth > 16 {
		maxWidth = 16
	}

	data := samples
	if len(data) > maxWidth {
		data = data[len(data)-maxWidth:]
	}

	var minVal, maxVal int64
	minVal, maxVal = data[0], data[0]
	for _, v := range data[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	var b strings.Builder
	for _, v := range data {
		if maxVal == minVal {
			b.WriteRune(sparkBlocks[0])
			continue
		}
		idx := int(float64(v-minVal) / float64(maxVal-minVal) * float64(len(sparkBlocks)-1))
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		b.WriteRune(sparkBlocks[idx])
	}
	return greyStyle.Render(b.String())
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

func formatBytes(b int64) string {
	mb := float64(b) / 1024 / 1024
	if mb >= 1 {
		return fmt.Sprintf("%.0f MB", mb)
	}
	kb := float64(b) / 1024
	if kb >= 1 {
		return fmt.Sprintf("%.0f KB", kb)
	}
	return fmt.Sprintf("%d B", b)
}
