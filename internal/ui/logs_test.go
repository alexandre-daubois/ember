package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLogsApp(buf *model.LogBuffer) *App {
	return &App{
		activeTab: tabLogs,
		tabs:      []tab{tabCaddy, tabConfig, tabCertificates, tabLogs},
		tabStates: map[tab]*tabState{
			tabCaddy: {}, tabConfig: {}, tabCertificates: {}, tabLogs: {},
		},
		history:   newHistoryStore(),
		logBuffer: buf,
		logSource: "/tmp/access.log",
		width:     120,
		height:    30,
	}
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

func TestRenderLogsTab_NoBuffer_ShowsHelp(t *testing.T) {
	app := newLogsApp(nil)
	out := stripANSI(app.renderLogsTab(120))
	assert.Contains(t, out, "Logs unavailable")
	assert.Contains(t, out, "--log-listen")
}

func TestRenderLogsTab_EmptyBuffer_ShowsWaiting(t *testing.T) {
	buf := model.NewLogBuffer(100)
	app := newLogsApp(buf)
	out := stripANSI(app.renderLogsTab(120))
	assert.Contains(t, out, "Listening on /tmp/access.log")
}

func TestRenderLogsTab_ShowsRecentEntriesNewestFirst(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "a.com", "GET", 200, now.Add(-3*time.Second))
	appendLog(t, buf, "b.com", "POST", 500, now.Add(-2*time.Second))
	appendLog(t, buf, "c.com", "DELETE", 404, now.Add(-1*time.Second))

	app := newLogsApp(buf)
	out := stripANSI(app.renderLogsTab(120))

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
	out := stripANSI(app.renderLogsTab(120))
	assert.Contains(t, out, "a.com")
	assert.NotContains(t, out, "b.com")

	// ...and against method.
	app.filter = "post"
	out = stripANSI(app.renderLogsTab(120))
	assert.Contains(t, out, "b.com")
	assert.NotContains(t, out, "a.com")
}

func TestRenderLogsTab_SearchFiltersByStatusCode(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "ok.com", "GET", 200, now)
	appendLog(t, buf, "fail.com", "GET", 503, now)

	app := newLogsApp(buf)
	app.filter = "503"

	out := stripANSI(app.renderLogsTab(120))
	assert.Contains(t, out, "fail.com")
	assert.NotContains(t, out, "ok.com")
}

func TestRenderLogsTab_FilterLabelShown(t *testing.T) {
	buf := model.NewLogBuffer(100)
	app := newLogsApp(buf)
	app.filter = "foo"

	out := stripANSI(app.renderLogsTab(120))
	assert.Contains(t, out, "foo")
}

func TestHandleLogsListKey_Pause(t *testing.T) {
	buf := model.NewLogBuffer(100)
	app := newLogsApp(buf)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.True(t, app.logPaused)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.False(t, app.logPaused)
}

func TestPause_FreezesRenderedEntries(t *testing.T) {
	// Pausing must snapshot the buffer so subsequent Appends do not leak
	// into the rendered view until the user resumes.
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "first.com", "GET", 200, now)
	appendLog(t, buf, "second.com", "GET", 200, now)

	app := newLogsApp(buf)
	// Enter pause.
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.True(t, app.logPaused)
	require.Len(t, app.logPausedSnapshot, 2)

	// New entries arrive after pause: the live buffer grows but the view
	// must still only show the frozen two.
	appendLog(t, buf, "after-pause.com", "GET", 200, now)
	out := stripANSI(app.renderLogsTab(120))
	assert.Contains(t, out, "first.com")
	assert.Contains(t, out, "second.com")
	assert.NotContains(t, out, "after-pause.com")
	assert.Contains(t, out, "PAUSED")

	// Resuming drops the cache and reveals every buffered entry.
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.False(t, app.logPaused)
	require.Nil(t, app.logPausedSnapshot)
	out = stripANSI(app.renderLogsTab(120))
	assert.Contains(t, out, "after-pause.com")
	assert.NotContains(t, out, "PAUSED")
}

func TestPause_EmptyBuffer_StillBlocksLiveEntries(t *testing.T) {
	buf := model.NewLogBuffer(100)
	app := newLogsApp(buf)

	// Pause on a completely empty buffer.
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	require.True(t, app.logPaused)

	// New entries arrive after pause.
	appendLog(t, buf, "late.com", "GET", 200, time.Now())

	out := stripANSI(app.renderLogsTab(120))
	assert.NotContains(t, out, "late.com", "entries arriving after pause on empty buffer must not appear")
	assert.Contains(t, out, "PAUSED")
}

func TestPause_FilterStillAppliesToFrozenWindow(t *testing.T) {
	// While paused, changing the search re-slices the frozen window rather
	// than flashing live data.
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "a.com", "GET", 200, now)
	appendLog(t, buf, "b.com", "GET", 500, now)

	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})

	// Post-pause, a new entry arrives. Search for "500": we should still
	// see only b.com (from the snapshot), not any fresh 500 from the live
	// buffer.
	appendLog(t, buf, "late-5xx.com", "GET", 503, now)
	app.filter = "500"

	out := stripANSI(app.renderLogsTab(120))
	assert.Contains(t, out, "b.com")
	assert.NotContains(t, out, "late-5xx.com")
}

func TestHandleLogsListKey_Clear(t *testing.T) {
	buf := model.NewLogBuffer(100)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	require.Equal(t, 1, buf.Len())

	app := newLogsApp(buf)
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assert.Equal(t, 0, buf.Len())
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
	assert.Equal(t, "third.com", app.filter, "filter must be the host the cursor was on, not hosts[0]")
	assert.Equal(t, 0, app.cursor)
}

func TestHandleListKey_JumpFromCaddyToLogs_NoHostSelectedSwitchesWithoutFilter(t *testing.T) {
	buf := model.NewLogBuffer(10)
	app := newCaddyAppWithHosts(buf)
	app.cursor = 0

	_, _ = app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	assert.Equal(t, tabLogs, app.activeTab)
	assert.Empty(t, app.filter, "no host to capture: filter must remain empty")
}

func TestHandleListKey_JumpFromCaddyToLogs_NoBufferIsNoOp(t *testing.T) {
	app := newCaddyAppWithHosts(nil, "a.com", "b.com")
	app.cursor = 1

	_, _ = app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	assert.Equal(t, tabCaddy, app.activeTab, "without a log buffer, l must not switch tab")
}
