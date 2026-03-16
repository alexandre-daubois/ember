package ui

import (
	"strings"
	"testing"
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
