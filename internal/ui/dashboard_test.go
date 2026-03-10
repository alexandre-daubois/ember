package ui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/alexandredaubois/ember/internal/fetcher"
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

func TestAppendSparkline_Caps(t *testing.T) {
	var h []float64
	for i := 0; i < 50; i++ {
		h = appendSparkline(h, float64(i))
	}
	assert.Len(t, h, sparklineSize)
	assert.Equal(t, float64(49), h[len(h)-1])
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
