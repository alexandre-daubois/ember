package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestRenderSparkline_TooFewValues(t *testing.T) {
	got := renderSparkline([]float64{1.0}, 10)
	assert.Equal(t, strings.Repeat(" ", 10), got)
}

func TestRenderSparkline_AllZero(t *testing.T) {
	got := renderSparkline([]float64{0, 0, 0, 0}, 10)
	assert.Contains(t, got, "▁▁▁▁")
}

func TestRenderSparkline_FixedWidth(t *testing.T) {
	short := renderSparkline([]float64{1, 2, 3}, 10)
	full := renderSparkline([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 10)

	shortRunes := utf8.RuneCountInString(stripANSI(short))
	fullRunes := utf8.RuneCountInString(stripANSI(full))

	assert.Equal(t, 10, shortRunes, "short sparkline should be 10 runes")
	assert.Equal(t, 10, fullRunes, "full sparkline should be 10 runes")
}

func TestRenderSparkline_MaxIsHighestBlock(t *testing.T) {
	got := stripANSI(renderSparkline([]float64{0, 5, 10}, 3))
	assert.Contains(t, got, "█", "max value should produce highest block")
	assert.Contains(t, got, "▁", "zero value should produce lowest block")
}

func TestAppendHistory_CapsAtSparkline(t *testing.T) {
	var h []float64
	for i := 0; i < 50; i++ {
		h = appendHistory(h, float64(i), sparklineSize)
	}
	assert.Len(t, h, sparklineSize)
	assert.Equal(t, float64(49), h[len(h)-1])
}

func TestAppendHistory_CapsAtGraphSize(t *testing.T) {
	var h []float64
	for i := 0; i < 500; i++ {
		h = appendHistory(h, float64(i), graphHistorySize)
	}
	assert.Len(t, h, graphHistorySize)
	assert.Equal(t, float64(499), h[len(h)-1])
}

func TestWorkerScript(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Worker PHP Thread - /app/worker.php", "/app/worker.php"},
		{"Worker PHP Thread - /app/api.php", "/app/api.php"},
		{"Regular PHP Thread", ""},
		{"", ""},
	}
	for _, tt := range tests {
		got := workerScript(tt.name)
		assert.Equal(t, tt.want, got, "workerScript(%q)", tt.name)
	}
}

func TestCountWorkerScripts(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Name: "Worker PHP Thread - /app/worker.php"},
		{Name: "Worker PHP Thread - /app/worker.php"},
		{Name: "Worker PHP Thread - /app/api.php"},
		{Name: "Regular PHP Thread"},
	}
	assert.Equal(t, 2, countWorkerScripts(threads))
}

func TestCountWorkerScripts_None(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Name: "Regular PHP Thread"},
		{Name: "Regular PHP Thread"},
	}
	assert.Equal(t, 0, countWorkerScripts(threads))
}

func TestCountWorkerThreads(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Name: "Worker PHP Thread - /app/worker.php"},
		{Name: "Worker PHP Thread - /app/worker.php"},
		{Name: "Worker PHP Thread - /app/api.php"},
		{Name: "Regular PHP Thread"},
	}
	assert.Equal(t, 3, countWorkerThreads(threads))
}

func TestCountWorkerThreads_None(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Name: "Regular PHP Thread"},
	}
	assert.Equal(t, 0, countWorkerThreads(threads))
}

func TestRenderThreadBar_Empty(t *testing.T) {
	assert.Equal(t, "", renderThreadBar(0, 0, 0, 20))
}

func TestRenderThreadBar_TooNarrow(t *testing.T) {
	assert.Equal(t, "", renderThreadBar(1, 1, 2, 5))
}

func TestRenderThreadBar_HasBrackets(t *testing.T) {
	bar := renderThreadBar(2, 3, 10, 20)
	plain := stripANSI(bar)
	assert.True(t, strings.HasPrefix(plain, "["), "bar should start with [")
	assert.True(t, strings.HasSuffix(plain, "]"), "bar should end with ]")
}

func TestRenderThreadBar_WidthMatchesMax(t *testing.T) {
	bar := renderThreadBar(5, 5, 10, 30)
	plain := stripANSI(bar)
	// [  +  30 bar chars  +  ] = 32
	assert.Equal(t, 32, utf8.RuneCountInString(plain))
}

func TestRenderDashboard_ShowsPercentiles(t *testing.T) {
	s := &model.State{
		Current: &fetcher.Snapshot{
			Threads: fetcher.ThreadsResponse{
				ThreadDebugStates: []fetcher.ThreadDebugState{
					{Index: 0, IsWaiting: true, Name: "Regular PHP Thread"},
				},
			},
			Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		},
		Derived: model.DerivedMetrics{
			HasPercentiles: true,
			P50:            12.3,
			P95:            89.5,
			P99:            234.1,
			TotalIdle:      1,
		},
	}
	out := stripANSI(renderDashboard(s, 100, "test", nil, nil, false, true))
	assert.Contains(t, out, "P50")
	assert.Contains(t, out, "12.3ms")
	assert.Contains(t, out, "P95")
	assert.Contains(t, out, "89.5ms")
	assert.Contains(t, out, "P99")
	assert.Contains(t, out, "234.1ms")
}

func TestRenderDashboard_HidesPercentilesWhenNoData(t *testing.T) {
	s := &model.State{
		Current: &fetcher.Snapshot{
			Threads: fetcher.ThreadsResponse{
				ThreadDebugStates: []fetcher.ThreadDebugState{
					{Index: 0, IsWaiting: true, Name: "Regular PHP Thread"},
				},
			},
			Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		},
		Derived: model.DerivedMetrics{
			HasPercentiles: false,
			TotalIdle:      1,
		},
	}
	out := stripANSI(renderDashboard(s, 100, "test", nil, nil, false, true))
	assert.NotContains(t, out, "P50")
	assert.NotContains(t, out, "P95")
	assert.NotContains(t, out, "P99")
}

func TestFormatPercentile_ColorCoding(t *testing.T) {
	low := stripANSI(formatPercentile(100.0))
	assert.Contains(t, low, "100.0ms")

	high := stripANSI(formatPercentile(1500.0))
	assert.Contains(t, high, "1500.0ms")
}

func stripANSI(s string) string {
	var out []rune
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		out = append(out, r)
	}
	return string(out)
}
