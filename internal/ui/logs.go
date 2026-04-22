package ui

import (
	"fmt"
	"strings"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// renderLogsTab is the View() entry point for the Logs tab. It slices the
// filtered log list through the scroll offset (when frozen) or returns the
// live tail (when following), so the cursor always maps to a stable entry on
// screen. Layout is a left sidepanel (tree: Runtime / Access / per-host
// children) plus a table; the sidepanel stays visible at every width and the
// table columns absorb any squeeze instead.
func (a *App) renderLogsTab(width, height int) string {
	if a.logBuffer == nil && a.runtimeLogBuffer == nil {
		return greyStyle.Render(" Logs unavailable: Caddy is not local and --log-listen was not set.\n" +
			" Pass --log-listen :PORT and make sure Caddy can reach this address. See docs/logs.md.")
	}

	items := a.sidepanelItems()
	a.logSel = normalizeLogSel(items, a.logSel)

	// The sidepanel is the only affordance to switch scopes, so we keep it
	// visible at every width. On very narrow terminals the table columns
	// get squeezed or clip — acceptable, since making the sidepanel
	// disappear would leave the user unable to change scope at all.
	sidepanelW := sidepanelFixedWidth
	tableW := width - sidepanelW
	if tableW < 20 {
		tableW = 20
	}

	all := a.filteredLogEntries()
	rightStatus := a.buildLogsHeaderStatus()

	bodyHeight := height - logHeaderHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	visible, localCursor := a.sliceLogViewport(all, bodyHeight)
	// When the sidepanel owns keyboard focus, suppress the table row
	// highlight: seeing both the sidepanel selection and a reversed row
	// at the same time makes it unclear which panel the next keypress
	// will drive. -1 is never matched by `i == cursor` in the renderer.
	if a.logSidepanelFocused {
		localCursor = -1
	}

	emptyHint := a.logsEmptyHint(len(visible))

	var table string
	if a.logSel.kind == logSelRuntime {
		table = renderRuntimeLogTable(visible, localCursor, tableW, height, rightStatus, emptyHint)
	} else {
		hideHost := a.logSel.kind == logSelAccessHost
		table = renderLogTable(visible, localCursor, tableW, height, rightStatus, emptyHint, hideHost)
	}

	selIdx := sidepanelIndex(items, a.logSel)
	sidepanel := renderSidepanel(items, selIdx, a.logSidepanelFocused, sidepanelW, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidepanel, table)
}

// logsEmptyHint picks the message shown when the table body is empty: a
// filter-specific hint when a search turned up nothing, a host-specific hint
// when the per-host view is waiting on traffic, or a generic "waiting" banner
// otherwise.
func (a *App) logsEmptyHint(visibleCount int) string {
	if visibleCount > 0 {
		return ""
	}
	sourceLen := a.logSourceLen()
	if a.filter != "" && sourceLen > 0 {
		return "No matching log lines (filter: " + a.filter + ")"
	}
	if sourceLen > 0 && a.logSel.kind == logSelAccessHost {
		return "No access logs yet for " + a.logSel.host
	}
	if a.logSource != "" {
		return "Listening on " + a.logSource + " — waiting for log lines (it can take up to 30s)..."
	}
	return "Waiting for log lines (it can take up to 30s for the first lines to appear)..."
}

// filteredLogEntries returns the current source list through the active
// filter. When frozen, everything in the snapshot is available so the user
// can scroll through full history; when live, only the newest pageSize()
// entries are needed (cursor stays at 0). The host-specific selection is
// folded into the LogFilter so the buffer walk applies both in one pass
// instead of copying the whole ring and filtering on top.
func (a *App) filteredLogEntries() []fetcher.LogEntry {
	buf := a.currentLogBuffer()
	if buf == nil {
		return nil
	}
	filter := a.activeLogFilter()
	if a.logSel.kind == logSelAccessHost {
		filter.Host = a.logSel.host
	}

	if a.logFrozen {
		return filterEntriesWithLimit(a.logSnapshot, filter, 0)
	}
	return buf.Snapshot(filter, a.pageSize())
}

// sliceLogViewport returns the rows to draw and the cursor position within
// that slice, re-clamping the persisted offset against the actual viewport
// height so the cursor is always visible.
func (a *App) sliceLogViewport(all []fetcher.LogEntry, bodyHeight int) ([]fetcher.LogEntry, int) {
	if !a.logFrozen || bodyHeight <= 0 {
		return all, 0
	}
	offset := a.logScrollOffset
	if a.cursor < offset {
		offset = a.cursor
	} else if a.cursor >= offset+bodyHeight {
		offset = a.cursor - bodyHeight + 1
	}
	if offset < 0 {
		offset = 0
	}
	maxOffset := len(all) - bodyHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	end := offset + bodyHeight
	if end > len(all) {
		end = len(all)
	}
	visible := all[offset:end]
	local := a.cursor - offset
	if local < 0 {
		local = 0
	}
	if local >= len(visible) && len(visible) > 0 {
		local = len(visible) - 1
	}
	return visible, local
}

// logSourceLen returns the size of the source the view is reading from, used
// only to distinguish "no logs at all" from "filter matched nothing".
func (a *App) logSourceLen() int {
	if a.logFrozen {
		return len(a.logSnapshot)
	}
	buf := a.currentLogBuffer()
	if buf == nil {
		return 0
	}
	return buf.Len()
}

// filterEntriesWithLimit applies a LogFilter to a pre-captured slice and
// returns at most limit matching entries, preserving order. Used to re-filter
// the frozen snapshot without touching the live buffer.
func filterEntriesWithLimit(entries []fetcher.LogEntry, filter model.LogFilter, limit int) []fetcher.LogEntry {
	if limit <= 0 {
		limit = len(entries)
	}
	out := make([]fetcher.LogEntry, 0, min(limit, len(entries)))
	for _, e := range entries {
		if !filter.Matches(e) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// pausedBadgeStyle renders the "● PAUSED" pill on the right of the column
// header when the view is frozen.
var pausedBadgeStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#FFF8F0", Dark: "#1A1410"}).
	Background(warn).
	Padding(0, 1)

// buildLogsHeaderStatus returns the pre-styled right-aligned status chunk
// for the column header. Empty when there's nothing to say.
func (a *App) buildLogsHeaderStatus() string {
	var parts []string
	if n := a.droppedLogCount(); n > 0 {
		parts = append(parts, warnStyle.Render(fmt.Sprintf("dropped: %d", n)))
	}
	if a.filter != "" {
		parts = append(parts, helpStyle.Render("filter: "+a.filter))
	}
	if a.logFrozen {
		label := "● PAUSED"
		if n := a.newLogsSinceFreeze(); n > 0 {
			label += fmt.Sprintf(" · %d new", n)
		}
		parts = append(parts, pausedBadgeStyle.Render(label))
	}
	return strings.Join(parts, "  ")
}

// droppedLogCount reports how many entries have been evicted by the ring
// buffer backing the currently-selected log scope. Surfaces a truth the UI
// would otherwise hide: the tail window is sliding, not infinite.
func (a *App) droppedLogCount() int64 {
	buf := a.currentLogBuffer()
	if buf == nil {
		return 0
	}
	return buf.Dropped()
}

// newLogsSinceFreeze counts entries that have been appended to the buffer
// since the snapshot was taken.
func (a *App) newLogsSinceFreeze() int64 {
	buf := a.currentLogBuffer()
	if !a.logFrozen || buf == nil {
		return 0
	}
	delta := buf.WriteCount() - a.logFrozenAt
	if delta < 0 {
		return 0
	}
	return delta
}

func (a *App) activeLogFilter() model.LogFilter {
	return model.LogFilter{Search: a.filter}
}

// freezeLogs captures the current buffer and enters frozen mode. Called
// implicitly on scroll and explicitly on `p` from live. The snapshot comes
// from the currently-selected buffer, so switching selection while frozen
// would show a stale or wrong view; the caller (moveSidepanel) resumes live
// mode before changing selection to avoid that mismatch.
func (a *App) freezeLogs() {
	buf := a.currentLogBuffer()
	if buf == nil || a.logFrozen {
		return
	}
	a.logSnapshot = buf.Snapshot(model.LogFilter{}, 0)
	a.logFrozenAt = buf.WriteCount()
	a.logScrollOffset = 0
	a.cursor = 0
	a.logFrozen = true
}

// resumeLogs drops the snapshot and returns the view to live-follow mode.
func (a *App) resumeLogs() {
	a.logFrozen = false
	a.logSnapshot = nil
	a.logFrozenAt = 0
	a.cursor = 0
	a.logScrollOffset = 0
}

// clampLogScrollOffset keeps the cursor visible inside the viewport and
// prevents the offset from running past the end of the filtered list. Called
// after every cursor mutation; render re-clamps against the true viewport
// height so any drift is self-correcting.
func (a *App) clampLogScrollOffset(total int) {
	if !a.logFrozen {
		a.logScrollOffset = 0
		return
	}
	bodyHeight := a.pageSize() - logHeaderHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	if a.cursor < a.logScrollOffset {
		a.logScrollOffset = a.cursor
	} else if a.cursor >= a.logScrollOffset+bodyHeight {
		a.logScrollOffset = a.cursor - bodyHeight + 1
	}
	if a.logScrollOffset < 0 {
		a.logScrollOffset = 0
	}
	maxOffset := total - bodyHeight
	if maxOffset < 0 {
		maxOffset = 0
	}
	if a.logScrollOffset > maxOffset {
		a.logScrollOffset = maxOffset
	}
}

// handleLogsListKey processes keystrokes when the Logs tab is in list mode.
// Focus routing: ←/→ toggle between sidepanel and table; up/down navigate
// whichever has focus. Navigation in the table auto-freezes from live mode
// so the list stops sliding; `f`, `home`, or `p` resume live follow.
func (a *App) handleLogsListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if cmd, ok := a.handleTabSwitch(key); ok {
		return a, cmd
	}

	if a.logSidepanelFocused {
		switch key {
		case "q", "ctrl+c":
			return a, tea.Quit
		case "up", "k":
			a.moveSidepanel(-1)
			return a, nil
		case "down", "j":
			a.moveSidepanel(+1)
			return a, nil
		case "home":
			a.moveSidepanel(-len(a.sidepanelItems()))
			return a, nil
		case "end":
			a.moveSidepanel(len(a.sidepanelItems()))
			return a, nil
		case "right", "l", "enter":
			// Refuse to hand focus to an empty table: there's nothing to
			// navigate over there, and the visible state (focused sidepanel,
			// empty viewport) would be indistinguishable from "focus moved
			// but nothing happened". Keep focus here until traffic arrives.
			if len(a.filteredLogEntries()) == 0 {
				return a, nil
			}
			a.logSidepanelFocused = false
			return a, nil
		case "?":
			a.prevMode = a.mode
			a.mode = viewHelp
			return a, nil
		}
		return a, nil
	}

	switch key {
	case "left", "h":
		// Leaving the table always resumes live follow: keeping the
		// PAUSED pill up while the user is navigating scopes in the
		// sidepanel is misleading — pause is about stopping the table
		// from sliding while you read a row, and the user is done
		// reading. When they come back, the table scrolls live.
		a.logSidepanelFocused = true
		a.resumeLogs()
		return a, nil
	case "f", "home":
		if a.logFrozen {
			a.resumeLogs()
			return a, nil
		}
	case "up", "down", "pgup", "pgdown", "end", "j", "k":
		if !a.logFrozen && a.currentLogBuffer() != nil && a.currentLogBuffer().Len() > 0 {
			a.freezeLogs()
		}
	}

	all := a.filteredLogEntries()
	maxIdx := len(all) - 1
	if maxIdx < 0 {
		maxIdx = 0
	}
	moveCursor(key, &a.cursor, maxIdx, a.pageSize())
	a.clampLogScrollOffset(len(all))

	switch key {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "/":
		a.mode = viewFilter
		a.filter = ""
	case "p":
		if a.logFrozen {
			a.resumeLogs()
		} else {
			a.freezeLogs()
		}
	case "c":
		if buf := a.currentLogBuffer(); buf != nil {
			buf.Clear()
			a.resumeLogs()
			a.status = "log buffer cleared"
		}
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
	}
	return a, nil
}

// logsHelpBindings returns the bindings shown at the bottom of the Logs tab.
func logsHelpBindings(frozen bool) []binding {
	pauseLabel := "pause"
	if frozen {
		pauseLabel = "resume"
	}
	bindings := []binding{
		{"↑/↓", "navigate"},
		{"←/→", "panel"},
		{"/", "filter"},
		{"p", pauseLabel},
	}
	if frozen {
		bindings = append(bindings, binding{"f", "follow"})
	}
	bindings = append(bindings,
		binding{"c", "clear"},
		binding{"Tab/S-Tab", "switch"},
		binding{"q", "quit"},
	)
	return bindings
}
