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

func TestAccessLogHosts_DedupesAndSorts(t *testing.T) {
	buf := model.NewLogBuffer(100)
	now := time.Now()
	appendLog(t, buf, "b.com", "GET", 200, now)
	appendLog(t, buf, "a.com", "GET", 200, now)
	appendLog(t, buf, "b.com", "POST", 200, now)
	appendLog(t, buf, "c.com", "GET", 404, now)

	hosts := accessLogHosts(buf)
	assert.Equal(t, []string{"a.com", "b.com", "c.com"}, hosts)
}

func TestAccessLogHosts_SkipsEmpty(t *testing.T) {
	buf := model.NewLogBuffer(100)
	buf.Append(fetcher.LogEntry{Timestamp: time.Now(), Host: ""})
	buf.Append(fetcher.LogEntry{Timestamp: time.Now(), Host: "real.com"})
	assert.Equal(t, []string{"real.com"}, accessLogHosts(buf))
}

func TestAccessLogHosts_NilBuffer(t *testing.T) {
	assert.Nil(t, accessLogHosts(nil))
}

func TestSidepanelItems_HasRuntimeAndAccessTopLevel(t *testing.T) {
	app := newLogsApp(model.NewLogBuffer(10))
	items := app.sidepanelItems()

	require.GreaterOrEqual(t, len(items), 2)
	assert.Equal(t, logSelRuntime, items[0].kind)
	assert.Equal(t, logSelAccess, items[1].kind)
	assert.Equal(t, 0, items[0].indent)
	assert.Equal(t, 0, items[1].indent)
}

func TestSidepanelItems_HostChildrenAreIndented(t *testing.T) {
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "one.com", "GET", 200, time.Now())
	appendLog(t, buf, "two.com", "GET", 200, time.Now())
	app := newLogsApp(buf)

	items := app.sidepanelItems()
	require.Len(t, items, 5)
	assert.Equal(t, logSelAccessHost, items[2].kind)
	assert.Equal(t, 1, items[2].indent)
	assert.Equal(t, "one.com", items[2].host)
	assert.Equal(t, "two.com", items[3].host)
	assert.Equal(t, logSelRoutes, items[4].kind, "By Route entry sits at the bottom")
	assert.Equal(t, 0, items[4].indent)
}

func TestSidepanelItems_HidesByRouteWhenNoAggregator(t *testing.T) {
	// "By Route" only exists when the aggregator is wired up. Showing the
	// entry without one would lead to a perpetual "waiting…" hint with no
	// path forward.
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)
	app.routeAggregator = nil

	items := app.sidepanelItems()
	for _, it := range items {
		assert.NotEqual(t, logSelRoutes, it.kind, "By Route must not appear without an aggregator")
		assert.NotEqual(t, logSelRoutesHost, it.kind)
	}
}

func TestNormalizeLogSel_StaleHostFallsBackToAccess(t *testing.T) {
	items := []sidepanelItem{
		{kind: logSelRuntime},
		{kind: logSelAccess},
		{kind: logSelAccessHost, host: "known.com"},
	}
	got := normalizeLogSel(items, logSel{kind: logSelAccessHost, host: "gone.com"})
	assert.Equal(t, logSelAccess, got.kind, "stale host must fall back to the Access aggregate")
}

func TestNormalizeLogSel_KeepsLivingSelection(t *testing.T) {
	items := []sidepanelItem{
		{kind: logSelRuntime},
		{kind: logSelAccess},
		{kind: logSelAccessHost, host: "a.com"},
	}
	got := normalizeLogSel(items, logSel{kind: logSelAccessHost, host: "a.com"})
	assert.Equal(t, logSel{kind: logSelAccessHost, host: "a.com"}, got)
}

func TestMoveSidepanel_ClampsAtBounds(t *testing.T) {
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)

	app.moveSidepanel(-10) // already at top
	assert.Equal(t, logSelRuntime, app.logSel.kind)

	app.moveSidepanel(+10) // walk to bottom (now "By Route")
	assert.Equal(t, logSelRoutes, app.logSel.kind)

	app.moveSidepanel(+10) // clamp at bottom
	assert.Equal(t, logSelRoutes, app.logSel.kind)
}

func TestHandleLogsListKey_LeftResumesLiveFollow(t *testing.T) {
	// Leaving the table must clear the PAUSED indicator: keeping the pill
	// up while the user navigates scopes in the sidepanel is misleading.
	// When focus returns to the table the view is live again.
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)
	app.freezeLogs()
	require.True(t, app.logFrozen)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyLeft})
	assert.True(t, app.logSidepanelFocused)
	assert.False(t, app.logFrozen, "left must resume live follow")
	assert.Nil(t, app.logSnapshot, "snapshot must be cleared")
}

func TestMoveSidepanel_ResumesFrozen(t *testing.T) {
	// moveSidepanel is only reachable when the sidepanel has focus, which
	// implies ← already resumed. The defence-in-depth resumeLogs call
	// inside moveSidepanel guarantees frozen state stays cleared even if
	// someone called it outside that path.
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)
	app.freezeLogs()
	require.True(t, app.logFrozen)

	app.moveSidepanel(+1)
	assert.False(t, app.logFrozen, "navigation must leave live follow in place")
}

func TestSelectHost_PresentAndAbsent(t *testing.T) {
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "known.com", "GET", 200, time.Now())
	app := newLogsApp(buf)

	app.selectHost("known.com")
	assert.Equal(t, logSelAccessHost, app.logSel.kind)
	assert.Equal(t, "known.com", app.logSel.host)

	// Absent host: intent is remembered so selection sticks once traffic arrives.
	app.selectHost("future.com")
	assert.Equal(t, logSelAccessHost, app.logSel.kind)
	assert.Equal(t, "future.com", app.logSel.host)

	// Empty string falls back to the Access aggregate.
	app.selectHost("")
	assert.Equal(t, logSelAccess, app.logSel.kind)
}

func TestCurrentLogBuffer_RoutesByKind(t *testing.T) {
	access := model.NewLogBuffer(10)
	runtime := model.NewLogBuffer(10)
	app := newLogsApp(access)
	app.runtimeLogBuffer = runtime

	app.logSel = logSel{kind: logSelAccess}
	assert.Same(t, access, app.currentLogBuffer())

	app.logSel = logSel{kind: logSelAccessHost, host: "x.com"}
	assert.Same(t, access, app.currentLogBuffer())

	app.logSel = logSel{kind: logSelRuntime}
	assert.Same(t, runtime, app.currentLogBuffer())
}

func TestRenderLogsTab_HostSelectionHidesHostColumnAndFilters(t *testing.T) {
	buf := model.NewLogBuffer(10)
	now := time.Now()
	appendLog(t, buf, "wanted.com", "GET", 200, now)
	appendLog(t, buf, "other.com", "POST", 201, now)

	app := newLogsApp(buf)
	app.logSel = logSel{kind: logSelAccessHost, host: "wanted.com"}

	tbl := tableOnly(stripANSI(app.renderLogsTab(120, 20)))
	// Host column header must be gone and the other host must not leak in.
	assert.NotContains(t, tbl, "Host  ", "Host column header must disappear in per-host view")
	assert.NotContains(t, tbl, "other.com")
	assert.Contains(t, tbl, "GET")
}

func TestRenderLogsTab_RuntimeSelectionUsesRuntimeBuffer(t *testing.T) {
	access := model.NewLogBuffer(10)
	appendLog(t, access, "acc.com", "GET", 200, time.Now())
	runtime := model.NewLogBuffer(10)
	runtime.Append(fetcher.LogEntry{
		Timestamp: time.Now(),
		Level:     "error",
		Logger:    "tls.handshake",
		Message:   "tls boom",
	})

	app := newLogsApp(access)
	app.runtimeLogBuffer = runtime
	app.logSel = logSel{kind: logSelRuntime}

	tbl := tableOnly(stripANSI(app.renderLogsTab(120, 20)))
	assert.Contains(t, tbl, "tls boom")
	assert.Contains(t, tbl, "ERROR")
	assert.NotContains(t, tbl, "acc.com", "runtime view must not leak access entries")
}

func TestHandleLogsListKey_FocusTogglesViaArrowKeys(t *testing.T) {
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)
	require.False(t, app.logSidepanelFocused)

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyLeft})
	assert.True(t, app.logSidepanelFocused, "left must focus the sidepanel")

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRight})
	assert.False(t, app.logSidepanelFocused, "right must return focus to the table")
}

func TestHandleLogsListKey_SidepanelUpMovesSelection(t *testing.T) {
	app := newLogsApp(model.NewLogBuffer(10))
	app.logSidepanelFocused = true
	require.Equal(t, logSelAccess, app.logSel.kind)

	// Starting at Access (index 1 in [Runtime, Access]), going up lands on
	// Runtime. Going down would not demonstrate anything here since there
	// are no host children to move onto.
	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, logSelRuntime, app.logSel.kind)
}

func TestHandleLogsListKey_SidepanelEnterReturnsFocus(t *testing.T) {
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)
	app.logSidepanelFocused = true

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyEnter})
	assert.False(t, app.logSidepanelFocused, "non-empty view hands focus to the table")
}

func TestHandleLogsListKey_SidepanelEnterOnEmptyKeepsFocus(t *testing.T) {
	// Focus must not transfer to an empty table because the user would
	// have no visible signal that the keypress did anything; the sidepanel
	// remains focused until there is actually something to navigate.
	app := newLogsApp(model.NewLogBuffer(10)) // empty access buffer
	app.logSel = logSel{kind: logSelRuntime}  // and no runtime buffer either
	app.logSidepanelFocused = true

	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyEnter},
		{Type: tea.KeyRight},
		{Type: tea.KeyRunes, Runes: []rune{'l'}},
	} {
		_, _ = app.handleLogsListKey(key)
		assert.True(t, app.logSidepanelFocused,
			"empty view must not transfer focus to the table (key=%v)", key)
	}
}

func TestRenderLogsTab_SidepanelFocusHidesTableCursor(t *testing.T) {
	// When focus is on the sidepanel, the log table must render without a
	// selected-row highlight: seeing both a reversed row and a highlighted
	// sidepanel item at the same time makes it impossible to tell which
	// panel the next keypress will drive.
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)

	app.logSidepanelFocused = false
	withCursor := stripANSI(app.renderLogsTab(120, 20))
	app.logSidepanelFocused = true
	withoutCursor := stripANSI(app.renderLogsTab(120, 20))

	// The table row prefix is ">" when selected, " " otherwise. Isolate
	// the table half so the sidepanel's own ">" doesn't trip the check.
	assert.Contains(t, tableOnly(withCursor), ">",
		"table must show the cursor when it has focus")
	assert.NotContains(t, tableOnly(withoutCursor), ">",
		"table must not show the cursor when sidepanel has focus")
}

func TestRenderSidepanel_SelectedRowHasCaretPrefix(t *testing.T) {
	// The `>` prefix is the NO_COLOR-safe cue for "this row is selected",
	// mirroring what the log table does. It must be present regardless of
	// whether the sidepanel currently owns keyboard focus.
	items := []sidepanelItem{
		{kind: logSelRuntime, label: "Runtime"},
		{kind: logSelAccess, label: "Access"},
	}
	for _, focused := range []bool{true, false} {
		out := stripANSI(renderSidepanel(items, 1, focused, sidepanelFixedWidth, 10))
		lines := strings.Split(out, "\n")
		// Selected row is at index 1 of items; sidepanel inserts 2 blank
		// rows at the top to align with the table header, so it's line 3.
		require.GreaterOrEqual(t, len(lines), 4)
		assert.True(t, strings.HasPrefix(lines[3], ">"),
			"selected row must start with `>` (focused=%v), got %q", focused, lines[3])
		assert.False(t, strings.HasPrefix(lines[2], ">"),
			"non-selected row must not have the caret (focused=%v)", focused)
	}
}

func TestSwitchTab_ToLogsResetsToRuntimeWithSidepanelFocus(t *testing.T) {
	// The user expects a predictable landing spot every time they come
	// back to the Logs tab; persisting the last selection was confusing
	// when the sidepanel cursor ended up off-screen or on a stale host.
	buf := model.NewLogBuffer(10)
	appendLog(t, buf, "a.com", "GET", 200, time.Now())
	app := newLogsApp(buf)
	app.activeTab = tabCaddy
	app.tabStates[tabCaddy] = &tabState{}
	// Simulate leftover state from a previous visit to the Logs tab.
	app.logSel = logSel{kind: logSelAccessHost, host: "a.com"}
	app.logSidepanelFocused = false
	app.freezeLogs()
	require.True(t, app.logFrozen)

	app.switchTab(tabLogs)

	assert.Equal(t, logSelRuntime, app.logSel.kind, "Logs tab always lands on Runtime")
	assert.True(t, app.logSidepanelFocused, "sidepanel must be focused after re-entry")
	assert.False(t, app.logFrozen, "frozen mode must be cleared on re-entry")
}

func TestHandleLogsListKey_ClearTargetsCurrentBuffer(t *testing.T) {
	access := model.NewLogBuffer(10)
	appendLog(t, access, "a.com", "GET", 200, time.Now())
	runtime := model.NewLogBuffer(10)
	runtime.Append(fetcher.LogEntry{Timestamp: time.Now(), Logger: "admin.api"})

	app := newLogsApp(access)
	app.runtimeLogBuffer = runtime
	app.logSel = logSel{kind: logSelRuntime}

	_, _ = app.handleLogsListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	assert.Equal(t, 0, runtime.Len(), "clearing on runtime view must clear the runtime buffer")
	assert.Equal(t, 1, access.Len(), "access buffer must be untouched")
}
