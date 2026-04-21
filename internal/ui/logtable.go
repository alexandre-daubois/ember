package ui

import (
	"fmt"
	"strings"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/charmbracelet/lipgloss"
)

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
)

var logHeaderBorderStyle = lipgloss.NewStyle().
	BorderBottom(true).
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(subtle)

var logHeaderLabelsStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(subtle)

func renderLogTable(entries []fetcher.LogEntry, cursor, width, height int, rightStatus, emptyHint string) string {
	uriW := width - colLogFixed
	if uriW < 10 {
		uriW = 10
	}

	labels := fmt.Sprintf(" %-*s%-*s%-*s%-*s%*s  %-*s",
		colLogTime, "Time",
		colLogStatus, "Code",
		colLogMethod, "Method",
		colLogHost, "Host",
		colLogDur, "Duration",
		uriW-2, "URI",
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
		rows = append(rows, formatLogRow(e, width, uriW, i == cursor))
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

func formatLogRow(e fetcher.LogEntry, width, uriW int, selected bool) string {
	prefix := " "
	if selected {
		prefix = ">"
	}

	if e.ParseError {
		raw := e.RawLine
		maxLen := width - 2
		if maxLen > 0 && len(raw) > maxLen {
			raw = raw[:maxLen-1] + "…"
		}
		line := prefix + " " + raw
		if selected {
			return selectedRowStyle.Width(width).Render(line)
		}
		return greyStyle.Width(width).Render(line)
	}

	timeStr := e.Timestamp.Local().Format("15:04:05.000")
	if len(timeStr) > colLogTime-1 {
		timeStr = timeStr[:colLogTime-1]
	}

	statusStr := "—"
	if e.Status > 0 {
		statusStr = fmt.Sprintf("%d", e.Status)
	}

	method := e.Method
	if method == "" {
		method = "—"
	}
	if len(method) > colLogMethod-1 {
		method = method[:colLogMethod-1]
	}

	host := e.Host
	if host == "" {
		host = "—"
	}
	if len(host) > colLogHost-1 {
		host = host[:colLogHost-2] + "…"
	}

	durStr := "—"
	if e.Duration > 0 {
		durStr = formatMs(e.Duration * 1000)
	}

	uri := e.URI
	if uri == "" {
		uri = "—"
	}
	if len(uri) > uriW-2 {
		uri = uri[:uriW-3] + "…"
	}

	timePart := fmt.Sprintf("%s%-*s", prefix, colLogTime, timeStr)
	statusPart := fmt.Sprintf("%-*s", colLogStatus, statusStr)
	methodPart := fmt.Sprintf("%-*s", colLogMethod, method)
	hostPart := fmt.Sprintf("%-*s", colLogHost, host)
	durPart := fmt.Sprintf("%*s  ", colLogDur, durStr)
	uriPart := fmt.Sprintf("%-*s", uriW-2, uri)

	if selected {
		row := timePart + statusPart + methodPart + hostPart + durPart + uriPart
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

	return timePart + styledStatus + methodPart + hostPart + durPart + uriPart
}
