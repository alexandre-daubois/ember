package ui

import (
	"fmt"

	"github.com/alexandredaubois/frankentop/internal/model"
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
		"  RPS %-8.0f Avg %.1fms     Queue %.0f",
		d.RPS, d.AvgTime, snap.Metrics.QueueDepth,
	)

	workerTotal := int(snap.Metrics.TotalThreads)
	line3 := fmt.Sprintf(
		"  Workers: %d idle · %d busy · %.0f crashed    Threads: %.0f/%d",
		d.TotalIdle, d.TotalBusy, d.TotalCrashes,
		snap.Metrics.BusyThreads, workerTotal,
	)

	title := titleStyle.Render(" FrankenTop v0.1.0 ")
	content := lipgloss.JoinVertical(lipgloss.Left, line1, line2, line3)

	box := boxStyle.Width(width - 2).Render(
		lipgloss.JoinVertical(lipgloss.Center, title, content),
	)

	return box
}
