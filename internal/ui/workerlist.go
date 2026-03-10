package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alexandredaubois/frankentop/internal/fetcher"
	"github.com/alexandredaubois/frankentop/internal/model"
	"github.com/charmbracelet/lipgloss"
)

type renderOpts struct {
	slowThreshold time.Duration
	leakWatcher   *model.LeakWatcher
	leakEnabled   bool
}

func renderWorkerListFromThreads(threads []fetcher.ThreadDebugState, cursor int, width int, opts renderOpts) string {
	if len(threads) == 0 {
		return greyStyle.Render(" No threads")
	}

	header := fmt.Sprintf(" %-4s %-10s %-40s %10s", "#", "State", "Name", "Time")
	headerLine := tableHeaderStyle.Width(width).Render(header)

	var rows []string
	for i, t := range threads {
		row := formatThreadRow(t, width, opts)
		if i == cursor {
			row = selectedRowStyle.Width(width).Render(row)
		}
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, content)
}

func formatThreadRow(t fetcher.ThreadDebugState, width int, opts renderOpts) string {
	var stateIcon string
	var style lipgloss.Style

	switch {
	case t.IsBusy:
		stateIcon = "● busy"
		style = busyStyle
	case t.IsWaiting:
		stateIcon = "○ idle"
		style = idleStyle
	default:
		stateIcon = "◌ " + t.State
		style = greyStyle
	}

	timeStr, timeStyle := formatTimeWithStyle(t, opts.slowThreshold)

	name := t.Name
	maxName := width - 30
	if maxName > 0 && len(name) > maxName {
		name = name[:maxName-1] + "…"
	}

	var suffix string
	if opts.leakEnabled && opts.leakWatcher != nil {
		ls := opts.leakWatcher.Status(t.Index)
		if ls.Leaking {
			suffix = leakStyle.Render(" ⚠ leak?")
		}
	}

	return fmt.Sprintf(" %-4d %s %-40s %s%s",
		t.Index,
		style.Render(fmt.Sprintf("%-10s", stateIcon)),
		name,
		timeStyle.Render(fmt.Sprintf("%10s", timeStr)),
		suffix,
	)
}

func formatTimeWithStyle(t fetcher.ThreadDebugState, slowThreshold time.Duration) (string, lipgloss.Style) {
	if slowThreshold == 0 {
		slowThreshold = 500 * time.Millisecond
	}

	if t.IsBusy && t.RequestStartedAt > 0 {
		elapsed := time.Since(time.UnixMilli(t.RequestStartedAt))
		text := fmt.Sprintf("%dms", elapsed.Milliseconds())
		switch {
		case elapsed >= slowThreshold*2:
			return text, dangerStyle
		case elapsed >= slowThreshold:
			return text, warnStyle
		default:
			return text, lipgloss.NewStyle()
		}
	}

	if t.IsWaiting && t.WaitingSinceMilliseconds > 0 {
		d := time.Duration(t.WaitingSinceMilliseconds) * time.Millisecond
		if d >= time.Second {
			return fmt.Sprintf("%.1fs idle", d.Seconds()), greyStyle
		}
		return fmt.Sprintf("%dms idle", d.Milliseconds()), greyStyle
	}

	return "—", greyStyle
}

func formatTime(t fetcher.ThreadDebugState) string {
	s, _ := formatTimeWithStyle(t, 500*time.Millisecond)
	return s
}

func sortThreads(threads []fetcher.ThreadDebugState, by model.SortField) []fetcher.ThreadDebugState {
	sorted := make([]fetcher.ThreadDebugState, len(threads))
	copy(sorted, threads)

	sort.SliceStable(sorted, func(i, j int) bool {
		switch by {
		case model.SortByState:
			return stateOrder(sorted[i]) < stateOrder(sorted[j])
		case model.SortByMemory:
			return sorted[i].MemoryUsage > sorted[j].MemoryUsage
		case model.SortByRequests:
			return sorted[i].RequestCount > sorted[j].RequestCount
		case model.SortByTime:
			return sorted[i].WaitingSinceMilliseconds < sorted[j].WaitingSinceMilliseconds
		default:
			return sorted[i].Index < sorted[j].Index
		}
	})

	return sorted
}

func stateOrder(t fetcher.ThreadDebugState) int {
	if t.IsBusy {
		return 0
	}
	if t.IsWaiting {
		return 1
	}
	return 2
}
