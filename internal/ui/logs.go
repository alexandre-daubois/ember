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

	rightStatus := a.buildLogsHeaderStatus()

	var table string
	if a.isRoutesView() {
		table = a.renderRoutesView(tableW, height, rightStatus)
	} else {
		all := a.filteredLogEntries()
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
		if a.logSel.kind == logSelRuntime {
			table = renderRuntimeLogTable(visible, localCursor, tableW, height, rightStatus, emptyHint)
		} else {
			hideHost := a.logSel.kind == logSelAccessHost
			table = renderLogTable(visible, localCursor, tableW, height, rightStatus, emptyHint, hideHost)
		}
	}

	selIdx := sidepanelIndex(items, a.logSel)
	sidepanel := renderSidepanel(items, selIdx, a.logSidepanelFocused, sidepanelW, height)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidepanel, table)
}

// renderRoutesView aggregates access logs into per-route stats and slices
// them through the cursor/scroll state shared with the log views. The host
// column is folded into Pattern as a soft prefix at the root view (showHost)
// so two hosts on the same path stay distinguishable; per-host drill-downs
// drop it because the sidepanel already encodes the scope.
func (a *App) renderRoutesView(width, height int, rightStatus string) string {
	stats := a.currentRouteStats()
	bodyHeight := height - logHeaderHeight
	if bodyHeight < 1 {
		bodyHeight = 1
	}
	visible, localCursor := a.sliceRoutesViewport(stats, bodyHeight)
	if a.logSidepanelFocused {
		localCursor = -1
	}
	hint := a.routesEmptyHint(len(visible))
	showHost := a.logSel.kind == logSelRoutes
	return renderRoutesTable(visible, localCursor, width, height, a.routeSortBy, showHost, rightStatus, hint)
}

func (a *App) isRoutesView() bool {
	return a.logSel.kind == logSelRoutes || a.logSel.kind == logSelRoutesHost
}

// currentRouteStats returns the route table the UI should display. The
// aggregator counts every access log Ember has seen this session, so the
// numbers are not capped by the access buffer's ring capacity — that
// matters as soon as a busy server wraps the 10 000-entry buffer.
func (a *App) currentRouteStats() []model.RouteStat {
	if a.routeAggregator == nil {
		return nil
	}
	stats := a.routeAggregator.Snapshot()
	if a.logSel.kind == logSelRoutesHost {
		stats = filterRouteStatsByHost(stats, a.logSel.host)
	}
	if a.filter != "" {
		stats = filterRouteStats(stats, a.filter)
	}
	model.SortRoutes(stats, a.routeSortBy)
	return stats
}

// filterRouteStatsByHost reuses the slice header in place: the snapshot
// from the aggregator is already a fresh copy the caller owns.
func filterRouteStatsByHost(stats []model.RouteStat, host string) []model.RouteStat {
	out := stats[:0]
	for _, s := range stats {
		if s.Key.Host == host {
			out = append(out, s)
		}
	}
	return out
}

// filterRouteStats matches against host, method, or normalized pattern
// (what the user sees on the root view, where the host is folded into the
// Pattern cell as a soft prefix), not the underlying URIs.
func filterRouteStats(stats []model.RouteStat, query string) []model.RouteStat {
	q := strings.ToLower(query)
	out := stats[:0]
	for _, s := range stats {
		if strings.Contains(strings.ToLower(s.Key.Host), q) ||
			strings.Contains(strings.ToLower(s.Key.Method), q) ||
			strings.Contains(strings.ToLower(s.Key.Pattern), q) {
			out = append(out, s)
		}
	}
	return out
}

// sliceRoutesViewport always honours a.cursor / a.logScrollOffset, regardless
// of freeze state — the By Route view is a scrollable list, not a tail, so
// the cursor must drive the viewport even in live mode (otherwise the user
// would press ↓ and watch nothing happen because the slice was clipped to
// the first bodyHeight rows).
func (a *App) sliceRoutesViewport(all []model.RouteStat, bodyHeight int) ([]model.RouteStat, int) {
	if bodyHeight <= 0 || len(all) == 0 {
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

// routesEmptyHint distinguishes "no traffic at all" from "filter matched
// nothing" so the user knows whether to wait or relax their query.
func (a *App) routesEmptyHint(visibleCount int) string {
	if visibleCount > 0 {
		return ""
	}
	totalBuckets := 0
	if a.routeAggregator != nil {
		totalBuckets = a.routeAggregator.BucketCount()
	}
	if a.filter != "" && totalBuckets > 0 {
		return "No matching routes (filter: " + a.filter + ")"
	}
	if totalBuckets == 0 {
		if a.logSource != "" {
			return "Listening on " + a.logSource + " — waiting for access logs (it can take up to 30s)..."
		}
		return "Waiting for access logs (it can take up to 30s for the first lines to appear)..."
	}
	return "No access logs to aggregate yet"
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
	// "dropped" and "PAUSED" only make sense for ring-buffered log views:
	// the By Route aggregator keeps counters for the whole session, so the
	// table is never lossy and never frozen — surfacing those indicators
	// here would just confuse users.
	if !a.isRoutesView() {
		if n := a.droppedLogCount(); n > 0 {
			parts = append(parts, warnStyle.Render(fmt.Sprintf("dropped: %d", n)))
		}
	}
	if a.filter != "" {
		parts = append(parts, helpStyle.Render("filter: "+a.filter))
	}
	if a.logFrozen && !a.isRoutesView() {
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
	// In live mode the log table pins the newest entry at row 0, so any
	// scroll offset is meaningless. The By Route view is different: it is
	// a scrollable list whose viewport must follow the cursor, frozen or
	// not, otherwise ↓ would silently no-op past the visible window.
	if !a.logFrozen && !a.isRoutesView() {
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
			if a.currentLogsListLen() == 0 {
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
		// Auto-freeze stops the *log tail* from sliding under the cursor.
		// The By Route view is an aggregation, not a tail: rows can shift
		// position when counts update but nothing is being scrolled past,
		// so freezing would only confuse the affordance.
		if !a.isRoutesView() && !a.logFrozen && a.currentLogBuffer() != nil && a.currentLogBuffer().Len() > 0 {
			a.freezeLogs()
		}
	case "s":
		if a.isRoutesView() {
			a.routeSortBy = a.routeSortBy.Next()
			return a, nil
		}
	case "S":
		if a.isRoutesView() {
			a.routeSortBy = a.routeSortBy.Prev()
			return a, nil
		}
	}

	total := a.currentLogsListLen()
	maxIdx := total - 1
	if maxIdx < 0 {
		maxIdx = 0
	}
	moveCursor(key, &a.cursor, maxIdx, a.pageSize())
	a.clampLogScrollOffset(total)

	switch key {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "/":
		a.mode = viewFilter
		a.filter = ""
	case "p":
		// Pause is meaningless in the By Route view (the aggregator never
		// loses data and nothing scrolls), so we skip the toggle there.
		if a.isRoutesView() {
			break
		}
		if a.logFrozen {
			a.resumeLogs()
		} else {
			a.freezeLogs()
		}
	case "c":
		if a.isRoutesView() {
			if a.routeAggregator != nil {
				a.routeAggregator.Reset()
				a.cursor = 0
				a.logScrollOffset = 0
				a.status = "route stats cleared"
			}
		} else if buf := a.currentLogBuffer(); buf != nil {
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

func (a *App) currentLogsListLen() int {
	if a.isRoutesView() {
		return len(a.currentRouteStats())
	}
	return len(a.filteredLogEntries())
}

// logsHelpBindings returns the bindings shown at the bottom of the Logs tab.
// routesView swaps in the s/S sort cue and drops pause/follow because the
// aggregated view is never lossy and never scrolls — there is nothing to
// freeze. The plain log views keep p/f because their ring buffer slides.
func logsHelpBindings(frozen, routesView bool, routeSort string) []binding {
	bindings := []binding{
		{"↑/↓", "navigate"},
		{"←/→", "panel"},
		{"/", "filter"},
	}
	if routesView {
		bindings = append(bindings, binding{"s/S", "sort(" + routeSort + ")"})
	} else {
		pauseLabel := "pause"
		if frozen {
			pauseLabel = "resume"
		}
		bindings = append(bindings, binding{"p", pauseLabel})
		if frozen {
			bindings = append(bindings, binding{"f", "follow"})
		}
	}
	bindings = append(bindings,
		binding{"c", "clear"},
		binding{"Tab/S-Tab", "switch"},
		binding{"q", "quit"},
	)
	return bindings
}
