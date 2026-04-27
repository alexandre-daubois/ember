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
	// Pattern is the only flex column; everything else is fixed so the
	// status counters and latency columns line up vertically across rows.
	colRouteFixedNonPattern = 1 + colRouteCount + colRouteMethod + 4*colRouteStatus1 + colRouteAvg + colRouteMax
)

func renderRoutesTable(stats []model.RouteStat, cursor, width, height int, sortBy model.RouteSortField, showHost bool, rightStatus, emptyHint string) string {
	// Pattern absorbs whatever width is left after the fixed columns; on
	// narrow terminals it gets squeezed (and truncated with an ellipsis by
	// fitCellLeft) rather than pushing the row past `width` and forcing the
	// terminal to wrap — keeping the table strictly within its allotment.
	remaining := width - colRouteFixedNonPattern
	if remaining < 1 {
		remaining = 1
	}
	patternW := remaining
	if patternW > colRoutePatternMax {
		patternW = colRoutePatternMax
	}
	// Any leftover width pushes the right-hand columns toward the edge so a
	// wide terminal does not stretch Pattern across half the screen.
	gap := remaining - patternW

	headerLine := renderLogHeader(buildRoutesHeader(sortBy, patternW, gap), rightStatus, width)

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
		rows = append(rows, formatRouteRow(s, width, patternW, gap, showHost, i == cursor))
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
func buildRoutesHeader(sortBy model.RouteSortField, patternW, gap int) string {
	return " " +
		padCellRight(routeSortLabel("Count", sortBy, model.SortByRouteCount), colRouteCount) +
		padCellRight("Method", colRouteMethod) +
		padCellRight(routeSortLabel("Pattern", sortBy, model.SortByRoutePattern), patternW) +
		strings.Repeat(" ", gap) +
		padCellLeft(statusLabel("2xx", okStyle), colRouteStatus1) +
		padCellLeft("3xx", colRouteStatus1) +
		padCellLeft(statusLabel("4xx", warnStyle), colRouteStatus1) +
		padCellLeft(statusLabel("5xx", dangerStyle), colRouteStatus1) +
		padCellRight(" "+routeSortLabel("Avg", sortBy, model.SortByRouteAvg), colRouteAvg) +
		padCellRight(" "+routeSortLabel("Max", sortBy, model.SortByRouteMax), colRouteMax)
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

func formatRouteRow(s model.RouteStat, width, patternW, gap int, showHost, selected bool) string {
	prefix := " "
	if selected {
		prefix = ">"
	}

	method := s.Key.Method
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

	if selected {
		return selectedRowStyle.Width(width).Render(row)
	}
	return row
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
