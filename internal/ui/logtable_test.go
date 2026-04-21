package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
)

func TestFormatLogRow_ParseError_Truncates(t *testing.T) {
	entry := fetcher.LogEntry{
		ParseError: true,
		RawLine:    strings.Repeat("x", 200),
	}
	row := stripANSI(formatLogRow(entry, 40, 20, false))
	assert.Contains(t, row, "…", "very long parse-error raw line must be ellipsised")
	assert.LessOrEqual(t, lipgloss.Width(row), 40)
}

func TestFormatLogRow_ParseError_Selected(t *testing.T) {
	entry := fetcher.LogEntry{ParseError: true, RawLine: "boom"}
	row := stripANSI(formatLogRow(entry, 40, 20, true))
	assert.Contains(t, row, ">")
	assert.Contains(t, row, "boom")
}

func TestFormatLogRow_StatusColors(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name   string
		status int
	}{
		{"2xx", 200},
		{"4xx", 404},
		{"5xx", 503},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			entry := fetcher.LogEntry{
				Timestamp: now,
				Method:    "GET",
				Host:      "h.com",
				URI:       "/x",
				Status:    c.status,
				Duration:  0.005,
			}
			row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false))
			assert.Contains(t, row, "h.com")
			assert.Contains(t, row, "GET")
		})
	}
}

func TestFormatLogRow_LongHostTruncates(t *testing.T) {
	longHost := strings.Repeat("a", 50) + ".com"
	entry := fetcher.LogEntry{
		Timestamp: time.Now(),
		Method:    "GET",
		Host:      longHost,
		URI:       "/x",
		Status:    200,
	}
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false))
	assert.Contains(t, row, "…", "host longer than its column must be ellipsised")
	assert.NotContains(t, row, longHost)
}

func TestFormatLogRow_LongURITruncates(t *testing.T) {
	longURI := "/" + strings.Repeat("p/", 80)
	entry := fetcher.LogEntry{
		Timestamp: time.Now(),
		Method:    "GET",
		Host:      "h.com",
		URI:       longURI,
		Status:    200,
	}
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false))
	assert.Contains(t, row, "…")
	assert.NotContains(t, row, longURI)
}

func TestFormatLogRow_MissingFieldsRenderDashes(t *testing.T) {
	entry := fetcher.LogEntry{Timestamp: time.Now()}
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false))
	assert.Contains(t, row, "—")
}

func TestFormatLogRow_LongMethodTruncates(t *testing.T) {
	entry := fetcher.LogEntry{
		Timestamp: time.Now(),
		Method:    "VERYLONGMETHOD",
		Host:      "h.com",
		URI:       "/x",
		Status:    200,
	}
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false))
	assert.NotContains(t, row, "VERYLONGMETHOD")
}

func TestFormatLogRow_SelectedShowsCursor(t *testing.T) {
	entry := fetcher.LogEntry{
		Timestamp: time.Now(),
		Method:    "GET",
		Host:      "h.com",
		URI:       "/",
		Status:    200,
	}
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), true))
	assert.Contains(t, row, ">")
}

func TestRenderLogHeader_NoStatus(t *testing.T) {
	out := stripANSI(renderLogHeader("Time  Code  Method", "", 80))
	assert.Contains(t, out, "Time")
	assert.Contains(t, out, "Method")
}

func TestRenderLogHeader_WithStatus(t *testing.T) {
	out := stripANSI(renderLogHeader("Time  Code  Method", "PAUSED", 80))
	assert.Contains(t, out, "Time")
	assert.Contains(t, out, "PAUSED")
}

func TestRenderLogHeader_NarrowWidthKeepsStatus(t *testing.T) {
	labels := strings.Repeat("Time  Code  Method  Host  ", 4)
	out := stripANSI(renderLogHeader(labels, "PAUSED", 30))
	assert.Contains(t, out, "PAUSED",
		"status pill must survive even when labels overflow the available width")
}

func TestRenderLogTable_EmptyEntriesShowsHint(t *testing.T) {
	out := stripANSI(renderLogTable(nil, 0, 80, 10, "", "Waiting..."))
	assert.Contains(t, out, "Waiting...")
}

func TestRenderLogTable_ClipsToBodyHeight(t *testing.T) {
	now := time.Now()
	entries := make([]fetcher.LogEntry, 50)
	for i := range entries {
		entries[i] = fetcher.LogEntry{
			Timestamp: now,
			Method:    "GET",
			Host:      "h.com",
			URI:       "/x",
			Status:    200,
		}
	}
	out := renderLogTable(entries, 0, 120, 10, "", "")
	assert.LessOrEqual(t, lipgloss.Height(out), 10,
		"renderLogTable must respect the requested height")
}
