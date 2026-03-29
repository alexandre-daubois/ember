package ui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRenderGraphPanels_AllEmpty(t *testing.T) {
	out := renderGraphPanels(80, 40, nil, nil, nil, nil, nil, true)
	assert.Contains(t, out, "no data")
}

func TestRenderGraphPanels_ContainsAllPanelTitles(t *testing.T) {
	data := []float64{1, 2, 3, 4, 5}
	out := renderGraphPanels(120, 50, data, data, data, data, data, true)
	for _, title := range []string{"CPU", "RPS", "RSS", "Queue", "Busy Threads"} {
		assert.Contains(t, out, title, "should contain panel %q", title)
	}
}

func TestRenderGraphPanels_WithoutFrankenPHP(t *testing.T) {
	data := []float64{1, 2, 3, 4, 5}
	out := renderGraphPanels(120, 50, data, data, data, data, data, false)
	assert.Contains(t, out, "CPU")
	assert.Contains(t, out, "RPS")
	assert.Contains(t, out, "RSS")
	assert.NotContains(t, out, "Queue")
	assert.NotContains(t, out, "Busy Threads")
}

func TestRenderGraphPanels_NarrowTerminal(t *testing.T) {
	data := []float64{1, 2, 3}
	out := renderGraphPanels(30, 20, data, data, data, data, data, true)
	assert.NotEmpty(t, out)
	assert.Contains(t, out, "CPU")
}

func TestRenderGraphPanels_ShortTerminal(t *testing.T) {
	data := []float64{1, 2, 3}
	out := renderGraphPanels(80, 10, data, data, data, data, data, true)
	assert.NotEmpty(t, out)
	assert.Contains(t, out, "CPU")
}

func TestRenderSingleGraph_NoData(t *testing.T) {
	p := graphPanel{title: "Test", unit: "%", values: nil}
	out := renderSingleGraph(p, 40, graphPanelHeight)
	assert.Contains(t, out, "no data")
	assert.Contains(t, out, "Test")
}

func TestRenderSingleGraph_WithUnit(t *testing.T) {
	p := graphPanel{title: "CPU", unit: "%", values: []float64{10, 20, 30}}
	out := renderSingleGraph(p, 40, graphPanelHeight)
	assert.Contains(t, out, "CPU")
	assert.Contains(t, out, "30.0 %")
}

func TestRenderSingleGraph_WithoutUnit(t *testing.T) {
	p := graphPanel{title: "Queue", unit: "", values: []float64{0, 1, 2}}
	out := renderSingleGraph(p, 40, graphPanelHeight)
	assert.Contains(t, out, "Queue")
	assert.Contains(t, out, "2")
}

func TestRenderSingleGraph_AllZeros(t *testing.T) {
	p := graphPanel{title: "Queue", unit: "", values: []float64{0, 0, 0, 0, 0}}
	out := renderSingleGraph(p, 60, graphPanelHeight)
	lines := strings.Split(out, "\n")
	// header line + margin + graphPanelHeight chart lines
	assert.GreaterOrEqual(t, len(lines), graphPanelHeight, "graph should use full height even with all zeros")
}

func TestRenderSingleGraph_TruncatesDataToWidth(t *testing.T) {
	values := make([]float64, 500)
	for i := range values {
		values[i] = float64(i)
	}
	p := graphPanel{title: "Big", unit: "", values: values}
	out := renderSingleGraph(p, 40, graphPanelHeight)
	assert.Contains(t, out, "Big")
	assert.NotEmpty(t, out)
}

func TestRenderSingleGraph_NarrowWidth(t *testing.T) {
	p := graphPanel{title: "CPU", unit: "%", values: []float64{1, 2, 3}}
	out := renderSingleGraph(p, 15, graphPanelHeight)
	assert.Contains(t, out, "CPU")
	assert.NotEmpty(t, out)
}

func TestRenderSingleGraph_SingleValue(t *testing.T) {
	p := graphPanel{title: "RPS", unit: "req/s", values: []float64{42}}
	out := renderSingleGraph(p, 40, graphPanelHeight)
	assert.Contains(t, out, "42.0 req/s")
}
