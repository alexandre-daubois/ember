package ui

import (
	"fmt"
	"math"

	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
)

type graphPanel struct {
	title  string
	unit   string
	values []float64
	color  asciigraph.AnsiColor
}

const graphPanelHeight = 8

func renderGraphPanels(width, height int, cpu, rps, rss, queue, busy []float64, hasFrankenPHP bool) string {
	panels := []graphPanel{
		{title: "CPU", unit: "%", values: cpu, color: asciigraph.Red},
		{title: "RPS", unit: "req/s", values: rps, color: asciigraph.Yellow},
		{title: "RSS", unit: "MB", values: rss, color: asciigraph.DarkGoldenrod},
	}
	if hasFrankenPHP {
		panels = append(panels,
			graphPanel{title: "Queue", unit: "", values: queue, color: asciigraph.Red},
			graphPanel{title: "Busy Threads", unit: "", values: busy, color: asciigraph.Coral},
		)
	}

	colWidth := width / 2

	var rows []string
	for i := 0; i < len(panels); i += 2 {
		if i+1 < len(panels) {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
				renderSingleGraph(panels[i], colWidth, graphPanelHeight),
				renderSingleGraph(panels[i+1], colWidth, graphPanelHeight),
			))
		} else {
			rows = append(rows, renderSingleGraph(panels[i], colWidth, graphPanelHeight))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

func renderSingleGraph(p graphPanel, width, height int) string {
	if len(p.values) == 0 {
		return greyStyle.Render(fmt.Sprintf(" %s — no data", p.title))
	}

	last := p.values[len(p.values)-1]
	var label string
	if p.unit != "" {
		label = fmt.Sprintf(" %s  %.1f %s", p.title, last, p.unit)
	} else {
		label = fmt.Sprintf(" %s  %.0f", p.title, last)
	}

	chartWidth := width - 10
	if chartWidth < 10 {
		chartWidth = 10
	}

	data := p.values
	if len(data) > chartWidth {
		data = data[len(data)-chartWidth:]
	}

	maxVal := 0.0
	for _, v := range data {
		if v > maxVal {
			maxVal = v
		}
	}
	upperBound := maxVal
	if upperBound < 1 {
		upperBound = 1
	}

	chart := asciigraph.Plot(data,
		asciigraph.Height(height),
		asciigraph.Width(chartWidth),
		asciigraph.SeriesColors(p.color),
		asciigraph.LowerBound(0),
		asciigraph.UpperBound(math.Ceil(upperBound)),
		asciigraph.YAxisValueFormatter(func(v float64) string {
			return fmt.Sprintf("%.0f", v)
		}),
	)

	header := lipgloss.NewStyle().Bold(true).Foreground(ember).Render(label)
	content := "\n" + header + "\n" + chart
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(content)
}
