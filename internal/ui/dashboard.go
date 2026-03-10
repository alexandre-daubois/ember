package ui

import (
	"fmt"

	"github.com/alexandredaubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

func renderDashboard(s *model.State, width int) string {
	if s.Current == nil {
		return boxStyle.Width(width - 2).Render("Waiting for data...")
	}

	snap := s.Current
	d := s.Derived
	p := snap.Process

	rss := float64(p.RSS) / 1024 / 1024
	uptime := model.FormatUptime(p.Uptime)

	line1 := fmt.Sprintf(
		"  CPU %.1f%%    RSS %.0f MB    Uptime %s",
		p.CPUPercent, rss, uptime,
	)

	line2 := fmt.Sprintf(
		"  RPS %-8.0f Avg %.1fms     In-flight %.0f   Queue %.0f",
		d.RPS, d.AvgTime, snap.Metrics.HTTPRequestsInFlight, snap.Metrics.QueueDepth,
	)

	threadTotal := len(snap.Threads.ThreadDebugStates)
	line3 := fmt.Sprintf(
		"  Workers: %d idle · %d busy · %.0f crashed    Threads: %d/%d",
		d.TotalIdle, d.TotalBusy, d.TotalCrashes,
		d.TotalBusy, threadTotal,
	)

	title := titleStyle.Render(" Ember 0.1 ")

	hasWorkerMetrics := len(snap.Metrics.Workers) > 0
	hasHTTPMetrics := snap.Metrics.HasHTTPMetrics

	var lines []string
	lines = append(lines, line1, line2, line3)

	if !hasWorkerMetrics && !hasHTTPMetrics {
		lines = append(lines, warnStyle.Render("  ⚠ No metrics, add `metrics` to your Caddyfile global block!"))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)

	box := boxStyle.Width(width - 2).Render(
		lipgloss.JoinVertical(lipgloss.Center, title, content),
	)

	return box
}
