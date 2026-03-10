package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

type renderOpts struct {
	slowThreshold time.Duration
	leakWatcher   *model.LeakWatcher
	leakEnabled   bool
}

// fixed column widths (excluding URI which is dynamic)
const (
	colIndex  = 5
	colState  = 12 // 11 content + 1 trailing space
	colMethod = 9  // 8 content + 1 trailing space
	colTime   = 12 // 11 content + 1 trailing space
	colMem    = 10 // 9 content + 1 trailing space
	colReqs   = 9
	colFixed  = 1 + colIndex + colState + colMethod + colTime + colMem + colReqs // 1 for prefix
)

func uriWidth(totalWidth int) int {
	w := totalWidth - colFixed
	if w < 10 {
		w = 10
	}
	return w
}

func renderWorkerListFromThreads(threads []fetcher.ThreadDebugState, cursor int, width int, sortBy model.SortField, opts renderOpts) string {
	if len(threads) == 0 {
		return greyStyle.Render(" No threads")
	}

	uriW := uriWidth(width)

	colHead := func(label string, field model.SortField, w int, right bool) string {
		if sortBy == field {
			label += " ▼"
		}
		if right {
			return fmt.Sprintf("%*s", w, label)
		}
		return fmt.Sprintf("%-*s", w, label)
	}

	header := fmt.Sprintf(" %-*s%-*s%-*s%-*s%*s%*s%*s",
		colIndex, colHead("#", model.SortByIndex, colIndex, false),
		colState, colHead("State", model.SortByState, colState, false),
		colMethod, colHead("Method", model.SortByMethod, colMethod, false),
		uriW, colHead("URI", model.SortByURI, uriW, false),
		colTime, colHead("Time", model.SortByTime, colTime, true),
		colMem, colHead("Mem", model.SortByMemory, colMem, true),
		colReqs, colHead("Reqs", model.SortByRequests, colReqs, true),
	)
	headerLine := tableHeaderStyle.Width(width).Render(header)

	var rows []string
	lastGroup := ""
	for i, t := range threads {
		group := threadGroup(t)
		if group != lastGroup {
			sep := " ── " + group + " "
			remaining := width - utf8.RuneCountInString(sep)
			if remaining > 0 {
				sep += strings.Repeat("─", remaining)
			}
			rows = append(rows, greyStyle.Render(sep))
			lastGroup = group
		}
		row := formatThreadRow(t, width, uriW, opts, i == cursor)
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, content)
}

func threadGroup(t fetcher.ThreadDebugState) string {
	if s := workerScript(t.Name); s != "" {
		return "(Worker script) " + s
	}
	return "threads"
}

func formatThreadRow(t fetcher.ThreadDebugState, width int, uriW int, opts renderOpts, selected bool) string {
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

	var suffix string
	if opts.leakEnabled && opts.leakWatcher != nil {
		ls := opts.leakWatcher.Status(t.Index)
		if ls.Leaking {
			suffix = leakStyle.Render(" ⚠ leak?")
		}
	}

	method := "—"
	uri := "—"
	if t.IsBusy && t.CurrentMethod != "" {
		method = t.CurrentMethod
	}
	if t.IsBusy && t.CurrentURI != "" {
		uri = t.CurrentURI
		if len(uri) > uriW {
			uri = uri[:uriW-1] + "…"
		}
	}

	memStr := "—"
	if t.MemoryUsage > 0 {
		memStr = formatBytes(t.MemoryUsage)
	}

	reqsStr := "—"
	if t.RequestCount > 0 {
		reqsStr = formatNumber(t.RequestCount)
	}

	prefix := " "
	if selected {
		prefix = ">"
		style = style.Reverse(true)
		timeStyle = timeStyle.Reverse(true)
	}

	methodStr := fmt.Sprintf("%-*s", colMethod, method)
	uriStr := fmt.Sprintf("%-*s", uriW, uri)
	memFmt := fmt.Sprintf("%*s", colMem, memStr)
	reqsFmt := fmt.Sprintf("%*s", colReqs, reqsStr)

	if selected {
		methodStr = selectedRowStyle.Render(methodStr)
		uriStr = selectedRowStyle.Render(uriStr)
		memFmt = selectedRowStyle.Render(memFmt)
		reqsFmt = selectedRowStyle.Render(reqsFmt)
	}

	row := fmt.Sprintf("%s%-*d%s%s%s%s%s%s%s",
		prefix,
		colIndex, t.Index,
		style.Render(fmt.Sprintf("%-*s", colState, stateIcon)),
		methodStr,
		uriStr,
		timeStyle.Render(fmt.Sprintf("%*s", colTime, timeStr)),
		memFmt,
		reqsFmt,
		suffix,
	)

	if selected {
		return selectedRowStyle.Width(width).Render(row)
	}
	return row
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
		gi, gj := threadGroup(sorted[i]), threadGroup(sorted[j])
		if gi != gj {
			return gi < gj
		}
		switch by {
		case model.SortByState:
			return stateOrder(sorted[i]) < stateOrder(sorted[j])
		case model.SortByMethod:
			return sorted[i].CurrentMethod < sorted[j].CurrentMethod
		case model.SortByURI:
			return sorted[i].CurrentURI < sorted[j].CurrentURI
		case model.SortByMemory:
			return sorted[i].MemoryUsage > sorted[j].MemoryUsage
		case model.SortByRequests:
			return sorted[i].RequestCount > sorted[j].RequestCount
		case model.SortByTime:
			return threadElapsedMs(sorted[i]) > threadElapsedMs(sorted[j])
		default:
			return sorted[i].Index < sorted[j].Index
		}
	})

	return sorted
}

func threadElapsedMs(t fetcher.ThreadDebugState) int64 {
	if t.IsBusy && t.RequestStartedAt > 0 {
		return time.Since(time.UnixMilli(t.RequestStartedAt)).Milliseconds()
	}
	if t.IsWaiting {
		return t.WaitingSinceMilliseconds
	}
	return 0
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

func formatNumber(n int64) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}
