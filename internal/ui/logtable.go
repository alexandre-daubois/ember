package ui

import (
	"fmt"
	"strings"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// fitCellLeft truncates s to at most `cells` display columns, appends "…"
// when truncation happens, and right-pads with spaces so the returned
// string is exactly `cells` cells wide. Display-cell aware: an emoji counts
// as 2 cells even though it's several bytes, which is what the terminal
// actually renders; byte- or rune-length arithmetic would overflow the
// column and wrap the row to a second line.
func fitCellLeft(s string, cells int) string {
	if cells <= 0 {
		return ""
	}
	truncated := runewidth.Truncate(s, cells, "…")
	pad := cells - runewidth.StringWidth(truncated)
	if pad > 0 {
		truncated += strings.Repeat(" ", pad)
	}
	return truncated
}

// fitCellRight is the right-aligned variant: pads on the left with spaces.
// Used for the Duration column whose numeric value reads better hugging the
// next column boundary.
func fitCellRight(s string, cells int) string {
	if cells <= 0 {
		return ""
	}
	truncated := runewidth.Truncate(s, cells, "…")
	pad := cells - runewidth.StringWidth(truncated)
	if pad > 0 {
		truncated = strings.Repeat(" ", pad) + truncated
	}
	return truncated
}

const (
	colLogTime   = 13 // "HH:MM:SS.mmm "
	colLogStatus = 5
	colLogMethod = 8
	colLogHost   = 22
	colLogDur    = 9
	colLogFixed  = 1 + colLogTime + colLogStatus + colLogMethod + colLogHost + colLogDur

	// logHeaderHeight is the number of visual rows consumed by the header:
	// one row of column labels plus one row for the bottom border. Shared
	// by the slicer in logs.go so the cursor-visibility math matches what
	// renderLogTable actually draws.
	logHeaderHeight = 2

	// Runtime log table uses Time + Level + Logger + Message; no request-
	// specific fields (status, method, host, duration, URI) are meaningful.
	colLogLevel     = 7  // "ERROR"
	colLogLogger    = 24 // "http.log.access.srv0"
	colRuntimeFixed = 1 + colLogTime + colLogLevel + colLogLogger
)

var logHeaderBorderStyle = lipgloss.NewStyle().
	BorderBottom(true).
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(subtle)

var logHeaderLabelsStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(subtle)

func renderLogTable(entries []fetcher.LogEntry, cursor, width, height int, rightStatus, emptyHint string, hideHost bool) string {
	fixed := colLogFixed
	if hideHost {
		fixed -= colLogHost
	}
	uriW := width - fixed
	if uriW < 10 {
		uriW = 10
	}

	var labels string
	if hideHost {
		labels = fmt.Sprintf(" %-*s%-*s%-*s%*s  %-*s",
			colLogTime, "Time",
			colLogStatus, "Code",
			colLogMethod, "Method",
			colLogDur, "Duration",
			uriW-2, "URI",
		)
	} else {
		labels = fmt.Sprintf(" %-*s%-*s%-*s%-*s%*s  %-*s",
			colLogTime, "Time",
			colLogStatus, "Code",
			colLogMethod, "Method",
			colLogHost, "Host",
			colLogDur, "Duration",
			uriW-2, "URI",
		)
	}
	headerLine := renderLogHeader(labels, rightStatus, width)

	bodyHeight := height - lipgloss.Height(headerLine)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var rows []string
	if len(entries) == 0 {
		rows = append(rows, greyStyle.Render(" "+emptyHint))
	}

	visible := entries
	if len(visible) > bodyHeight {
		visible = visible[:bodyHeight]
	}

	for i, e := range visible {
		rows = append(rows, formatLogRow(e, width, uriW, i == cursor, hideHost))
	}

	for len(rows) < bodyHeight {
		rows = append(rows, "")
	}

	body := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, body)
}

// renderRuntimeLogTable draws the runtime (non-access) log view. Columns
// differ from the access table because Status/Method/Host/URI/Duration are
// meaningless for startup, reload, TLS or admin-API log lines; we show
// Time + Level + Logger + Message instead.
func renderRuntimeLogTable(entries []fetcher.LogEntry, cursor, width, height int, rightStatus, emptyHint string) string {
	msgW := width - colRuntimeFixed
	if msgW < 10 {
		msgW = 10
	}

	labels := fmt.Sprintf(" %-*s%-*s%-*s%-*s",
		colLogTime, "Time",
		colLogLevel, "Level",
		colLogLogger, "Logger",
		msgW, "Message",
	)
	headerLine := renderLogHeader(labels, rightStatus, width)

	bodyHeight := height - lipgloss.Height(headerLine)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	var rows []string
	if len(entries) == 0 {
		rows = append(rows, greyStyle.Render(" "+emptyHint))
	}

	visible := entries
	if len(visible) > bodyHeight {
		visible = visible[:bodyHeight]
	}

	for i, e := range visible {
		rows = append(rows, formatRuntimeLogRow(e, width, msgW, i == cursor))
	}

	for len(rows) < bodyHeight {
		rows = append(rows, "")
	}

	body := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, body)
}

// renderLogHeader lays out column labels on the left and an already-styled
// status pill on the right of a single header line, with a border underneath.
// Embedding the status here saves a full line compared to a separate banner
// and keeps the frozen indicator visually anchored to the table.
func renderLogHeader(labels, rightStatus string, width int) string {
	styledLabels := logHeaderLabelsStyle.Render(labels)
	if rightStatus == "" {
		return logHeaderBorderStyle.Width(width).Render(styledLabels)
	}

	labelsW := lipgloss.Width(styledLabels)
	statusW := lipgloss.Width(rightStatus)
	gap := width - labelsW - statusW - 1
	if gap < 1 {
		gap = 1
		// If labels won't fit next to the status, truncate the URI-end of
		// the labels rather than dropping the status, which carries state.
		maxLabelsW := width - statusW - 2
		if maxLabelsW < 10 {
			maxLabelsW = 10
		}
		if labelsW > maxLabelsW {
			styledLabels = lipgloss.NewStyle().MaxWidth(maxLabelsW).Render(styledLabels)
		}
	}
	combined := styledLabels + strings.Repeat(" ", gap) + rightStatus + " "
	return logHeaderBorderStyle.Width(width).Render(combined)
}

// formatRuntimeLogRow renders one runtime-log row. Level is colour-coded like
// status codes in the access table: red for ERROR, amber for WARN, default
// foreground for INFO/DEBUG. Malformed lines (ParseError) show the raw input.
func formatRuntimeLogRow(e fetcher.LogEntry, width, msgW int, selected bool) string {
	prefix := " "
	if selected {
		prefix = ">"
	}

	if e.ParseError {
		raw := fitCellLeft(e.RawLine, width-2)
		line := prefix + " " + raw
		if selected {
			return selectedRowStyle.Width(width).Render(line)
		}
		return greyStyle.Width(width).Render(line)
	}

	timeStr := e.Timestamp.Local().Format("15:04:05.000")

	level := strings.ToUpper(e.Level)
	if level == "" {
		level = "—"
	}

	logger := e.Logger
	if logger == "" {
		logger = "—"
	}

	message := e.Message
	if message == "" {
		message = "—"
	}

	// fitCellLeft handles both truncation (with "…") and padding by display
	// cells, so emojis or CJK runes in message/logger stay inside the column
	// even though they consume 2 cells per glyph.
	timePart := prefix + fitCellLeft(timeStr, colLogTime)
	levelPart := fitCellLeft(level, colLogLevel)
	loggerPart := fitCellLeft(logger, colLogLogger)
	msgPart := fitCellLeft(message, msgW)

	if selected {
		row := timePart + levelPart + loggerPart + msgPart
		return selectedRowStyle.Width(width).Render(row)
	}

	styledLevel := levelPart
	switch strings.ToUpper(e.Level) {
	case "ERROR", "FATAL", "PANIC":
		styledLevel = dangerStyle.Render(levelPart)
	case "WARN", "WARNING":
		styledLevel = warnStyle.Render(levelPart)
	}

	return timePart + styledLevel + loggerPart + msgPart
}

func formatLogRow(e fetcher.LogEntry, width, uriW int, selected, hideHost bool) string {
	prefix := " "
	if selected {
		prefix = ">"
	}

	if e.ParseError {
		raw := fitCellLeft(e.RawLine, width-2)
		line := prefix + " " + raw
		if selected {
			return selectedRowStyle.Width(width).Render(line)
		}
		return greyStyle.Width(width).Render(line)
	}

	timeStr := e.Timestamp.Local().Format("15:04:05.000")

	statusStr := "—"
	if e.Status > 0 {
		statusStr = fmt.Sprintf("%d", e.Status)
	}

	method := e.Method
	if method == "" {
		method = "—"
	}

	host := e.Host
	if host == "" {
		host = "—"
	}

	durStr := "—"
	if e.Duration > 0 {
		durStr = formatMs(e.Duration * 1000)
	}

	uri := e.URI
	if uri == "" {
		uri = "—"
	}

	// fitCell* pad and truncate by display cells so emojis / CJK runes in
	// host or URI don't overflow their column and wrap the row to a second
	// line. Duration is right-aligned with a two-space gutter to keep the
	// numeric column hugging the next cell.
	timePart := prefix + fitCellLeft(timeStr, colLogTime)
	statusPart := fitCellLeft(statusStr, colLogStatus)
	methodPart := fitCellLeft(method, colLogMethod)
	hostPart := fitCellLeft(host, colLogHost)
	durPart := fitCellRight(durStr, colLogDur) + "  "
	uriPart := fitCellLeft(uri, uriW-2)

	if selected {
		var row string
		if hideHost {
			row = timePart + statusPart + methodPart + durPart + uriPart
		} else {
			row = timePart + statusPart + methodPart + hostPart + durPart + uriPart
		}
		return selectedRowStyle.Width(width).Render(row)
	}

	styledStatus := statusPart
	switch {
	case e.Status >= 500:
		styledStatus = dangerStyle.Render(statusPart)
	case e.Status >= 400:
		styledStatus = warnStyle.Render(statusPart)
	case e.Status >= 200 && e.Status < 300:
		styledStatus = okStyle.Render(statusPart)
	}

	if hideHost {
		return timePart + styledStatus + methodPart + durPart + uriPart
	}
	return timePart + styledStatus + methodPart + hostPart + durPart + uriPart
}
