package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderRoutesTable_HeaderColumnOrder(t *testing.T) {
	out := stripANSI(renderRoutesTable(nil, 0, 160, 4, model.SortByRouteCount, false, "", ""))
	header := strings.SplitN(out, "\n", 2)[0]
	expected := []string{"Count", "Method", "Pattern", "2xx", "3xx", "4xx", "5xx", "Avg", "Max"}
	prev := -1
	for _, label := range expected {
		idx := strings.Index(header, label)
		require.GreaterOrEqual(t, idx, 0, "header missing %q in %q", label, header)
		assert.Greater(t, idx, prev, "%q should appear after the previous label", label)
		prev = idx
	}
}

func TestRenderRoutesTable_HappyPath(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	stats := []model.RouteStat{
		{
			Key:           model.RouteKey{Method: "GET", Pattern: "/users/:id"},
			Count:         42,
			Status2xx:     40,
			Status5xx:     2,
			DurationSumMs: 420,
			DurationMaxMs: 80,
			LastSeen:      now.Add(-2 * time.Second),
		},
	}
	out := stripANSI(renderRoutesTable(stats, 0, 120, 5, model.SortByRouteCount, false, "", "no rows"))
	assert.Contains(t, out, "Method")
	assert.Contains(t, out, "Pattern")
	assert.Contains(t, out, "/users/:id")
	assert.Contains(t, out, "GET")
	assert.Contains(t, out, "42")
	assert.Contains(t, out, "10.0ms") // avg
	assert.Contains(t, out, "80.0ms") // max
}

func TestRenderRoutesTable_HeaderColumnOrderTightWindow(t *testing.T) {
	// At 80 columns Pattern should still fit (capped at colRoutePatternMax)
	// while every other column is preserved end-to-end.
	out := stripANSI(renderRoutesTable(nil, 0, 80, 4, model.SortByRouteCount, false, "", ""))
	header := strings.SplitN(out, "\n", 2)[0]
	for _, label := range []string{"Count", "Method", "Pattern", "2xx", "3xx", "4xx", "5xx", "Avg", "Max"} {
		assert.Contains(t, header, label)
	}
}

func TestRenderRoutesTable_EmptyHint(t *testing.T) {
	out := stripANSI(renderRoutesTable(nil, 0, 80, 5, model.SortByRouteCount, false, "", "waiting…"))
	assert.Contains(t, out, "waiting…")
}

func TestRenderRoutesTable_HostPrefixOnRoot(t *testing.T) {
	stats := []model.RouteStat{
		{Key: model.RouteKey{Host: "api.localhost", Method: "GET", Pattern: "/users/:id"}, Count: 5, Status2xx: 5},
		{Key: model.RouteKey{Host: "app.localhost", Method: "GET", Pattern: "/users/:id"}, Count: 3, Status2xx: 3},
	}
	// showHost=true → the host disambiguates two routes that share the path.
	root := stripANSI(renderRoutesTable(stats, -1, 160, 6, model.SortByRouteCount, true, "", ""))
	assert.Contains(t, root, "api.localhost /users/:id")
	assert.Contains(t, root, "app.localhost /users/:id")

	// showHost=false (per-host drilldown) → the prefix is dropped.
	drill := stripANSI(renderRoutesTable(stats[:1], -1, 160, 6, model.SortByRouteCount, false, "", ""))
	assert.NotContains(t, drill, "api.localhost /users")
	assert.Contains(t, drill, "/users/:id")
}

func TestRenderRoutesTable_SelectionPrefix(t *testing.T) {
	stats := []model.RouteStat{{Key: model.RouteKey{Method: "GET", Pattern: "/a"}, Count: 1, Status2xx: 1}}
	out := renderRoutesTable(stats, 0, 100, 4, model.SortByRouteCount, false, "", "")
	stripped := stripANSI(out)
	// First non-header line is the row; selection prefix is ">".
	lines := strings.Split(stripped, "\n")
	require.GreaterOrEqual(t, len(lines), 3)
	row := lines[2]
	assert.Equal(t, ">", strings.TrimRight(row, " ")[:1])
}

func TestRenderRoutesTable_StatusMarkersNoColor(t *testing.T) {
	stats := []model.RouteStat{
		{
			Key:       model.RouteKey{Method: "GET", Pattern: "/a"},
			Count:     8,
			Status2xx: 5,
			Status4xx: 2,
			Status5xx: 1,
		},
	}
	out := stripANSI(renderRoutesTable(stats, -1, 120, 4, model.SortByRouteCount, false, "", ""))
	assert.Contains(t, out, "2*", "4xx must carry a textual marker for NO_COLOR")
	assert.Contains(t, out, "1!", "5xx must carry a textual marker for NO_COLOR")
}

func TestRenderRoutesTable_PatternTruncation(t *testing.T) {
	long := "/api/v1/very-long-path-segment-that-should-not-overflow/and/wrap/the/row/" + strings.Repeat("x", 200)
	stats := []model.RouteStat{{Key: model.RouteKey{Method: "GET", Pattern: long}, Count: 1, Status2xx: 1}}
	width := 100
	out := renderRoutesTable(stats, -1, width, 3, model.SortByRouteCount, false, "", "")
	stripped := stripANSI(out)
	for _, line := range strings.Split(stripped, "\n") {
		assert.LessOrEqual(t, len(line), width*4, "row should not blow up uncontrollably (allowing for unicode)")
	}
	assert.Contains(t, stripped, "…", "long pattern should be truncated with an ellipsis")
}

func TestHighlightPattern_StylesPlaceholders(t *testing.T) {
	out := highlightPattern("/users/:uuid/orders/:id", false)
	// In a real terminal lipgloss may strip styling under termenv.Ascii;
	// the test just checks the placeholder text survives intact.
	assert.Contains(t, stripANSI(out), ":uuid")
	assert.Contains(t, stripANSI(out), ":id")
}

func TestHighlightPattern_SelectedRowSkipsStyling(t *testing.T) {
	// Selected rows already use reverse video; an extra tint on top would
	// fight the inversion. Make sure the function returns the raw input.
	in := "/users/:uuid"
	got := highlightPattern(in, true)
	assert.Equal(t, in, got)
}

func TestFormatStatusCount(t *testing.T) {
	assert.Equal(t, "    0", formatStatusCount(0, ""))
	assert.Equal(t, "    0", formatStatusCount(0, "!"))
	assert.Equal(t, "    7", formatStatusCount(7, ""))
	assert.Equal(t, "   3!", formatStatusCount(3, "!"))
	assert.Equal(t, "  12*", formatStatusCount(12, "*"))
}

func TestPadCell_NoPaddingWhenContentFitsExactly(t *testing.T) {
	// When the content already occupies the full cell width, pad is ≤ 0 and
	// the helpers must return the input untouched (no extra spaces, no
	// truncation): otherwise neighbouring columns would shift when a label
	// happens to land on the boundary.
	assert.Equal(t, "abc", padCellRight("abc", 3))
	assert.Equal(t, "abc", padCellRight("abc", 1))
	assert.Equal(t, "abc", padCellLeft("abc", 3))
	assert.Equal(t, "abc", padCellLeft("abc", 1))
}

func TestRenderRoutesTable_FallbacksOnMissingMethodAndPattern(t *testing.T) {
	// A bucket with empty Method or Pattern (e.g. a malformed access log)
	// must still render a row with the "—" placeholder rather than leaving
	// holes in the column grid.
	stats := []model.RouteStat{{Key: model.RouteKey{}, Count: 1, Status2xx: 1}}
	out := stripANSI(renderRoutesTable(stats, -1, 120, 4, model.SortByRouteCount, false, "", ""))
	assert.Contains(t, out, "—", "empty method and pattern must fall back to a dash")
}

func TestRenderRoutesTable_OverlongStatsClippedToBodyHeight(t *testing.T) {
	// More buckets than rows in the body → the renderer truncates instead
	// of letting the table push past `height`. Callers feed a viewport
	// already sized to fit, but this guard keeps a stale slice from
	// blowing the layout.
	stats := make([]model.RouteStat, 10)
	for i := range stats {
		stats[i] = model.RouteStat{Key: model.RouteKey{Method: "GET", Pattern: "/r"}, Count: i + 1, Status2xx: 1}
	}
	out := renderRoutesTable(stats, -1, 120, 4, model.SortByRouteCount, false, "", "")
	lines := strings.Split(out, "\n")
	// header + border (logHeaderHeight=2) + at most bodyHeight=2 rows.
	assert.LessOrEqual(t, len(lines), 4, "renderer must not exceed `height` lines")
}

func TestRenderRoutesTable_TinyHeightStillRendersOneRow(t *testing.T) {
	// height ≤ header → bodyHeight clamps to 1 so the table never collapses
	// to nothing; the user always sees at least the top row.
	stats := []model.RouteStat{{Key: model.RouteKey{Method: "GET", Pattern: "/a"}, Count: 1, Status2xx: 1}}
	out := stripANSI(renderRoutesTable(stats, -1, 120, 1, model.SortByRouteCount, false, "", ""))
	assert.Contains(t, out, "/a")
}

func TestRenderRoutesTable_VeryNarrowWidthClampsPattern(t *testing.T) {
	// Below the fixed-column footprint the renderer clamps the Pattern
	// column to a single cell rather than going negative. The row can
	// still overflow the requested width on a sub-72-col terminal — but
	// the renderer must not panic or truncate other columns.
	stats := []model.RouteStat{{Key: model.RouteKey{Method: "GET", Pattern: "/a"}, Count: 1, Status2xx: 1}}
	out := renderRoutesTable(stats, -1, 40, 4, model.SortByRouteCount, false, "", "")
	assert.NotEmpty(t, stripANSI(out))
}
