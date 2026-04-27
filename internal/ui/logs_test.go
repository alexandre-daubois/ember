package ui

import (
	"fmt"
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

func newLogsApp(buf *model.LogBuffer) *App {
	// Mirror the production wiring: startNetListener always creates the
	// access buffer and the route aggregator together. Tests that need a
	// nil aggregator (to exercise that branch) override the field on the
	// returned App, see newRoutesApp.
	return &App{
		activeTab: tabLogs,
		tabs:      []tab{tabCaddy, tabConfig, tabCertificates, tabLogs},
		tabStates: map[tab]*tabState{
			tabCaddy: {}, tabConfig: {}, tabCertificates: {}, tabLogs: {},
		},
		history:         newHistoryStore(),
		logBuffer:       buf,
		routeAggregator: model.NewRouteAggregator(),
		logSource:       "/tmp/access.log",
		width:           120,
		height:          30,
		logSel:          logSel{kind: logSelAccess},
	}
}

// tableOnly extracts the table portion of a full Logs-tab render (right of
// the sidepanel border). Pause/freeze tests assert on what's visible IN the
// table, independent of the sidepanel tree which always reflects live state.
func tableOnly(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "│"); idx >= 0 {
			lines[i] = line[idx+len("│"):]
		}
	}
	return strings.Join(lines, "\n")
}

func appendLog(t *testing.T, buf *model.LogBuffer, host, method string, status int, ts time.Time) {
	t.Helper()
	buf.Append(fetcher.LogEntry{
		Timestamp: ts,
		Host:      host,
		Method:    method,
		Status:    status,
		URI:       "/path/" + method,
		Duration:  0.012,
	})
}

func TestBuildLogsHeaderStatus_DroppedChip(t *testing.T) {
	// Tiny buffer so the wrap happens deterministically; anything beyond
	// capacity must be surfaced as a "dropped: N" chip so the user knows
	// the tail window is sliding rather than infinite.
	buf := model.NewLogBuffer(2)
	app := newLogsApp(buf)

	assert.NotContains(t, stripANSI(app.buildLogsHeaderStatus()), "dropped",
		"no chip while the buffer has not wrapped")

	now := time.Now()
	for i := 0; i < 5; i++ {
		appendLog(t, buf, "h.com", "GET", 200, now)
	}
	assert.Contains(t, stripANSI(app.buildLogsHeaderStatus()), "dropped: 3",
		"wrapped buffer must surface the eviction count")
}

func TestRenderLogsTab_NoBuffer_ShowsHelp(t *testing.T) {
	app := newLogsApp(nil)
	out := stripANSI(app.renderLogsTab(120, 20))
	assert.Contains(t, out, "Logs unavailable")
	assert.Contains(t, out, "--log-listen")
}

func TestRenderLogsTab_EmptyBuffer_ShowsWaiting(t *testing.T) {
	buf := model.NewLogBuffer(100)
	app := newLogsApp(buf)
	out := stripANSI(app.renderLogsTab(120, 20))
	assert.Contains(t, out, "Listening on /tmp/access.log")
}

func TestRenderLogsTab_FillsRequestedHeight(t *testing.T) {
	buf := model.NewLogBuffer(100)
	app := newLogsApp(buf)
	out := app.renderLogsTab(120, 20)
	assert.Equal(t, 20, lipgloss.Height(out),
		"empty buffer must still render exactly the requested height")

	appendLog(t, buf, "single.com", "GET", 200, time.Now())
	out = app.renderLogsTab(120, 20)
	assert.Equal(t, 20, lipgloss.Height(out),
		"a single log row must still render exactly the requested height")

	app.filter = "single"
	out = app.renderLogsTab(120, 20)
	assert.Equal(t, 20, lipgloss.Height(out),
		"the filter banner must not push the table beyond the requested height")
}

func TestRenderLogsTab_ShowsRecentEntriesNewestFirst(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "a.com", "GET", 200, now.Add(-3*time.Second))
	appendLog(t, buf, "b.com", "POST", 500, now.Add(-2*time.Second))
	appendLog(t, buf, "c.com", "DELETE", 404, now.Add(-1*time.Second))

	app := newLogsApp(buf)
	out := stripANSI(app.renderLogsTab(120, 20))

	assert.Contains(t, out, "a.com")
	assert.Contains(t, out, "b.com")
	assert.Contains(t, out, "c.com")

	cIdx := strings.Index(out, "c.com")
	aIdx := strings.Index(out, "a.com")
	require.NotEqual(t, -1, cIdx)
	require.NotEqual(t, -1, aIdx)
	assert.Less(t, cIdx, aIdx)
}

func TestRenderLogsTab_SearchFiltersAcrossColumns(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "a.com", "GET", 200, now)
	appendLog(t, buf, "b.com", "POST", 200, now)

	// The free-text filter replaces all dedicated per-column filters. It must
	// match against host...
	app := newLogsApp(buf)
	app.filter = "a.com"
	tbl := tableOnly(stripANSI(app.renderLogsTab(120, 20)))
	assert.Contains(t, tbl, "a.com")
	assert.NotContains(t, tbl, "b.com")

	// ...and against method.
	app.filter = "post"
	tbl = tableOnly(stripANSI(app.renderLogsTab(120, 20)))
	assert.Contains(t, tbl, "b.com")
	assert.NotContains(t, tbl, "a.com")
}

func TestRenderLogsTab_SearchFiltersByStatusCode(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "ok.com", "GET", 200, now)
	appendLog(t, buf, "fail.com", "GET", 503, now)

	app := newLogsApp(buf)
	app.filter = "503"

	tbl := tableOnly(stripANSI(app.renderLogsTab(120, 20)))
	assert.Contains(t, tbl, "fail.com")
	assert.NotContains(t, tbl, "ok.com")
}

func TestRenderLogsTab_FilterLabelShown(t *testing.T) {
	buf := model.NewLogBuffer(100)
	app := newLogsApp(buf)
	app.filter = "foo"

	out := stripANSI(app.renderLogsTab(120, 20))
	assert.Contains(t, out, "foo")
}

func TestHandleLogsListKey_PauseToggle(t *testing.T) {
	buf := model.NewLogBuffer(100)
	app := newLogsApp(buf)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.True(t, app.logFrozen)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.False(t, app.logFrozen)
}

func TestPause_FreezesRenderedEntries(t *testing.T) {
	// Pausing with `p` must snapshot the buffer so subsequent Appends do not
	// leak into the rendered view until the user resumes.
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "first.com", "GET", 200, now)
	appendLog(t, buf, "second.com", "GET", 200, now)

	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.True(t, app.logFrozen)
	require.Len(t, app.logSnapshot, 2)

	appendLog(t, buf, "after-pause.com", "GET", 200, now)
	full := stripANSI(app.renderLogsTab(120, 20))
	tbl := tableOnly(full)
	assert.Contains(t, tbl, "first.com")
	assert.Contains(t, tbl, "second.com")
	assert.NotContains(t, tbl, "after-pause.com")
	assert.Contains(t, full, "PAUSED")

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.False(t, app.logFrozen)
	require.Nil(t, app.logSnapshot)
	full = stripANSI(app.renderLogsTab(120, 20))
	assert.Contains(t, tableOnly(full), "after-pause.com")
	assert.NotContains(t, full, "PAUSED")
}

func TestPause_EmptyBuffer_StillBlocksLiveEntries(t *testing.T) {
	buf := model.NewLogBuffer(100)
	app := newLogsApp(buf)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.True(t, app.logFrozen)

	appendLog(t, buf, "late.com", "GET", 200, time.Now())

	out := stripANSI(app.renderLogsTab(120, 20))
	assert.NotContains(t, tableOnly(out), "late.com", "entries arriving after pause on empty buffer must not appear in the table")
	assert.Contains(t, out, "PAUSED")
}

func TestPause_FilterStillAppliesToFrozenWindow(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "a.com", "GET", 200, now)
	appendLog(t, buf, "b.com", "GET", 500, now)

	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})

	appendLog(t, buf, "late-5xx.com", "GET", 503, now)
	app.filter = "500"

	tbl := tableOnly(stripANSI(app.renderLogsTab(120, 20)))
	assert.Contains(t, tbl, "b.com")
	assert.NotContains(t, tbl, "late-5xx.com")
}

func TestHandleLogsListKey_Clear(t *testing.T) {
	buf := model.NewLogBuffer(100)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	require.Equal(t, 1, buf.Len())

	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assert.Equal(t, 0, buf.Len())
	assert.False(t, app.logFrozen, "clearing must also resume live follow")
}

func TestHandleLogsListKey_SlashEntersFilterMode(t *testing.T) {
	app := newLogsApp(model.NewLogBuffer(10))
	app.filter = "leftover"

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, viewFilter, app.mode)
	assert.Empty(t, app.filter, "entering filter mode must start from an empty input")
}

func TestActiveLogFilter_UsesSearch(t *testing.T) {
	app := newLogsApp(model.NewLogBuffer(10))
	app.filter = "abc"

	got := app.activeLogFilter()
	assert.Equal(t, "abc", got.Search)
}

func TestScroll_AutoFreezesFromLive(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "a.com", "GET", 200, now.Add(-2*time.Second))
	appendLog(t, buf, "b.com", "GET", 200, now.Add(-time.Second))
	appendLog(t, buf, "c.com", "GET", 200, now)

	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyDown})

	assert.True(t, app.logFrozen, "pressing down from live view must auto-freeze")
	assert.Equal(t, 1, app.cursor)
}

func TestScroll_FrozenViewDoesNotShiftOnNewArrival(t *testing.T) {
	// The whole point of auto-freeze: scrolling must not be disturbed by
	// new logs arriving at the top of the buffer.
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "entry-a.com", "GET", 200, now.Add(-3*time.Second))
	appendLog(t, buf, "entry-b.com", "GET", 200, now.Add(-2*time.Second))
	appendLog(t, buf, "entry-c.com", "GET", 200, now.Add(-time.Second))

	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyDown})
	require.True(t, app.logFrozen)
	require.Equal(t, 1, app.cursor)

	for i := 0; i < 5; i++ {
		appendLog(t, buf, "fresh.com", "GET", 200, now)
	}

	full := stripANSI(app.renderLogsTab(120, 20))
	assert.NotContains(t, tableOnly(full), "fresh.com", "frozen view must hide live arrivals in the table")
	assert.Contains(t, full, "5 new", "banner must surface how many lines are hidden")
}

func TestScroll_FollowKeyResumesWhenFrozen(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "a.com", "GET", 200, now.Add(-time.Second))
	appendLog(t, buf, "b.com", "GET", 200, now)

	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyDown})
	require.True(t, app.logFrozen)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	assert.False(t, app.logFrozen, "f must resume live follow")
	assert.Equal(t, 0, app.cursor)
}

func TestScroll_HomeAlsoResumes(t *testing.T) {
	// Home remains available for keyboards that have it; Mac users lean on f.
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "a.com", "GET", 200, now)

	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.True(t, app.logFrozen)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyHome})
	assert.False(t, app.logFrozen, "home must also resume live follow")
}

func TestScroll_OffsetLetsCursorGoBeyondViewport(t *testing.T) {
	// With N entries larger than the viewport, scrolling must reveal older
	// rows via logScrollOffset.
	buf := model.NewLogBuffer(100)
	now := time.Now()
	for i := 0; i < 30; i++ {
		appendLog(t, buf, fmt.Sprintf("host-%02d.com", i), "GET", 200, now.Add(time.Duration(i)*time.Second))
	}

	app := newLogsApp(buf)
	app.height = 20 // bodyHeight ~ 9, much smaller than 30 entries

	for i := 0; i < 15; i++ {
		_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyDown})
	}

	assert.True(t, app.logFrozen)
	assert.Equal(t, 15, app.cursor)
	assert.Greater(t, app.logScrollOffset, 0, "cursor past the viewport must grow the scroll offset")
}

func TestScroll_FilterClearsScrollOffset(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	for i := 0; i < 10; i++ {
		appendLog(t, buf, fmt.Sprintf("host-%d.com", i), "GET", 200, now.Add(time.Duration(i)*time.Second))
	}

	app := newLogsApp(buf)
	for i := 0; i < 5; i++ {
		_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	require.Equal(t, 5, app.cursor)

	app.mode = viewFilter
	_, _ = app.handleFilterKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})

	assert.Equal(t, 0, app.cursor)
	assert.Equal(t, 0, app.logScrollOffset)
}

func TestHeader_LiveHasNoPausedPill(t *testing.T) {
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)

	out := stripANSI(app.renderLogsTab(120, 20))
	assert.NotContains(t, out, "PAUSED")
}

func TestHeader_FrozenShowsPausedPill(t *testing.T) {
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	out := stripANSI(app.renderLogsTab(120, 20))
	assert.Contains(t, out, "PAUSED")
}

func TestHeader_FollowBindingAppearsOnlyWhenFrozen(t *testing.T) {
	// Live: help shows pause, no follow.
	help := stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabLogs, false, false))
	assert.Contains(t, help, "pause")
	assert.NotContains(t, help, "follow")

	// Frozen: help shows resume + follow hint.
	help = stripANSI(renderHelp(model.SortByIndex, model.SortByHost, model.SortByCertDomain, model.SortByUpstreamAddress, model.SortByRouteCount, false, 120, tabLogs, true, false))
	assert.Contains(t, help, "resume")
	assert.Contains(t, help, "follow")
}

func TestClampCursor_LeavesLogsTabAlone(t *testing.T) {
	// Regression: clampCursor used to fall through to filteredThreads() on
	// the Logs tab, resetting the cursor to 0 on every 1s metrics tick.
	buf := model.NewLogBuffer(10)
	now := time.Now()
	for i := 0; i < 5; i++ {
		appendLog(t, buf, "h.com", "GET", 200, now.Add(time.Duration(i)*time.Second))
	}
	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyDown})
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyDown})
	require.Equal(t, 2, app.cursor)

	app.clampCursor()

	assert.Equal(t, 2, app.cursor, "clampCursor must not touch the Logs tab cursor")
}

func newCaddyAppWithHosts(buf *model.LogBuffer, hosts ...string) *App {
	hd := make([]model.HostDerived, len(hosts))
	for i, h := range hosts {
		hd[i] = model.HostDerived{Host: h}
	}
	app := &App{
		activeTab: tabCaddy,
		tabs:      []tab{tabCaddy, tabConfig, tabCertificates, tabLogs},
		tabStates: map[tab]*tabState{
			tabCaddy: {}, tabConfig: {}, tabCertificates: {}, tabLogs: {},
		},
		history:   newHistoryStore(),
		logBuffer: buf,
		width:     120,
		height:    30,
	}
	app.state.HostDerived = hd
	return app
}

func TestHandleListKey_JumpFromCaddyToLogs_PreservesSelectedHost(t *testing.T) {
	// Regression: switchTab overwrites a.cursor with the Logs tab's saved
	// cursor (0 on first switch), so reading hosts[a.cursor] AFTER switchTab
	// would index the wrong host. The fix captures the host before switching.
	buf := model.NewLogBuffer(10)
	app := newCaddyAppWithHosts(buf, "first.com", "second.com", "third.com")
	app.cursor = 2

	_, _ = app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	assert.Equal(t, tabLogs, app.activeTab)
	assert.Equal(t, logSelAccessHost, app.logSel.kind, "sidepanel must land on a host entry")
	assert.Equal(t, "third.com", app.logSel.host, "selected host must be the cursor's, not hosts[0]")
	assert.Equal(t, 0, app.cursor)
}

func TestHandleListKey_JumpFromCaddyToLogs_NoHostLandsOnRuntimeDefault(t *testing.T) {
	buf := model.NewLogBuffer(10)
	app := newCaddyAppWithHosts(buf)
	app.cursor = 0

	_, _ = app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	assert.Equal(t, tabLogs, app.activeTab)
	assert.Equal(t, logSelRuntime, app.logSel.kind, "switching to Logs without a host lands on the Runtime default")
	assert.Empty(t, app.logSel.host)
}

func TestHandleListKey_JumpFromCaddyToLogs_NoBufferIsNoOp(t *testing.T) {
	app := newCaddyAppWithHosts(nil, "a.com", "b.com")
	app.cursor = 1

	_, _ = app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	assert.Equal(t, tabCaddy, app.activeTab, "without a log buffer, l must not switch tab")
}
