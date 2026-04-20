package ui

import (
	"fmt"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// renderLogsTab is the View() entry point for the Logs tab. It slices the
// filtered log list through the scroll offset (when frozen) or returns the
// live tail (when following), so the cursor always maps to a stable entry on
// screen.
func (a *App) renderLogsTab(width, height int) string {
	if a.logBuffer == nil {
		return greyStyle.Render(" Logs unavailable: Caddy is not local and --log-listen was not set.\n" +
			" Pass --log-listen :PORT and make sure Caddy can reach this address. See docs/logs.md.")
	}

	all := a.filteredLogEntries()
	rightStatus := a.buildLogsHeaderStatus()

	bodyHeight := height - logHeaderHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	visible, localCursor := a.sliceLogViewport(all, bodyHeight)

	sourceLen := a.logSourceLen()
	emptyHint := "Waiting for log lines (it can take up to 30s for the first lines to appear)..."
	if a.logSource != "" {
		emptyHint = "Listening on " + a.logSource + " — waiting for log lines (it can take up to 30s)..."
	}
	if a.filter != "" && sourceLen > 0 && len(visible) == 0 {
		emptyHint = "No matching log lines (filter: " + a.filter + ")"
	}

	return renderLogTable(visible, localCursor, width, height, rightStatus, emptyHint)
}

// filteredLogEntries returns the current source list through the active
// filter. When frozen, everything in the snapshot is available so the user
// can scroll through full history; when live, only the newest pageSize()
// entries are needed (cursor stays at 0).
func (a *App) filteredLogEntries() []fetcher.LogEntry {
	if a.logBuffer == nil {
		return nil
	}
	filter := a.activeLogFilter()
	if a.logFrozen {
		return filterEntriesWithLimit(a.logSnapshot, filter, 0)
	}
	return a.logBuffer.Snapshot(filter, a.pageSize())
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
	if a.logBuffer == nil {
		return 0
	}
	return a.logBuffer.Len()
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
	var pill string
	if a.logFrozen {
		label := "● PAUSED"
		if n := a.newLogsSinceFreeze(); n > 0 {
			label += fmt.Sprintf(" · %d new", n)
		}
		pill = pausedBadgeStyle.Render(label)
	}
	if a.filter == "" {
		return pill
	}
	filterChip := helpStyle.Render("filter: " + a.filter)
	if pill == "" {
		return filterChip
	}
	return filterChip + "  " + pill
}

// newLogsSinceFreeze counts entries that have been appended to the buffer
// since the snapshot was taken.
func (a *App) newLogsSinceFreeze() int64 {
	if !a.logFrozen || a.logBuffer == nil {
		return 0
	}
	delta := a.logBuffer.WriteCount() - a.logFrozenAt
	if delta < 0 {
		return 0
	}
	return delta
}

func (a *App) activeLogFilter() model.LogFilter {
	return model.LogFilter{Search: a.filter}
}

// freezeLogs captures the current buffer and enters frozen mode. Called
// implicitly on scroll and explicitly on `p` from live.
func (a *App) freezeLogs() {
	if a.logBuffer == nil || a.logFrozen {
		return
	}
	a.logSnapshot = a.logBuffer.Snapshot(model.LogFilter{}, 0)
	a.logFrozenAt = a.logBuffer.WriteCount()
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
// Navigation keys auto-freeze from live mode so the list stops sliding.
// `f`, `home`, or `p` resume live follow.
func (a *App) handleLogsListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if cmd, ok := a.handleTabSwitch(key); ok {
		return a, cmd
	}

	switch key {
	case "f", "home":
		if a.logFrozen {
			a.resumeLogs()
			return a, nil
		}
	case "up", "down", "pgup", "pgdown", "end", "j", "k":
		if !a.logFrozen && a.logBuffer != nil && a.logBuffer.Len() > 0 {
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
		if a.logBuffer != nil {
			a.logBuffer.Clear()
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
