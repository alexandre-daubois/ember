package ui

import (
	"fmt"
	"math"
	"strings"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

func renderDashboard(s *model.State, width int, version string, rpsHistory, cpuHistory []float64, stale bool, hasFrankenPHP bool) string {
	if width < 10 {
		return "…"
	}
	if s.Current == nil {
		return greyStyle.Render(" Waiting for data...")
	}

	snap := s.Current
	d := s.Derived
	p := snap.Process

	rss := float64(p.RSS) / 1024 / 1024
	uptime := model.FormatUptime(p.Uptime)

	// title line: left-aligned title + right-aligned config
	titleLeft := titleStyle.Render(fmt.Sprintf(" Ember %s", version))
	if stale {
		titleLeft += " " + warnStyle.Render("STALE")
	}

	var threadBusy, threadIdle, threadTotal int
	for _, t := range snap.Threads.ThreadDebugStates {
		if workerScript(t.Name) != "" {
			continue
		}
		threadTotal++
		if t.IsBusy {
			threadBusy++
		} else if t.IsWaiting {
			threadIdle++
		}
	}

	var configParts []string
	if hasFrankenPHP {
		workerThreadCount := countWorkerThreads(snap.Threads.ThreadDebugStates)
		configParts = []string{fmt.Sprintf("%d threads", len(snap.Threads.ThreadDebugStates))}
		if workerThreadCount > 0 {
			configParts = append([]string{fmt.Sprintf("%d workers", workerThreadCount)}, configParts...)
		}
		if snap.Threads.ReservedThreadCount > 0 {
			configParts = append(configParts, fmt.Sprintf("%d reserved", snap.Threads.ReservedThreadCount))
		}
	} else {
		hostCount := len(snap.Metrics.Hosts)
		if hostCount > 0 {
			configParts = []string{fmt.Sprintf("%d hosts", hostCount)}
		}
	}
	configRight := greyStyle.Render(strings.Join(configParts, " · ") + " ")
	gap := width - lipgloss.Width(titleLeft) - lipgloss.Width(configRight)
	if gap < 1 {
		gap = 1
	}
	titleLine := titleLeft + strings.Repeat(" ", gap) + configRight

	if d.TotalCrashes > 0 {
		crashStr := fmt.Sprintf("%.0f", d.TotalCrashes)
		configRight = dangerStyle.Render(crashStr+" crashed") + "  " + configRight
	}

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
	line1 := fmt.Sprintf(" CPU %s %s  RSS %s  Uptime %s", cpuRaw, cpuSpark, rssStr, uptimeStr)

	// line 2: RPS (colored), sparkline, Avg (colored), In-flight, Queue
	rpsFmt := fmt.Sprintf("%-7s", fmt.Sprintf("%.0f", d.RPS))
	if d.RPS > 0 {
		rpsFmt = idleStyle.Render(rpsFmt)
	}
	avgRaw := fmt.Sprintf("%-10s", formatMs(d.AvgTime))
	switch {
	case d.AvgTime >= 1000:
		avgRaw = dangerStyle.Render(fmt.Sprintf("%-10s", formatMs(d.AvgTime)))
	case d.AvgTime >= 500:
		avgRaw = warnStyle.Render(fmt.Sprintf("%-10s", formatMs(d.AvgTime)))
	}
	inflightStr := fmt.Sprintf("%-4s", fmt.Sprintf("%.0f", snap.Metrics.HTTPRequestsInFlight))
	rpsSpark := renderSparkline(rpsHistory, sparklineSize)
	line2 := fmt.Sprintf(" RPS %s %s  Avg %s  In-flight %s",
		rpsFmt, rpsSpark, avgRaw, inflightStr)
	if hasFrankenPHP {
		queueRaw := fmt.Sprintf("%-4s", fmt.Sprintf("%.0f", snap.Metrics.QueueDepth))
		if snap.Metrics.QueueDepth > 0 {
			queueRaw = warnStyle.Render(fmt.Sprintf("%-4s", fmt.Sprintf("%.0f", snap.Metrics.QueueDepth)))
		}
		line2 += fmt.Sprintf("  Queue %s", queueRaw)
	}

	separator := separatorStyle.Render(strings.Repeat("─", width))

	var lines []string
	lines = append(lines, titleLine, line1, line2)

	if hasFrankenPHP {
		threadInactive := threadTotal - threadBusy - threadIdle
		legend := fmt.Sprintf(" %s %s %s",
			busyStyle.Render(fmt.Sprintf("%d busy", threadBusy)),
			idleStyle.Render(fmt.Sprintf("%d idle", threadIdle)),
			greyStyle.Render(fmt.Sprintf("%d inactive", threadInactive)),
		)
		legendW := lipgloss.Width(legend)
		threadLabel := fmt.Sprintf(" Threads %d/%d ", threadBusy, threadTotal)
		barMaxW := width - len(threadLabel) - legendW - 4
		threadBar := renderThreadBar(threadBusy, threadIdle, threadTotal, barMaxW)
		lines = append(lines, threadLabel+threadBar+legend)
	}

	if !snap.Metrics.HasHTTPMetrics && len(snap.Threads.ThreadDebugStates) == 0 {
		lines = append(lines, warnStyle.Render(" ⚠ No metrics — add `metrics` to Caddyfile global block"))
	}

	lines = append(lines, separator)

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

func renderThreadBar(busy, idle, total, maxWidth int) string {
	if total == 0 || maxWidth < 10 {
		return ""
	}
	barWidth := maxWidth

	busyW := busy * barWidth / total
	idleW := idle * barWidth / total
	inactiveW := barWidth - busyW - idleW

	bar := busyStyle.Render(strings.Repeat("█", busyW)) +
		idleStyle.Render(strings.Repeat("█", idleW)) +
		greyStyle.Render(strings.Repeat("░", inactiveW))

	return "[" + bar + "]"
}

const workerPrefix = "Worker PHP Thread - "

func workerScript(name string) string {
	if strings.HasPrefix(name, workerPrefix) {
		return name[len(workerPrefix):]
	}
	return ""
}

func countWorkerThreads(threads []fetcher.ThreadDebugState) int {
	count := 0
	for _, t := range threads {
		if workerScript(t.Name) != "" {
			count++
		}
	}
	return count
}

func countWorkerScripts(threads []fetcher.ThreadDebugState) int {
	seen := make(map[string]struct{})
	for _, t := range threads {
		if s := workerScript(t.Name); s != "" {
			seen[s] = struct{}{}
		}
	}
	return len(seen)
}

func formatMs(ms float64) string {
	if ms >= 10000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	return fmt.Sprintf("%.1fms", ms)
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
	hint1 := "  Make sure Caddy is running with the admin API enabled:"
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
