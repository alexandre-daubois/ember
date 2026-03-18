package ui

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

func renderHostDetailPanel(h model.HostDerived, width, height int) string {
	inner := width - 4
	if inner < 10 {
		inner = 10
	}

	var lines []string

	host := h.Host
	if host == "*" {
		host = "* (All traffic)"
	}
	if len(host) > inner {
		host = host[:inner-1] + "…"
	}
	lines = append(lines, titleStyle.Render(host))

	lines = append(lines, "")
	lines = append(lines, sectionHeader("Traffic", inner))
	lines = append(lines, detailKV("RPS", formatDetailRate(h.RPS)))
	lines = append(lines, detailKV("In-flight", fmt.Sprintf("%.0f", h.InFlight)))
	if h.TotalRequests > 0 {
		lines = append(lines, detailKV("Total", formatNumber(int64(h.TotalRequests))))
	}

	if h.ErrorRate > 0 {
		lines = append(lines, "  "+detailLabelStyle.Render("Errors")+dangerStyle.Render(formatDetailRate(h.ErrorRate)))
	}

	lines = append(lines, "")
	lines = append(lines, sectionHeader("Latency", inner))
	if h.HasPercentiles {
		lines = append(lines, detailKV("P50", formatMs(h.P50)))
		lines = append(lines, detailKV("P90", formatMs(h.P90)))
		lines = append(lines, detailKV("P95", formatMs(h.P95)))
		lines = append(lines, detailKV("P99", formatMs(h.P99)))
	}
	if h.AvgTime > 0 {
		lines = append(lines, detailKV("Avg", formatMs(h.AvgTime)))
	}
	if !h.HasPercentiles && h.AvgTime == 0 {
		lines = append(lines, greyStyle.Render("  —"))
	}

	if h.HasTTFB {
		lines = append(lines, "")
		lines = append(lines, sectionHeader("TTFB", inner))
		lines = append(lines, detailKV("P50", formatMs(h.TTFBP50)))
		lines = append(lines, detailKV("P90", formatMs(h.TTFBP90)))
		lines = append(lines, detailKV("P95", formatMs(h.TTFBP95)))
		lines = append(lines, detailKV("P99", formatMs(h.TTFBP99)))
	}

	if len(h.StatusCodes) > 0 {
		lines = append(lines, "")
		lines = append(lines, sectionHeader("Status Codes", inner))
		codes := sortedStatusCodes(h.StatusCodes)
		for _, sc := range codes {
			label := fmt.Sprintf("%d", sc.code)
			value := fmt.Sprintf("%s/s", formatRate(sc.rate))
			style := lipgloss.NewStyle()
			if sc.code >= 400 && sc.code < 500 {
				style = warnStyle
			} else if sc.code >= 500 {
				style = dangerStyle
			}
			lines = append(lines, "  "+detailLabelStyle.Render(label)+style.Render(value))
		}
	}

	if len(h.MethodRates) > 0 {
		lines = append(lines, "")
		lines = append(lines, sectionHeader("Methods", inner))
		var totalRate float64
		for _, r := range h.MethodRates {
			totalRate += r
		}
		methods := sortedMethods(h.MethodRates)
		for _, m := range methods {
			pct := ""
			if totalRate > 0 {
				pct = fmt.Sprintf(" (%d%%)", int(m.rate/totalRate*100+0.5))
			}
			lines = append(lines, detailKV(m.method, fmt.Sprintf("%s/s%s", formatRate(m.rate), pct)))
		}
	}

	if h.AvgRequestSize > 0 || h.AvgResponseSize > 0 {
		lines = append(lines, "")
		lines = append(lines, sectionHeader("Transfer Size", inner))
		if h.AvgRequestSize > 0 {
			lines = append(lines, detailKV("Req avg", formatBytes(int64(h.AvgRequestSize))))
		}
		if h.AvgResponseSize > 0 {
			lines = append(lines, detailKV("Resp avg", formatBytes(int64(h.AvgResponseSize))))
		}
	}

	lines = append(lines, "")
	lines = append(lines, helpStyle.Render("  "+helpKeyStyle.Render("Esc")+" close"))

	content := strings.Join(lines, "\n")

	contentHeight := lipgloss.Height(content)
	boxChrome := 2
	available := height - boxChrome
	if contentHeight < available {
		content += strings.Repeat("\n", available-contentHeight)
	}

	return boxStyle.Width(width - 2).Render(content)
}

func formatDetailRate(v float64) string {
	if v == 0 {
		return "—"
	}
	return fmt.Sprintf("%s/s", formatRate(v))
}

type statusCodeEntry struct {
	code int
	rate float64
}

func sortedStatusCodes(codes map[int]float64) []statusCodeEntry {
	entries := make([]statusCodeEntry, 0, len(codes))
	for code, rate := range codes {
		entries = append(entries, statusCodeEntry{code, rate})
	}
	slices.SortFunc(entries, func(a, b statusCodeEntry) int {
		return cmp.Compare(a.code, b.code)
	})
	return entries
}

type methodEntry struct {
	method string
	rate   float64
}

func sortedMethods(methods map[string]float64) []methodEntry {
	entries := make([]methodEntry, 0, len(methods))
	for method, rate := range methods {
		entries = append(entries, methodEntry{method, rate})
	}
	slices.SortFunc(entries, func(a, b methodEntry) int {
		return cmp.Compare(b.rate, a.rate)
	})
	return entries
}
