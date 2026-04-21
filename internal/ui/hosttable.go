package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

const (
	colHostRPS       = 8
	colHostSparkline = 8
	colHostAvg       = 10
	colHostInFlight  = 10
	colHost2xx       = 8
	colHost4xx       = 8
	colHost5xx       = 8
	colHostFixed     = 1 + colHostRPS + colHostSparkline + colHostAvg + colHostInFlight + colHost2xx + colHost4xx + colHost5xx
)

func hostNameWidth(totalWidth int) int {
	w := totalWidth - colHostFixed
	if w < 15 {
		w = 15
	}
	return w
}

func renderHostTable(hosts []model.HostDerived, cursor, width, height int, sortBy model.HostSortField, hostRPS map[string][]float64) string {
	hostW := hostNameWidth(width)

	colHead := func(label string, field model.HostSortField, w int, right bool) string {
		if sortBy == field {
			label += " ▼"
		}
		if right {
			return fmt.Sprintf("%*s", w, label)
		}
		return fmt.Sprintf("%-*s", w, label)
	}

	header := fmt.Sprintf(" %-*s%*s%*s%*s%*s%*s%*s%*s",
		hostW, colHead("Host", model.SortByHost, hostW, false),
		colHostRPS, colHead("RPS", model.SortByHostRPS, colHostRPS, true),
		colHostSparkline, "",
		colHostAvg, colHead("Avg", model.SortByHostAvg, colHostAvg, true),
		colHostInFlight, colHead("In-fl", model.SortByHostInFlight, colHostInFlight, true),
		colHost2xx, colHead("2xx/s", model.SortByHost2xx, colHost2xx, true),
		colHost4xx, colHead("4xx/s", model.SortByHost4xx, colHost4xx, true),
		colHost5xx, colHead("5xx/s", model.SortByHost5xx, colHost5xx, true),
	)

	headerLine := tableHeaderStyle.Width(width).Render(header)

	var rows []string
	for i, h := range hosts {
		rows = append(rows, formatHostRow(h, width, hostW, i == cursor, hostRPS))
	}

	bodyHeight := height - lipgloss.Height(headerLine)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	start := 0
	if cursor > bodyHeight-1 {
		start = cursor - bodyHeight + 1
	}
	end := start + bodyHeight
	if end > len(rows) {
		end = len(rows)
		start = end - bodyHeight
		if start < 0 {
			start = 0
		}
	}

	content := strings.Join(rows[start:end], "\n")
	if h := lipgloss.Height(content); h < bodyHeight {
		content += strings.Repeat("\n", bodyHeight-h)
	}
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, content)
}

func formatHostRow(h model.HostDerived, width, hostW int, selected bool, hostRPS map[string][]float64) string {
	host := h.Host
	if host == "*" {
		host = "* (All traffic)"
	}
	if len(host) > hostW-1 {
		host = host[:hostW-2] + "…"
	}

	rpsStr := "—"
	if h.RPS > 0 {
		rpsStr = fmt.Sprintf("%.0f", h.RPS)
	}
	avgStr := "—"
	if h.AvgTime > 0 {
		avgStr = formatMs(h.AvgTime)
	}
	inflightStr := fmt.Sprintf("%.0f", h.InFlight)

	sum2xx := statusCodeRange(h.StatusCodes, 200, 299)
	sum4xx := statusCodeRange(h.StatusCodes, 400, 499)
	sum5xx := statusCodeRange(h.StatusCodes, 500, 599)

	s2xx := formatRate(sum2xx)
	s4xx := formatRate(sum4xx)
	s5xx := formatRate(sum5xx)

	prefix := " "
	if selected {
		prefix = ">"
	}

	hostPart := fmt.Sprintf("%s%-*s", prefix, hostW, host)
	rpsPart := fmt.Sprintf("%*s", colHostRPS, rpsStr)
	sparkRaw := renderSparklineRaw(hostRPS[h.Host], colHostSparkline)
	avgPart := fmt.Sprintf("%*s", colHostAvg, avgStr)
	inflPart := fmt.Sprintf("%*s", colHostInFlight, inflightStr)
	part2xx := fmt.Sprintf("%*s", colHost2xx, s2xx)
	part4xx := fmt.Sprintf("%*s", colHost4xx, s4xx)
	part5xx := fmt.Sprintf("%*s", colHost5xx, s5xx)

	if selected {
		row := hostPart + rpsPart + sparkRaw + avgPart + inflPart + part2xx + part4xx + part5xx
		return selectedRowStyle.Width(width).Render(row)
	}

	styled4xx := part4xx
	styled5xx := part5xx
	if sum4xx > 0 {
		styled4xx = warnStyle.Render(part4xx)
	}
	if sum5xx > 0 {
		styled5xx = dangerStyle.Render(part5xx)
	}

	return hostPart + rpsPart + greyStyle.Render(sparkRaw) + avgPart + inflPart + part2xx + styled4xx + styled5xx
}

func statusCodeRange(codes map[int]float64, lo, hi int) float64 {
	var total float64
	for code, count := range codes {
		if code >= lo && code <= hi {
			total += count
		}
	}
	return total
}

func formatRate(v float64) string {
	if v == 0 {
		return "—"
	}
	if v >= 1000 {
		return fmt.Sprintf("%.1fk", v/1000)
	}
	if v >= 10 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}

func sortHosts(hosts []model.HostDerived, by model.HostSortField) []model.HostDerived {
	sorted := make([]model.HostDerived, len(hosts))
	copy(sorted, hosts)

	slices.SortStableFunc(sorted, func(a, b model.HostDerived) int {
		switch by {
		case model.SortByHostRPS:
			return cmp.Compare(b.RPS, a.RPS)
		case model.SortByHostAvg:
			return cmp.Compare(b.AvgTime, a.AvgTime)
		case model.SortByHostInFlight:
			return cmp.Compare(b.InFlight, a.InFlight)
		case model.SortByHost2xx:
			return cmp.Compare(statusCodeRange(b.StatusCodes, 200, 299), statusCodeRange(a.StatusCodes, 200, 299))
		case model.SortByHost4xx:
			return cmp.Compare(statusCodeRange(b.StatusCodes, 400, 499), statusCodeRange(a.StatusCodes, 400, 499))
		case model.SortByHost5xx:
			return cmp.Compare(statusCodeRange(b.StatusCodes, 500, 599), statusCodeRange(a.StatusCodes, 500, 599))
		default:
			return cmp.Compare(a.Host, b.Host)
		}
	})

	return sorted
}
