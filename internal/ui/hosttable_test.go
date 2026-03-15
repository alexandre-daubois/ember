package ui

import (
	"strings"
	"testing"

	"github.com/alexandredaubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestFormatRate(t *testing.T) {
	tests := []struct {
		v    float64
		want string
	}{
		{0, "—"},
		{0.5, "0.5"},
		{3.7, "3.7"},
		{10, "10"},
		{99.9, "100"},
		{999, "999"},
		{1000, "1.0k"},
		{1500, "1.5k"},
		{12345, "12.3k"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatRate(tt.v), "formatRate(%v)", tt.v)
	}
}

func TestStatusCodeRange(t *testing.T) {
	codes := map[int]float64{
		200: 10.5,
		201: 2.0,
		301: 5.0,
		404: 3.0,
		500: 1.0,
	}

	assert.Equal(t, 12.5, statusCodeRange(codes, 200, 299))
	assert.Equal(t, 5.0, statusCodeRange(codes, 300, 399))
	assert.Equal(t, 3.0, statusCodeRange(codes, 400, 499))
	assert.Equal(t, 1.0, statusCodeRange(codes, 500, 599))
	assert.Equal(t, 0.0, statusCodeRange(codes, 600, 699))
	assert.Equal(t, 0.0, statusCodeRange(nil, 200, 299))
}

func TestSortHosts(t *testing.T) {
	hosts := []model.HostDerived{
		{Host: "b.com", RPS: 10, AvgTime: 50, P90: 80, P95: 100, P99: 200, InFlight: 2,
			StatusCodes: map[int]float64{200: 8, 404: 1, 500: 1}},
		{Host: "a.com", RPS: 50, AvgTime: 20, P90: 30, P95: 40, P99: 60, InFlight: 5,
			StatusCodes: map[int]float64{200: 45, 404: 3, 500: 2}},
		{Host: "c.com", RPS: 30, AvgTime: 100, P90: 150, P95: 200, P99: 500, InFlight: 1,
			StatusCodes: map[int]float64{200: 25, 500: 5}},
	}

	tests := []struct {
		field model.HostSortField
		first string
	}{
		{model.SortByHost, "a.com"},
		{model.SortByHostRPS, "a.com"},
		{model.SortByHostAvg, "c.com"},
		{model.SortByHostP90, "c.com"},
		{model.SortByHostP95, "c.com"},
		{model.SortByHostP99, "c.com"},
		{model.SortByHostInFlight, "a.com"},
		{model.SortByHost2xx, "a.com"},
		{model.SortByHost4xx, "a.com"},
		{model.SortByHost5xx, "c.com"},
	}

	for _, tt := range tests {
		sorted := sortHosts(hosts, tt.field)
		assert.Equal(t, tt.first, sorted[0].Host, "sortHosts by %v", tt.field)
		assert.Equal(t, 3, len(sorted))
		// original slice must not be modified
		assert.Equal(t, "b.com", hosts[0].Host)
	}
}

func TestSortHosts_Empty(t *testing.T) {
	sorted := sortHosts(nil, model.SortByHostRPS)
	assert.Empty(t, sorted)
}

func TestRenderHostTable_EmptyHosts(t *testing.T) {
	out := renderHostTable(nil, 0, 120, model.SortByHost, nil)
	assert.Contains(t, out, "Host")
	assert.Contains(t, out, "RPS")
	lines := strings.Split(out, "\n")
	// header + minHostRows padding
	assert.GreaterOrEqual(t, len(lines), minHostRows)
}

func TestRenderHostTable_PaddingRows(t *testing.T) {
	hosts := []model.HostDerived{
		{Host: "example.com", StatusCodes: map[int]float64{}},
	}
	out := renderHostTable(hosts, 0, 120, model.SortByHost, nil)
	lines := strings.Split(out, "\n")
	// 1 header + at least minHostRows
	assert.GreaterOrEqual(t, len(lines), 1+minHostRows)
}

func TestRenderHostTable_SortIndicator(t *testing.T) {
	hosts := []model.HostDerived{
		{Host: "a.com", StatusCodes: map[int]float64{}},
	}
	out := renderHostTable(hosts, 0, 120, model.SortByHostRPS, nil)
	assert.Contains(t, out, "RPS ▼")
}

func TestFormatHostRow_StarRenamed(t *testing.T) {
	h := model.HostDerived{Host: "*", StatusCodes: map[int]float64{}}
	row := formatHostRow(h, 120, 30, false, false, nil)
	assert.Contains(t, row, "* (All traffic)")
}

func TestFormatHostRow_HostTruncation(t *testing.T) {
	h := model.HostDerived{Host: "very-long-hostname-that-exceeds-width.example.com", StatusCodes: map[int]float64{}}
	row := formatHostRow(h, 120, 15, false, false, nil)
	assert.Contains(t, row, "…")
}

func TestFormatHostRow_PercentilesShown(t *testing.T) {
	h := model.HostDerived{
		Host:           "test.com",
		HasPercentiles: true,
		P90:            45.0,
		P95:            120.0,
		P99:            350.0,
		StatusCodes:    map[int]float64{},
	}
	row := formatHostRow(h, 150, 30, false, false, nil)
	assert.Contains(t, row, "45.0ms")
	assert.Contains(t, row, "120.0ms")
	assert.Contains(t, row, "350.0ms")
}

func TestFormatHostRow_NoPercentilesDash(t *testing.T) {
	h := model.HostDerived{
		Host:           "test.com",
		HasPercentiles: false,
		StatusCodes:    map[int]float64{},
	}
	row := formatHostRow(h, 120, 30, false, false, nil)
	assert.Contains(t, row, "—")
}

func TestFormatHostRow_SelectedPrefix(t *testing.T) {
	h := model.HostDerived{Host: "test.com", StatusCodes: map[int]float64{}}
	row := formatHostRow(h, 120, 30, true, false, nil)
	assert.Contains(t, row, ">")
}

func TestRenderHostTable_WithSparklines(t *testing.T) {
	hosts := []model.HostDerived{
		{Host: "spark.test", RPS: 100, StatusCodes: map[int]float64{200: 100}},
	}
	history := map[string][]float64{
		"spark.test": {10, 20, 50, 80, 100, 60, 30, 90},
	}
	out := renderHostTable(hosts, 0, 140, model.SortByHost, history)
	hasSparkChar := false
	for _, r := range out {
		for _, b := range sparkBlocks {
			if r == b {
				hasSparkChar = true
				break
			}
		}
	}
	assert.True(t, hasSparkChar, "expected sparkline characters in output")
}
