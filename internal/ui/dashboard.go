package ui

import (
	"fmt"
	"strings"

	"github.com/alexandredaubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

func renderDashboard(s *model.State, width int, version string) string {
	if s.Current == nil {
		return boxStyle.Width(width - 2).Render("Waiting for data...")
	}

	snap := s.Current
	d := s.Derived
	p := snap.Process

	rss := float64(p.RSS) / 1024 / 1024
	uptime := model.FormatUptime(p.Uptime)

	title := titleStyle.Render(fmt.Sprintf(" Ember %s ", version))

	// line 1: CPU (colored), RSS, Uptime
	cpuStr := fmt.Sprintf("%.1f%%", p.CPUPercent)
	switch {
	case p.CPUPercent >= 150:
		cpuStr = dangerStyle.Render(cpuStr)
	case p.CPUPercent >= 80:
		cpuStr = warnStyle.Render(cpuStr)
	default:
		cpuStr = idleStyle.Render(cpuStr)
	}
	line1 := fmt.Sprintf("  CPU %s    RSS %.0f MB    Uptime %s", cpuStr, rss, uptime)

	// line 2: RPS (colored), Avg (colored), In-flight, Queue
	rpsStr := fmt.Sprintf("%.0f", d.RPS)
	if d.RPS > 0 {
		rpsStr = idleStyle.Render(rpsStr)
	}
	avgStr := fmt.Sprintf("%.1fms", d.AvgTime)
	switch {
	case d.AvgTime >= 1000:
		avgStr = dangerStyle.Render(avgStr)
	case d.AvgTime >= 500:
		avgStr = warnStyle.Render(avgStr)
	}
	queueStr := fmt.Sprintf("%.0f", snap.Metrics.QueueDepth)
	if snap.Metrics.QueueDepth > 0 {
		queueStr = warnStyle.Render(queueStr)
	}
	line2 := fmt.Sprintf("  RPS %-8s Avg %-10s In-flight %.0f   Queue %s",
		rpsStr, avgStr, snap.Metrics.HTTPRequestsInFlight, queueStr)

	// line 3: Workers + thread bar
	threadTotal := len(snap.Threads.ThreadDebugStates)
	crashStr := fmt.Sprintf("%.0f", d.TotalCrashes)
	if d.TotalCrashes > 0 {
		crashStr = dangerStyle.Render(crashStr)
	}

	threadBar := renderThreadBar(d.TotalBusy, d.TotalIdle, threadTotal, width-40)
	line3 := fmt.Sprintf("  Workers: %d idle · %d busy · %s crashed    Threads: %d/%d",
		d.TotalIdle, d.TotalBusy, crashStr, d.TotalBusy, threadTotal)

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
