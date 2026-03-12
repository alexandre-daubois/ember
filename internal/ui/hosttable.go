package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/alexandredaubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

const (
	colHostRPS      = 8
	colHostAvg      = 10
	colHostP90      = 10
	colHostP95      = 10
	colHostP99      = 10
	colHostInFlight = 10
	colHost2xx      = 8
	colHost4xx      = 8
	colHost5xx      = 8
	colHostFixed    = 1 + colHostRPS + colHostAvg + colHostP90 + colHostP95 + colHostP99 + colHostInFlight + colHost2xx + colHost4xx + colHost5xx
)

func hostNameWidth(totalWidth int) int {
	w := totalWidth - colHostFixed
	if w < 15 {
		w = 15
	}
	return w
}

const minHostRows = 10

func renderHostTable(hosts []model.HostDerived, cursor int, width int, sortBy model.HostSortField) string {
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

	header := fmt.Sprintf(" %-*s%*s%*s%*s%*s%*s%*s%*s%*s%*s",
		hostW, colHead("Host", model.SortByHost, hostW, false),
		colHostRPS, colHead("RPS", model.SortByHostRPS, colHostRPS, true),
		colHostAvg, colHead("Avg", model.SortByHostAvg, colHostAvg, true),
		colHostP90, colHead("P90", model.SortByHostP90, colHostP90, true),
		colHostP95, colHead("P95", model.SortByHostP95, colHostP95, true),
		colHostP99, colHead("P99", model.SortByHostP99, colHostP99, true),
		colHostInFlight, colHead("In-fl", model.SortByHostInFlight, colHostInFlight, true),
		colHost2xx, colHead("2xx/s", model.SortByHost2xx, colHost2xx, true),
		colHost4xx, colHead("4xx/s", model.SortByHost4xx, colHost4xx, true),
		colHost5xx, colHead("5xx/s", model.SortByHost5xx, colHost5xx, true),
	)

	headerLine := tableHeaderStyle.Width(width).Render(header)

	var rows []string
	for i, h := range hosts {
		rows = append(rows, formatHostRow(h, width, hostW, i == cursor, i%2 == 1))
	}

	for i := len(hosts); i < minHostRows; i++ {
		emptyRow := fmt.Sprintf(" %-*s%*s%*s%*s%*s%*s%*s%*s%*s%*s",
			hostW, "", colHostRPS, "", colHostAvg, "", colHostP90, "",
			colHostP95, "", colHostP99, "", colHostInFlight, "",
			colHost2xx, "", colHost4xx, "", colHost5xx, "")
		style := lipgloss.NewStyle()
		if i%2 == 1 {
			style = zebraStyle
		}
		rows = append(rows, style.Width(width).Render(emptyRow))
	}

	content := strings.Join(rows, "\n")
	return lipgloss.JoinVertical(lipgloss.Left, headerLine, content)
}

func formatHostRow(h model.HostDerived, width, hostW int, selected, zebra bool) string {
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
	p90Str := "—"
	p95Str := "—"
	p99Str := "—"
	if h.HasPercentiles {
		p90Str = formatMs(h.P90)
		p95Str = formatMs(h.P95)
		p99Str = formatMs(h.P99)
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
	avgPart := fmt.Sprintf("%*s", colHostAvg, avgStr)
	p90Part := fmt.Sprintf("%*s", colHostP90, p90Str)
	p95Part := fmt.Sprintf("%*s", colHostP95, p95Str)
	p99Part := fmt.Sprintf("%*s", colHostP99, p99Str)
	inflPart := fmt.Sprintf("%*s", colHostInFlight, inflightStr)
	part2xx := fmt.Sprintf("%*s", colHost2xx, s2xx)
	part4xx := fmt.Sprintf("%*s", colHost4xx, s4xx)
	part5xx := fmt.Sprintf("%*s", colHost5xx, s5xx)

	if selected {
		row := hostPart + rpsPart + avgPart + p90Part + p95Part + p99Part + inflPart + part2xx + part4xx + part5xx
		return selectedRowStyle.Width(width).Render(row)
	}

	style := lipgloss.NewStyle()
	if zebra {
		style = zebraStyle
	}

	// Color the status code columns
	styled4xx := part4xx
	styled5xx := part5xx
	if sum4xx > 0 {
		styled4xx = warnStyle.Render(part4xx)
	}
	if sum5xx > 0 {
		styled5xx = dangerStyle.Render(part5xx)
	}

	row := style.Render(hostPart) +
		style.Render(rpsPart) +
		style.Render(avgPart) +
		style.Render(p90Part) +
		style.Render(p95Part) +
		style.Render(p99Part) +
		style.Render(inflPart) +
		style.Render(part2xx) +
		styled4xx +
		styled5xx

	if zebra {
		return zebraStyle.Width(width).Render(row)
	}
	return row
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

	sort.SliceStable(sorted, func(i, j int) bool {
		switch by {
		case model.SortByHostRPS:
			return sorted[i].RPS > sorted[j].RPS
		case model.SortByHostAvg:
			return sorted[i].AvgTime > sorted[j].AvgTime
		case model.SortByHostP90:
			return sorted[i].P90 > sorted[j].P90
		case model.SortByHostP95:
			return sorted[i].P95 > sorted[j].P95
		case model.SortByHostP99:
			return sorted[i].P99 > sorted[j].P99
		case model.SortByHostInFlight:
			return sorted[i].InFlight > sorted[j].InFlight
		case model.SortByHost2xx:
			return statusCodeRange(sorted[i].StatusCodes, 200, 299) > statusCodeRange(sorted[j].StatusCodes, 200, 299)
		case model.SortByHost4xx:
			return statusCodeRange(sorted[i].StatusCodes, 400, 499) > statusCodeRange(sorted[j].StatusCodes, 400, 499)
		case model.SortByHost5xx:
			return statusCodeRange(sorted[i].StatusCodes, 500, 599) > statusCodeRange(sorted[j].StatusCodes, 500, 599)
		default:
			return sorted[i].Host < sorted[j].Host
		}
	})

	return sorted
}
