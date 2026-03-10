package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/alexandredaubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

func renderDashboard(s *model.State, width int, version string, rpsHistory, cpuHistory []float64) string {
	if s.Current == nil {
		return boxStyle.Width(width - 2).Render("Waiting for data...")
	}

	snap := s.Current
	d := s.Derived
	p := snap.Process

	rss := float64(p.RSS) / 1024 / 1024
	uptime := model.FormatUptime(p.Uptime)

	title := titleStyle.Render(fmt.Sprintf(" Ember %s ", version))

	// line 1: CPU (colored), sparkline, RSS, Uptime
	cpuRaw := fmt.Sprintf("%-7s", fmt.Sprintf("%.1f%%", p.CPUPercent))
	switch {
	case p.CPUPercent >= 150:
		cpuRaw = dangerStyle.Render(cpuRaw)
	case p.CPUPercent >= 80:
		cpuRaw = warnStyle.Render(cpuRaw)
	default:
		cpuRaw = idleStyle.Render(cpuRaw)
	}
	cpuSpark := renderSparkline(cpuHistory, sparklineSize)
	rssStr := fmt.Sprintf("%-8s", fmt.Sprintf("%.0f MB", rss))
	uptimeStr := fmt.Sprintf("%-10s", uptime)
	line1 := fmt.Sprintf("  CPU %s %s  RSS %s  Uptime %s", cpuRaw, cpuSpark, rssStr, uptimeStr)

	// line 2: RPS (colored), sparkline, Avg (colored), In-flight, Queue
	rpsRaw := fmt.Sprintf("%-7s", fmt.Sprintf("%.0f", d.RPS))
	if d.RPS > 0 {
		rpsRaw = idleStyle.Render(fmt.Sprintf("%-7s", fmt.Sprintf("%.0f", d.RPS)))
	}
	avgRaw := fmt.Sprintf("%-10s", fmt.Sprintf("%.1fms", d.AvgTime))
	switch {
	case d.AvgTime >= 1000:
		avgRaw = dangerStyle.Render(fmt.Sprintf("%-10s", fmt.Sprintf("%.1fms", d.AvgTime)))
	case d.AvgTime >= 500:
		avgRaw = warnStyle.Render(fmt.Sprintf("%-10s", fmt.Sprintf("%.1fms", d.AvgTime)))
	}
	inflightStr := fmt.Sprintf("%-4s", fmt.Sprintf("%.0f", snap.Metrics.HTTPRequestsInFlight))
	queueRaw := fmt.Sprintf("%-4s", fmt.Sprintf("%.0f", snap.Metrics.QueueDepth))
	if snap.Metrics.QueueDepth > 0 {
		queueRaw = warnStyle.Render(fmt.Sprintf("%-4s", fmt.Sprintf("%.0f", snap.Metrics.QueueDepth)))
	}
	rpsSpark := renderSparkline(rpsHistory, sparklineSize)
	line2 := fmt.Sprintf("  RPS %s %s  Avg %s  In-flight %s  Queue %s",
		rpsRaw, rpsSpark, avgRaw, inflightStr, queueRaw)

	// line 3: Workers + thread bar
	threadTotal := len(snap.Threads.ThreadDebugStates)
	crashRaw := fmt.Sprintf("%-3s", fmt.Sprintf("%.0f", d.TotalCrashes))
	if d.TotalCrashes > 0 {
		crashRaw = dangerStyle.Render(fmt.Sprintf("%-3s", fmt.Sprintf("%.0f", d.TotalCrashes)))
	}

	threadBar := renderThreadBar(d.TotalBusy, d.TotalIdle, threadTotal, width-40)
	line3 := fmt.Sprintf("  Workers: %-3d idle · %-3d busy · %s crashed    Threads: %d/%d",
		d.TotalIdle, d.TotalBusy, crashRaw, d.TotalBusy, threadTotal)

	hasWorkerMetrics := len(snap.Metrics.Workers) > 0
	hasHTTPMetrics := snap.Metrics.HasHTTPMetrics

	var lines []string
	lines = append(lines, line1, line2, line3, "  "+threadBar)

	if !hasWorkerMetrics && !hasHTTPMetrics {
		lines = append(lines, warnStyle.Render("  ⚠ No metrics — add `metrics` to Caddyfile global block"))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)

	return boxStyle.Width(width - 2).Render(
		lipgloss.JoinVertical(lipgloss.Center, title, content),
	)
}

func renderThreadBar(busy, idle, total, maxWidth int) string {
	if total == 0 || maxWidth < 10 {
		return ""
	}
	barWidth := maxWidth
	if barWidth > 60 {
		barWidth = 60
	}

	busyW := busy * barWidth / total
	idleW := idle * barWidth / total
	inactiveW := barWidth - busyW - idleW

	bar := busyStyle.Render(strings.Repeat("█", busyW)) +
		idleStyle.Render(strings.Repeat("█", idleW)) +
		greyStyle.Render(strings.Repeat("░", inactiveW))

	return "[" + bar + "]"
}

var sparkBlocks = []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func renderSparkline(values []float64, fixedWidth int) string {
	if len(values) < 2 {
		return strings.Repeat(" ", fixedWidth)
	}

	maxVal := 0.0
	for _, v := range values {
		if v > maxVal {
			maxVal = v
		}
	}

	var b strings.Builder
	pad := fixedWidth - len(values)
	for i := 0; i < pad; i++ {
		b.WriteRune(' ')
	}
	for _, v := range values {
		if maxVal == 0 {
			b.WriteRune(sparkBlocks[0])
			continue
		}
		idx := int(math.Round(v / maxVal * float64(len(sparkBlocks)-1)))
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

func renderConnectionError(err string, width, height int) string {
	title := dangerStyle.Render("  Connection failed")
	msg := greyStyle.Render("  Cannot reach the Caddy admin API.")
	hint1 := "  Make sure FrankenPHP is running and the admin API is enabled:"
	hint2 := helpStyle.Render("    { admin localhost:2019 }")
	hint3 := "  Or specify a custom address:"
	hint4 := helpStyle.Render("    ember --addr http://host:port")
	retry := greyStyle.Render("  Retrying automatically...")

	content := lipgloss.JoinVertical(lipgloss.Left,
		"", title, "", msg, "", hint1, hint2, "", hint3, hint4, "", retry, "",
	)

	popup := boxStyle.Width(52).Render(content)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup)
}
