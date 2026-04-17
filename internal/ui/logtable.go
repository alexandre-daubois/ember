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
)

func renderLogTable(entries []fetcher.LogEntry, cursor, width, height int, filterActive bool, filterLabel, emptyHint string) string {
	uriW := width - colLogFixed
	if uriW < 10 {
		uriW = 10
	}

	header := fmt.Sprintf(" %-*s%-*s%-*s%-*s%*s  %-*s",
		colLogTime, "Time",
		colLogStatus, "Code",
		colLogMethod, "Method",
		colLogHost, "Host",
		colLogDur, "Duration",
		uriW-2, "URI",
	)
	headerLine := tableHeaderStyle.Width(width).Render(header)

	var bannerLine string
	if filterActive && filterLabel != "" {
		bannerLine = helpStyle.Width(width).Render(" " + filterLabel)
	}

	bodyHeight := height - lipgloss.Height(headerLine)
	if bannerLine != "" {
		bodyHeight -= lipgloss.Height(bannerLine)
	}
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
		rows = append(rows, formatLogRow(e, width, uriW, i == cursor, i%2 == 1))
	}

	for len(rows) < bodyHeight {
		rows = append(rows, "")
	}

	body := strings.Join(rows, "\n")
	joined := lipgloss.JoinVertical(lipgloss.Left, headerLine, body)

	if bannerLine != "" {
		return lipgloss.JoinVertical(lipgloss.Left, bannerLine, joined)
	}
	return joined
}

func formatLogRow(e fetcher.LogEntry, width, uriW int, selected, zebra bool) string {
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

	base := lipgloss.NewStyle()
	if zebra {
		base = zebraStyle
	}

	styledStatus := base.Render(statusPart)
	switch {
	case e.Status >= 500:
		styledStatus = dangerStyle.Render(statusPart)
	case e.Status >= 400:
		styledStatus = warnStyle.Render(statusPart)
	case e.Status >= 200 && e.Status < 300:
		styledStatus = okStyle.Render(statusPart)
	}

	row := base.Render(timePart) +
		styledStatus +
		base.Render(methodPart) +
		base.Render(hostPart) +
		base.Render(durPart) +
		base.Render(uriPart)

	if zebra {
		return zebraStyle.Width(width).Render(row)
	}
	return row
}
