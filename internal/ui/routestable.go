package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

const (
	colRouteCount      = 10
	colRouteMethod     = 10
	colRoutePatternMax = 40
	colRouteStatus1    = 7
	colRouteAvg        = 11
	colRouteMax        = 11
	colRouteMem        = 10
	// Pattern is the only flex column; everything else is fixed so the
	// status counters and latency columns line up vertically across rows.
	colRouteFixedNonPattern = 1 + colRouteCount + colRouteMethod + 4*colRouteStatus1 + colRouteAvg + colRouteMax
)

// routeFixedNonPattern is the fixed-column footprint: the memory columns only
// exist when FrankenPHP is detected, so they only count when shown.
func routeFixedNonPattern(showMem bool) int {
	if showMem {
		return colRouteFixedNonPattern + 2*colRouteMem
	}
	return colRouteFixedNonPattern
}

// routeStatusReserve is the room the right-hand status pill needs on the header
// line. renderLogHeader truncates the column labels (never the pill) when the
// two collide, which would drop the rightmost labels and the "▼" sort marker
// while the rows kept printing those cells.
func routeStatusReserve(rightStatus string) int {
	if rightStatus == "" {
		return 0
	}
	return lipgloss.Width(rightStatus) + 2
}

// routeMemColumnsFit reports whether the memory columns can be drawn without
// costing Pattern a single cell: they only earn their 20 cells out of the slack
// a wide terminal leaves once Pattern is capped and the status pill has its
// room. Pattern is the only column that identifies a route — at the root view it
// also carries the host — so squeezing it to make room for memory would trade
// the row's identity for its footprint, and clipping the labels instead would
// leave the header advertising fewer columns than the rows print.
func routeMemColumnsFit(width, statusReserve int) bool {
	return width-statusReserve-routeFixedNonPattern(true) >= colRoutePatternMax
}

func renderRoutesTable(stats []model.RouteStat, cursor, width, height int, sortBy model.RouteSortField, showHost, showMem bool, rightStatus, emptyHint string) string {
	reserve := routeStatusReserve(rightStatus)
	if showMem && !routeMemColumnsFit(width, reserve) {
		showMem = false
	}

	// Pattern absorbs whatever width is left after the fixed columns; on
	// narrow terminals it gets squeezed (and truncated with an ellipsis by
	// fitCellLeft) rather than pushing the row past `width` and forcing the
	// terminal to wrap — keeping the table strictly within its allotment.
	remaining := width - routeFixedNonPattern(showMem)
	if remaining < 1 {
		remaining = 1
	}
	patternW := remaining
	if patternW > colRoutePatternMax {
		patternW = colRoutePatternMax
	}
	// Any leftover width pushes the right-hand columns toward the edge so a
	// wide terminal does not stretch Pattern across half the screen — and it is
	// where the status pill gets its room. With no slack to spare the labels
	// truncate as they always have, Pattern keeping every cell it had.
	gap := remaining - patternW
	if gap >= reserve {
		gap -= reserve
	}

	headerLine := renderLogHeader(buildRoutesHeader(sortBy, patternW, gap, showMem), rightStatus, width)

	bodyHeight := height - lipgloss.Height(headerLine)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var rows []string
	if len(stats) == 0 {
		rows = append(rows, greyStyle.Render(" "+emptyHint))
	}

	visible := stats
	if len(visible) > bodyHeight {
		visible = visible[:bodyHeight]
	}

	for i, s := range visible {
		rows = append(rows, formatRouteRow(s, width, patternW, gap, showHost, showMem, i == cursor))
	}

	for len(rows) < bodyHeight {
		rows = append(rows, "")
	}

	body := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, body)
}

// buildRoutesHeader pre-pads cells manually because Go's `%-*s` width
// specifier counts bytes, and lipgloss-styled labels embed ANSI escapes that
// would silently defeat the padding.
func buildRoutesHeader(sortBy model.RouteSortField, patternW, gap int, showMem bool) string {
	header := " " +
		padCellRight(routeSortLabel("Count", sortBy, model.SortByRouteCount), colRouteCount) +
		padCellRight("Method", colRouteMethod) +
		fitCellLeft(routeSortLabel("Pattern", sortBy, model.SortByRoutePattern), patternW) +
		strings.Repeat(" ", gap) +
		padCellLeft(statusLabel("2xx", okStyle), colRouteStatus1) +
		padCellLeft("3xx", colRouteStatus1) +
		padCellLeft(statusLabel("4xx", warnStyle), colRouteStatus1) +
		padCellLeft(statusLabel("5xx", dangerStyle), colRouteStatus1) +
		padCellRight(" "+routeSortLabel("Avg", sortBy, model.SortByRouteAvg), colRouteAvg) +
		padCellRight(" "+routeSortLabel("Max", sortBy, model.SortByRouteMax), colRouteMax)
	if showMem {
		header += padCellRight(" "+routeSortLabel("Avg Mem", sortBy, model.SortByRouteAvgMem), colRouteMem) +
			padCellRight(" "+routeSortLabel("Max Mem", sortBy, model.SortByRouteMaxMem), colRouteMem)
	}
	return header
}

// routeSortLabel uses the same "▼" glyph the host and upstream tables use, so
// the affordance stays consistent across the app and survives NO_COLOR.
func routeSortLabel(name string, current, target model.RouteSortField) string {
	if current == target {
		return name + " ▼"
	}
	return name
}

func statusLabel(name string, style lipgloss.Style) string {
	return style.Render(name)
}

// padCellRight / padCellLeft use lipgloss.Width (ANSI-aware) instead of the
// runewidth-based fitCell* helpers, which over-pad when the content is
// already styled.
func padCellRight(s string, cells int) string {
	pad := cells - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return s + strings.Repeat(" ", pad)
}

func padCellLeft(s string, cells int) string {
	pad := cells - lipgloss.Width(s)
	if pad <= 0 {
		return s
	}
	return strings.Repeat(" ", pad) + s
}

func formatRouteRow(s model.RouteStat, width, patternW, gap int, showHost, showMem, selected bool) string {
	prefix := " "
	if selected {
		prefix = ">"
	}

	// Method bypasses fitCellLeft (it goes straight through padCellRight), so
	// neutralise it here; pattern/host are sanitised via fitCellLeft below.
	method := sanitizeControl(s.Key.Method)
	if method == "" {
		method = "—"
	}

	pattern := s.Key.Pattern
	if pattern == "" {
		pattern = "—"
	}
	// Per-host drill-downs drop the prefix because the scope is already
	// encoded by the sidepanel; the root view keeps it so two hosts that
	// share the same path stay distinguishable.
	if showHost && s.Key.Host != "" {
		pattern = s.Key.Host + " " + pattern
	}

	avg := "—"
	if s.Count > 0 {
		avg = formatMs(s.AvgMs())
	}

	maxStr := "—"
	if s.DurationMaxMs > 0 {
		maxStr = formatMs(s.DurationMaxMs)
	}

	row := prefix +
		padCellRight(strconv.Itoa(s.Count), colRouteCount) +
		padCellRight(method, colRouteMethod) +
		highlightPattern(fitCellLeft(pattern, patternW), selected) +
		strings.Repeat(" ", gap) +
		formatRouteStatusCells(s, selected) +
		padCellRight(" "+avg, colRouteAvg) +
		padCellRight(" "+maxStr, colRouteMax)

	if showMem {
		avgMem, maxMem := "—", "—"
		if s.MemSamples > 0 {
			avgMem = formatRouteMem(int64(s.AvgMemBytes()))
		}
		if s.MemMaxBytes > 0 {
			maxMem = formatRouteMem(s.MemMaxBytes)
		}
		row += padCellRight(" "+avgMem, colRouteMem) +
			padCellRight(" "+maxMem, colRouteMem)
	}

	if selected {
		return selectedRowStyle.Width(width).Render(row)
	}
	return row
}

// formatRouteMem keeps one decimal instead of reusing formatBytes' whole-MB
// rounding: PHP route footprints routinely sit within a megabyte of each other,
// so "2 MB" on every row would hide the very differences the column exists to
// rank. Worst case ("1023.9 GB") still fits the 10-cell column.
func formatRouteMem(b int64) string {
	mb := float64(b) / 1024 / 1024
	if mb >= 1024 {
		return fmt.Sprintf("%.1f GB", mb/1024)
	}
	if mb >= 1 {
		return fmt.Sprintf("%.1f MB", mb)
	}
	return formatBytes(b)
}

// formatRouteStatusCells suffixes 4xx/5xx with "*"/"!" so error classes
// remain scannable when NO_COLOR strips the colour. Selected rows skip the
// per-cell colour because reverse-video already encodes the selection.
func formatRouteStatusCells(s model.RouteStat, selected bool) string {
	cells := [4]string{
		formatStatusCount(s.Status2xx, ""),
		formatStatusCount(s.Status3xx, ""),
		formatStatusCount(s.Status4xx, "*"),
		formatStatusCount(s.Status5xx, "!"),
	}
	if !selected {
		cells[0] = styleStatusCount(cells[0], s.Status2xx, okStyle)
		cells[2] = styleStatusCount(cells[2], s.Status4xx, warnStyle)
		cells[3] = styleStatusCount(cells[3], s.Status5xx, dangerStyle)
	}
	out := strings.Builder{}
	for _, cell := range cells {
		out.WriteString(padCellLeft(cell, colRouteStatus1))
	}
	return out.String()
}

// patternAccentStyle tints :uuid / :hash / :id so the dynamic part of every
// pattern jumps out at a glance — without it, `:uuid` blends into the rest
// of the path. Selected rows skip the styling because reverse-video already
// swaps fg/bg.
var patternAccentStyle = lipgloss.NewStyle().Foreground(amber)

func highlightPattern(s string, selected bool) string {
	if selected {
		return s
	}
	for _, ph := range model.Placeholders {
		if strings.Contains(s, ph) {
			s = strings.ReplaceAll(s, ph, patternAccentStyle.Render(ph))
		}
	}
	return s
}

func formatStatusCount(n int, marker string) string {
	if n == 0 || marker == "" {
		return fmt.Sprintf("%5d", n)
	}
	return fmt.Sprintf("%4d%s", n, marker)
}

func styleStatusCount(cell string, n int, style lipgloss.Style) string {
	if n == 0 {
		return cell
	}
	return style.Render(cell)
}
