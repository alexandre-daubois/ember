package ui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRenderSparkline_TooFewValues(t *testing.T) {
	got := renderSparkline([]float64{1.0}, 10)
	if got != strings.Repeat(" ", 10) {
		t.Errorf("expected 10 spaces, got %q", got)
	}
}

func TestRenderSparkline_AllZero(t *testing.T) {
	got := renderSparkline([]float64{0, 0, 0, 0}, 10)
	if !strings.Contains(got, "▁▁▁▁") {
		t.Errorf("all-zero sparkline should use lowest block, got %q", got)
	}
}

func TestRenderSparkline_FixedWidth(t *testing.T) {
	short := renderSparkline([]float64{1, 2, 3}, 10)
	full := renderSparkline([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 10)

	shortRunes := utf8.RuneCountInString(stripANSI(short))
	fullRunes := utf8.RuneCountInString(stripANSI(full))

	if shortRunes != 10 {
		t.Errorf("short sparkline should be 10 runes, got %d", shortRunes)
	}
	if fullRunes != 10 {
		t.Errorf("full sparkline should be 10 runes, got %d", fullRunes)
	}
}

func TestRenderSparkline_MaxIsHighestBlock(t *testing.T) {
	got := stripANSI(renderSparkline([]float64{0, 5, 10}, 3))
	if !strings.Contains(got, "█") {
		t.Errorf("max value should produce highest block, got %q", got)
	}
	if !strings.Contains(got, "▁") {
		t.Errorf("zero value should produce lowest block, got %q", got)
	}
}

func TestAppendSparkline_Caps(t *testing.T) {
	var h []float64
	for i := 0; i < 50; i++ {
		h = appendSparkline(h, float64(i))
	}
	if len(h) != sparklineSize {
		t.Errorf("expected len %d, got %d", sparklineSize, len(h))
	}
	if h[len(h)-1] != 49 {
		t.Errorf("expected last value 49, got %.0f", h[len(h)-1])
	}
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
