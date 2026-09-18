package ui

import (
	"fmt"
	"math"
	"slices"

	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
	"github.com/muesli/termenv"
)

type graphPanel struct {
	title  string
	unit   string
	values []float64
	color  asciigraph.AnsiColor
}

const graphPanelHeight = 8

// graphPanelChrome is the number of lines each rendered panel adds on top of
// the chart itself: a leading blank line, the title line and asciigraph's
// bottom axis line.
const graphPanelChrome = 3

// graphPanelMinHeight keeps a chart readable even on very short terminals.
const graphPanelMinHeight = 3

type graphSeries struct {
	cpu, rps, rss, queue, busy []float64
	subscribers, updates       []float64
	hasFrankenPHP, hasMercure  bool
	mercureOnly                bool
}

func renderGraphPanels(width, height int, s graphSeries) string {
	panels := []graphPanel{
		{title: "CPU", unit: "%", values: s.cpu, color: asciigraph.Red},
		{title: "RPS", unit: "req/s", values: s.rps, color: asciigraph.Yellow},
		{title: "RSS", unit: "MB", values: s.rss, color: asciigraph.DarkGoldenrod},
	}
	if s.mercureOnly {
		panels = slices.Delete(panels, 1, 2)
	}
	if s.hasFrankenPHP {
		panels = append(panels,
			graphPanel{title: "Queue", unit: "", values: s.queue, color: asciigraph.Red},
			graphPanel{title: "Busy Threads", unit: "", values: s.busy, color: asciigraph.Coral},
		)
	}
	if s.hasMercure {
		panels = append(panels,
			graphPanel{title: "Subscribers", unit: "", values: s.subscribers, color: asciigraph.Blue},
			graphPanel{title: "Updates", unit: "upd/s", values: s.updates, color: asciigraph.Green},
		)
	}

	colWidth := width / 2
	panelHeight := panelHeightFor(height, len(panels))

	var rows []string
	for i := 0; i < len(panels); i += 2 {
		if i+1 < len(panels) {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top,
				renderSingleGraph(panels[i], colWidth, panelHeight),
				renderSingleGraph(panels[i+1], colWidth, panelHeight),
			))
		} else {
			rows = append(rows, renderSingleGraph(panels[i], colWidth, panelHeight))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// panelHeightFor divides the available height across the panel rows (two
// panels per row) so the whole view fits instead of overflowing and being
// truncated from the top by the terminal. Falls back to graphPanelHeight when
// there is ample room.
func panelHeightFor(height, panelCount int) int {
	if panelCount == 0 {
		return graphPanelHeight
	}
	rows := (panelCount + 1) / 2
	h := height/rows - graphPanelChrome
	if h > graphPanelHeight {
		h = graphPanelHeight
	}
	if h < graphPanelMinHeight {
		h = graphPanelMinHeight
	}
	return h
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

	plotData := make([]float64, len(data))
	for i, v := range data {
		plotData[i] = math.Round(v*100) / 100
	}

	maxVal := 0.0
	for _, v := range plotData {
		if v > maxVal {
			maxVal = v
		}
	}
	upperBound := maxVal
	if upperBound < 1 {
		upperBound = 1
	}

	opts := []asciigraph.Option{
		asciigraph.Height(height),
		asciigraph.Width(chartWidth),
		asciigraph.LowerBound(0),
		asciigraph.UpperBound(math.Ceil(upperBound)),
		asciigraph.YAxisValueFormatter(func(v float64) string {
			return fmt.Sprintf("%.0f", v)
		}),
	}
	// asciigraph writes raw ANSI colour codes that bypass lipgloss, so honour
	// NO_COLOR (Ascii profile) by dropping series colours entirely.
	if lipgloss.ColorProfile() != termenv.Ascii {
		opts = append(opts, asciigraph.SeriesColors(p.color))
	}
	chart := asciigraph.Plot(plotData, opts...)

	header := lipgloss.NewStyle().Bold(true).Foreground(ember).Render(label)
	content := "\n" + header + "\n" + chart
	return lipgloss.NewStyle().Width(width).MaxWidth(width).Render(content)
}
