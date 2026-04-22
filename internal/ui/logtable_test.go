package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatLogRow_ParseError_Truncates(t *testing.T) {
	entry := fetcher.LogEntry{
		ParseError: true,
		RawLine:    strings.Repeat("x", 200),
	}
	row := stripANSI(formatLogRow(entry, 40, 20, false, false))
	assert.Contains(t, row, "…", "very long parse-error raw line must be ellipsised")
	assert.LessOrEqual(t, lipgloss.Width(row), 40)
}

func TestFormatLogRow_ParseError_Selected(t *testing.T) {
	entry := fetcher.LogEntry{ParseError: true, RawLine: "boom"}
	row := stripANSI(formatLogRow(entry, 40, 20, true, false))
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
			row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false, false))
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
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false, false))
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
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false, false))
	assert.Contains(t, row, "…")
	assert.NotContains(t, row, longURI)
}

func TestFormatLogRow_MissingFieldsRenderDashes(t *testing.T) {
	entry := fetcher.LogEntry{Timestamp: time.Now()}
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false, false))
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
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), false, false))
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
	row := stripANSI(formatLogRow(entry, 120, uriWidth(120), true, false))
	assert.Contains(t, row, ">")
}

func TestFormatLogRow_HideHostDropsHostColumn(t *testing.T) {
	entry := fetcher.LogEntry{
		Timestamp: time.Now(),
		Method:    "GET",
		Host:      "visible.example",
		URI:       "/path",
		Status:    200,
	}
	hidden := stripANSI(formatLogRow(entry, 120, uriWidth(120)+colLogHost, false, true))
	visible := stripANSI(formatLogRow(entry, 120, uriWidth(120), false, false))
	assert.NotContains(t, hidden, "visible.example", "host column must disappear when hideHost=true")
	assert.Contains(t, visible, "visible.example")
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
	out := stripANSI(renderLogTable(nil, 0, 80, 10, "", "Waiting...", false))
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
	out := renderLogTable(entries, 0, 120, 10, "", "", false)
	assert.LessOrEqual(t, lipgloss.Height(out), 10,
		"renderLogTable must respect the requested height")
}

func TestRenderRuntimeLogTable_ShowsLevelAndLogger(t *testing.T) {
	entries := []fetcher.LogEntry{
		{
			Timestamp: time.Now(),
			Level:     "error",
			Logger:    "tls.handshake",
			Message:   "certificate expired",
		},
	}
	out := stripANSI(renderRuntimeLogTable(entries, 0, 120, 10, "", ""))
	assert.Contains(t, out, "ERROR")
	assert.Contains(t, out, "tls.handshake")
	assert.Contains(t, out, "certificate expired")
}

func TestRenderRuntimeLogTable_EmptyShowsHint(t *testing.T) {
	out := stripANSI(renderRuntimeLogTable(nil, 0, 120, 10, "", "No runtime logs"))
	assert.Contains(t, out, "No runtime logs")
}

func TestFormatRuntimeLogRow_EmojiFitsColumnWidth(t *testing.T) {
	// Regression: an emoji in the message used to overflow its column
	// because truncation and padding were byte-based while the terminal
	// renders wide glyphs (emoji, CJK) as 2 cells. The row would wrap to
	// two lines when not selected and collapse back to one when focused.
	// lipgloss.Width is grapheme-aware so it matches what the terminal
	// actually renders; an exceeding display width means the terminal
	// wraps on render.
	entry := fetcher.LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		Logger:    "admin.api",
		Message:   "server started 🚀 ready to serve requests",
	}
	width := 120
	msgW := width - colRuntimeFixed

	for _, name := range []string{"plain", "selected"} {
		t.Run(name, func(t *testing.T) {
			var row string
			switch name {
			case "plain":
				row = formatRuntimeLogRow(entry, width, msgW, false)
			case "selected":
				row = formatRuntimeLogRow(entry, width, msgW, true)
			}
			assert.LessOrEqual(t, lipgloss.Width(row), width,
				"rendered display width must not exceed the terminal width, or the row wraps")
		})
	}
}

func TestFormatRuntimeLogRow_SeverityPrefix(t *testing.T) {
	// The prefix character doubles as a severity cue so ERROR/WARN rows
	// remain scannable when colours are off (NO_COLOR). Selection overrides
	// the severity prefix because the cursor marker is more important.
	cases := []struct {
		level    string
		selected bool
		want     string
	}{
		{"ERROR", false, "!"},
		{"FATAL", false, "!"},
		{"PANIC", false, "!"},
		{"WARN", false, "*"},
		{"WARNING", false, "*"},
		{"INFO", false, " "},
		{"DEBUG", false, " "},
		{"", false, " "},
		{"ERROR", true, ">"},
		{"WARN", true, ">"},
		{"INFO", true, ">"},
	}
	width := 120
	msgW := width - colRuntimeFixed
	for _, tc := range cases {
		t.Run(tc.level+"_sel="+strconv.FormatBool(tc.selected), func(t *testing.T) {
			entry := fetcher.LogEntry{
				Timestamp: time.Now(),
				Level:     tc.level,
				Logger:    "caddy",
				Message:   "hello",
			}
			row := stripANSI(formatRuntimeLogRow(entry, width, msgW, tc.selected))
			require.NotEmpty(t, row)
			assert.Equal(t, tc.want, string(row[0]),
				"first char of the row must be the severity/selection prefix")
		})
	}
}

func TestFormatLogRow_EmojiInURIFitsColumnWidth(t *testing.T) {
	entry := fetcher.LogEntry{
		Timestamp: time.Now(),
		Method:    "GET",
		Host:      "h.com",
		URI:       "/search/🔥/results",
		Status:    200,
	}
	width := 120
	row := formatLogRow(entry, width, uriWidth(width), false, false)
	assert.LessOrEqual(t, lipgloss.Width(row), width,
		"a URI containing an emoji must not push the row past the requested width")
}

func TestFitCellLeft_EmojiCountsAsTwoCells(t *testing.T) {
	// "🚀" is 4 bytes but 2 display cells. Target width 5 must produce a
	// string of exactly 5 cells (emoji + 3 trailing spaces, not 4).
	got := fitCellLeft("🚀", 5)
	assert.Equal(t, 5, lipgloss.Width(got))
}
