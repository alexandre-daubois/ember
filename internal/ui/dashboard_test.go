package ui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
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
	assert.InDelta(t, float64(49), h[len(h)-1], 0.001)
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
	assert.Empty(t, renderThreadBar(0, 0, 0, 20))
}

func TestRenderThreadBar_TooNarrow(t *testing.T) {
	assert.Empty(t, renderThreadBar(1, 1, 2, 5))
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

func TestRenderDashboard_ShowsPausedBadge(t *testing.T) {
	s := &model.State{
		Current: &fetcher.Snapshot{},
	}
	out := stripANSI(renderDashboard(s, 120, "v0.1", nil, nil, false, true, false))
	assert.Contains(t, out, "PAUSED")
}

func TestRenderDashboard_NoPausedWhenRunning(t *testing.T) {
	s := &model.State{
		Current: &fetcher.Snapshot{},
	}
	out := stripANSI(renderDashboard(s, 120, "v0.1", nil, nil, false, false, false))
	assert.NotContains(t, out, "PAUSED")
}

func TestRenderDashboard_ShowsErrorRate(t *testing.T) {
	s := &model.State{
		Current: &fetcher.Snapshot{},
		Derived: model.DerivedMetrics{ErrorRate: 12},
	}
	out := stripANSI(renderDashboard(s, 120, "v0.1", nil, nil, false, false, false))
	assert.Contains(t, out, "Err/s")
	assert.Contains(t, out, "12")
}

func TestRenderDashboard_HidesErrorRateWhenZero(t *testing.T) {
	s := &model.State{
		Current: &fetcher.Snapshot{},
		Derived: model.DerivedMetrics{ErrorRate: 0},
	}
	out := stripANSI(renderDashboard(s, 120, "v0.1", nil, nil, false, false, false))
	assert.NotContains(t, out, "Err/s")
}

func TestRenderConnectionError_ContainsErrorMessage(t *testing.T) {
	out := renderConnectionError("connection refused", 80, 24)
	plain := stripANSI(out)

	assert.Contains(t, plain, "Connection failed")
	assert.Contains(t, plain, "Cannot reach the Caddy admin API")
	assert.Contains(t, plain, "admin localhost:2019")
	assert.Contains(t, plain, "ember --addr")
	assert.Contains(t, plain, "Retrying automatically")
}

func TestRenderConnectionError_NarrowTerminal(t *testing.T) {
	out := renderConnectionError("timeout", 60, 20)
	assert.NotEmpty(t, out)
	assert.Contains(t, stripANSI(out), "Connection failed")
}

func TestFormatMs_Milliseconds(t *testing.T) {
	assert.Equal(t, "123.4ms", formatMs(123.4))
	assert.Equal(t, "0.0ms", formatMs(0))
	assert.Equal(t, "9999.9ms", formatMs(9999.9))
}

func TestFormatMs_Seconds(t *testing.T) {
	assert.Equal(t, "10.0s", formatMs(10000))
	assert.Equal(t, "45.5s", formatMs(45500))
}

func TestFormatMs_Negative(t *testing.T) {
	assert.Equal(t, "-5.0ms", formatMs(-5))
}

func TestRenderDashboard_ShowsReloadOK(t *testing.T) {
	s := &model.State{
		Current: &fetcher.Snapshot{
			Metrics: fetcher.MetricsSnapshot{
				HasConfigReloadMetrics:           true,
				ConfigLastReloadSuccessful:       1,
				ConfigLastReloadSuccessTimestamp: float64(time.Now().Add(-5 * time.Minute).Unix()),
			},
		},
	}
	out := stripANSI(renderDashboard(s, 120, "v0.1", nil, nil, false, false, false))
	assert.Contains(t, out, "config reload")
	assert.Contains(t, out, "ago")
	assert.NotContains(t, out, "FAILED")
}

func TestRenderDashboard_ShowsReloadFailed(t *testing.T) {
	s := &model.State{
		Current: &fetcher.Snapshot{
			Metrics: fetcher.MetricsSnapshot{
				HasConfigReloadMetrics:           true,
				ConfigLastReloadSuccessful:       0,
				ConfigLastReloadSuccessTimestamp: float64(time.Now().Add(-10 * time.Minute).Unix()),
			},
		},
	}
	out := stripANSI(renderDashboard(s, 120, "v0.1", nil, nil, false, false, false))
	assert.Contains(t, out, "config reload FAILED")
	assert.NotContains(t, out, "ago")
}

func TestRenderDashboard_NoReloadWhenNoData(t *testing.T) {
	s := &model.State{
		Current: &fetcher.Snapshot{},
	}
	out := stripANSI(renderDashboard(s, 120, "v0.1", nil, nil, false, false, false))
	assert.NotContains(t, out, "config reload")
}

func TestFormatReloadAge(t *testing.T) {
	assert.Equal(t, "0s", formatReloadAge(-5*time.Second))
	assert.Equal(t, "30s", formatReloadAge(30*time.Second))
	assert.Equal(t, "5m", formatReloadAge(5*time.Minute))
	assert.Equal(t, "2h 30m", formatReloadAge(2*time.Hour+30*time.Minute))
	assert.Equal(t, "3d 2h", formatReloadAge(74*time.Hour))
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
