package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRoutesApp(agg *model.RouteAggregator) *App {
	app := newLogsApp(model.NewLogBuffer(100))
	app.routeAggregator = agg
	app.logSel = logSel{kind: logSelRoutes}
	return app
}

func trackAccess(agg *model.RouteAggregator, host, method, uri string, status int) {
	agg.Track(fetcher.LogEntry{
		Timestamp: time.Now(),
		Logger:    "http.log.access.log0",
		Host:      host,
		Method:    method,
		URI:       uri,
		Status:    status,
		Duration:  0.01,
	})
}

func TestFilterRouteStats_MatchesHost(t *testing.T) {
	stats := []model.RouteStat{
		{Key: model.RouteKey{Host: "api.localhost", Method: "GET", Pattern: "/users"}, Count: 1},
		{Key: model.RouteKey{Host: "app.localhost", Method: "GET", Pattern: "/posts"}, Count: 1},
	}
	got := filterRouteStats(append([]model.RouteStat(nil), stats...), "api")
	require.Len(t, got, 1)
	assert.Equal(t, "api.localhost", got[0].Key.Host)
}

func TestFilterRouteStats_MatchesMethodOrPattern(t *testing.T) {
	stats := []model.RouteStat{
		{Key: model.RouteKey{Method: "GET", Pattern: "/users"}, Count: 1},
		{Key: model.RouteKey{Method: "POST", Pattern: "/orders"}, Count: 1},
	}
	byMethod := filterRouteStats(append([]model.RouteStat(nil), stats...), "post")
	require.Len(t, byMethod, 1)
	assert.Equal(t, "POST", byMethod[0].Key.Method)

	byPattern := filterRouteStats(append([]model.RouteStat(nil), stats...), "users")
	require.Len(t, byPattern, 1)
	assert.Equal(t, "/users", byPattern[0].Key.Pattern)
}

func TestFilterRouteStats_NoMatchReturnsEmpty(t *testing.T) {
	stats := []model.RouteStat{{Key: model.RouteKey{Method: "GET", Pattern: "/x"}, Count: 1}}
	got := filterRouteStats(stats, "nothing-matches")
	assert.Empty(t, got)
}

func TestRoutesEmptyHint_NoTrafficYet(t *testing.T) {
	agg := model.NewRouteAggregator()
	app := newRoutesApp(agg)
	app.logSource = "/tmp/access.log"

	hint := app.routesEmptyHint(0)
	assert.Contains(t, hint, "/tmp/access.log", "must surface the listening source so the user knows where to look")
	assert.Contains(t, hint, "waiting")
}

func TestRoutesEmptyHint_NoTrafficNoSource(t *testing.T) {
	agg := model.NewRouteAggregator()
	app := newRoutesApp(agg)
	app.logSource = ""

	hint := app.routesEmptyHint(0)
	assert.Contains(t, hint, "Waiting for access logs")
	assert.NotContains(t, hint, "Listening on")
}

func TestRoutesEmptyHint_FilterMatchedNothing(t *testing.T) {
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/users/1", 200)
	app := newRoutesApp(agg)
	app.filter = "no-such-thing"

	hint := app.routesEmptyHint(0)
	assert.Contains(t, hint, "filter: no-such-thing")
}

func TestRoutesEmptyHint_NonZeroVisibleReturnsBlank(t *testing.T) {
	app := newRoutesApp(model.NewRouteAggregator())
	assert.Empty(t, app.routesEmptyHint(3))
}

func TestRoutesEmptyHint_HostDrillDownWithoutMatch(t *testing.T) {
	// Aggregator has buckets, no filter, but the per-host drill-down lands
	// on a host that has not produced any traffic in the *current* slice
	// (e.g. cursor selected before traffic, or buckets only for other hosts).
	// Hint must distinguish this from "no traffic at all".
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/x", 200)
	app := newRoutesApp(agg)
	app.logSel = logSel{kind: logSelRoutesHost, host: "ghost.localhost"}

	hint := app.routesEmptyHint(0)
	assert.Equal(t, "No access logs to aggregate yet", hint)
}

func TestRenderRoutesView_TinyHeightStillRenders(t *testing.T) {
	// height ≤ logHeaderHeight forces bodyHeight to 1, so the table still
	// renders one row instead of collapsing to nothing.
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/x", 200)
	app := newRoutesApp(agg)

	out := stripANSI(app.renderRoutesView(120, 1, ""))
	assert.NotEmpty(t, out)
}

func TestCurrentRouteStats_DrillsByHostAndFilters(t *testing.T) {
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/users/1", 200)
	trackAccess(agg, "api.localhost", "POST", "/orders", 201)
	trackAccess(agg, "app.localhost", "GET", "/dashboard", 200)
	app := newRoutesApp(agg)

	app.logSel = logSel{kind: logSelRoutesHost, host: "api.localhost"}
	app.filter = ""
	got := app.currentRouteStats()
	require.Len(t, got, 2, "drill-down on api.localhost must drop app.localhost")
	for _, s := range got {
		assert.Equal(t, "api.localhost", s.Key.Host)
	}

	app.filter = "post"
	got = app.currentRouteStats()
	require.Len(t, got, 1, "filter must compose with the host drill-down")
	assert.Equal(t, "POST", got[0].Key.Method)
}

func TestCurrentRouteStats_NilAggregatorReturnsNil(t *testing.T) {
	app := newRoutesApp(nil)
	assert.Nil(t, app.currentRouteStats())
}

func TestHandleLogsListKey_ClearRoutes_ResetsCursorAndOffset(t *testing.T) {
	// Regression: clearing the route stats was leaving logScrollOffset on
	// its previous value, so the next render started past the (now empty)
	// list and the user saw an apparently frozen blank table.
	agg := model.NewRouteAggregator()
	for i := 0; i < 50; i++ {
		trackAccess(agg, "api.localhost", "GET", "/r"+string(rune('a'+i%26)), 200)
	}
	app := newRoutesApp(agg)
	app.cursor = 30
	app.logScrollOffset = 25

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})

	assert.Equal(t, 0, app.cursor, "cursor must reset")
	assert.Equal(t, 0, app.logScrollOffset, "scroll offset must reset")
	assert.Equal(t, 0, agg.BucketCount(), "aggregator must be cleared")
}

func TestHandleLogsListKey_PauseIsNoOpInRoutesView(t *testing.T) {
	// p (pause) is meaningless in the aggregated By Route view: the
	// aggregator never loses data and nothing is being tailed past.
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/x", 200)
	app := newRoutesApp(agg)
	require.False(t, app.logFrozen)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.False(t, app.logFrozen, "pause must remain a no-op on the routes view")
}

func TestHandleLogsListKey_SortCyclesInRoutesView(t *testing.T) {
	app := newRoutesApp(model.NewRouteAggregator())
	require.Equal(t, model.SortByRouteCount, app.routeSortBy)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	assert.Equal(t, model.SortByRoutePattern, app.routeSortBy)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	assert.Equal(t, model.SortByRouteCount, app.routeSortBy, "S walks back to Count")
}

func TestRenderRoutesTable_RespectsWidthAt80Columns(t *testing.T) {
	// Regression: the previous implementation forced remaining≥12, which
	// pushed the row past `width` on terminals just under 83 cols. The fix
	// keeps the table strictly within its allotment from 72 cols upward.
	stats := []model.RouteStat{{
		Key:           model.RouteKey{Method: "GET", Pattern: "/users/:id"},
		Count:         10,
		Status2xx:     10,
		DurationSumMs: 100,
		DurationMaxMs: 30,
	}}
	for _, width := range []int{72, 80, 100, 160} {
		out := renderRoutesTable(stats, 0, width, 4, model.SortByRouteCount, false, "", "")
		for _, line := range strings.Split(stripANSI(out), "\n") {
			assert.LessOrEqualf(t, lipgloss.Width(line), width,
				"row exceeds requested width=%d: %q", width, line)
		}
	}
}

func TestSliceRoutesViewport(t *testing.T) {
	mk := func(n int) []model.RouteStat {
		out := make([]model.RouteStat, n)
		for i := range out {
			out[i] = model.RouteStat{Key: model.RouteKey{Method: "GET", Pattern: "/r"}, Count: i}
		}
		return out
	}

	tests := []struct {
		name       string
		total      int
		bodyHeight int
		cursor     int
		offset     int
		wantStart  int
		wantLen    int
		wantLocal  int
	}{
		{"empty", 0, 5, 0, 0, 0, 0, 0},
		{"zero body", 10, 0, 0, 0, 0, 10, 0}, // pass-through guard
		{"all visible", 5, 10, 2, 0, 0, 5, 2},
		{"cursor in window", 20, 5, 3, 0, 0, 5, 3},
		{"cursor below window scrolls down", 20, 5, 8, 0, 4, 5, 4},
		{"cursor above window scrolls up", 20, 5, 2, 7, 2, 5, 0},
		{"offset clamped past end", 20, 5, 19, 50, 15, 5, 4},
		{"negative offset clamped to 0", 5, 3, 0, -2, 0, 3, 0},
		{"local clamped when cursor past end", 10, 5, 99, 0, 5, 5, 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := newRoutesApp(model.NewRouteAggregator())
			app.cursor = tt.cursor
			app.logScrollOffset = tt.offset
			visible, local := app.sliceRoutesViewport(mk(tt.total), tt.bodyHeight)
			require.Len(t, visible, tt.wantLen)
			assert.Equal(t, tt.wantLocal, local)
			if tt.wantLen > 0 && tt.total > 0 {
				assert.Equal(t, tt.wantStart, visible[0].Count, "viewport must start at index %d", tt.wantStart)
			}
		})
	}
}

func TestRenderRoutesView_DelegatesToTable(t *testing.T) {
	// Smoke test the orchestrator: feeds traffic, asks for a render, and
	// checks that the resulting frame carries the table contents through.
	// The math (filter, sort, viewport) is tested in dedicated tests above;
	// this just guards against the wiring breaking silently.
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/users/1", 200)
	trackAccess(agg, "api.localhost", "POST", "/orders", 201)
	app := newRoutesApp(agg)

	out := stripANSI(app.renderRoutesView(160, 8, ""))
	assert.Contains(t, out, "/users/:id")
	assert.Contains(t, out, "/orders")
	assert.Contains(t, out, "api.localhost", "root view must keep the host prefix")
}

func TestRenderRoutesView_HostDrillDownDropsHostPrefix(t *testing.T) {
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/users/1", 200)
	app := newRoutesApp(agg)
	app.logSel = logSel{kind: logSelRoutesHost, host: "api.localhost"}

	out := stripANSI(app.renderRoutesView(160, 8, ""))
	assert.Contains(t, out, "/users/:id")
	assert.NotContains(t, out, "api.localhost /users", "drill-down should drop the host prefix")
}

func TestRenderRoutesView_SidepanelFocusedHidesCursor(t *testing.T) {
	// When the sidepanel owns keyboard focus the table must not also
	// reverse-highlight a row — the user would not know which panel the
	// next keypress drives. We render with focus on the sidepanel and
	// confirm no row carries the ">" prefix.
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/x", 200)
	app := newRoutesApp(agg)
	app.logSidepanelFocused = true

	out := stripANSI(app.renderRoutesView(120, 6, ""))
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " "), ">") {
			t.Errorf("sidepanel-focused render should not draw the row cursor: %q", line)
		}
	}
}

func TestNormalizeLogSel_StaleRoutesHostFallsBackToRoutes(t *testing.T) {
	// A stale per-host drill-down (host disappeared from the aggregator,
	// e.g. after Reset) must keep the user in the By Route view rather
	// than ejecting them to Access — that would be jarring and lose the
	// scope they were just looking at.
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/x", 200)
	app := newRoutesApp(agg)
	items := app.sidepanelItems()

	got := normalizeLogSel(items, logSel{kind: logSelRoutesHost, host: "gone.example"})
	assert.Equal(t, logSelRoutes, got.kind)
	assert.Empty(t, got.host)
}

func TestSidepanelItems_RoutesHostsComeFromAggregator(t *testing.T) {
	// The access buffer caps at 10 000 entries; the aggregator does not.
	// The By Route children must come from the aggregator so a busy server
	// keeps offering the same drill-downs even after the buffer wraps.
	agg := model.NewRouteAggregator()
	trackAccess(agg, "api.localhost", "GET", "/x", 200)
	trackAccess(agg, "app.localhost", "GET", "/y", 200)
	app := newRoutesApp(agg)

	items := app.sidepanelItems()
	var routesHosts []string
	for _, it := range items {
		if it.kind == logSelRoutesHost {
			routesHosts = append(routesHosts, it.host)
		}
	}
	assert.Equal(t, []string{"api.localhost", "app.localhost"}, routesHosts)
}
