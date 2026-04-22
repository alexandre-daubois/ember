package ui

import (
	"strings"

	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/charmbracelet/lipgloss"
)

// logSelKind identifies which slice of the log view the sidepanel is pointing
// at. Access-per-host is a separate kind because the table layout depends on
// whether a specific host is selected (the Host column is dropped).
type logSelKind int

const (
	logSelAccess logSelKind = iota
	logSelAccessHost
	logSelRuntime
)

// logSel is the persistent sidepanel selection. We persist kind+host rather
// than an index so the selection survives list rebuilds (hosts appear or
// disappear as traffic comes in), and cursors stay anchored on the same
// logical entry across refreshes.
type logSel struct {
	kind logSelKind
	host string
}

// sidepanelItem is one visible row in the sidepanel tree. `indent` is 0 for
// top-level entries ("Runtime", "Access") and 1 for host children so the
// renderer can draw the tree without caring about the logical structure.
type sidepanelItem struct {
	kind   logSelKind
	label  string
	host   string
	indent int
}

// sidepanelFixedWidth is the total column width reserved for the sidepanel,
// including the right border drawn by the renderer. Wide enough for
// "Access" and short hostnames; long hostnames are truncated with an ellipsis.
// Rendered at every width so the user always has a way to switch scopes.
const sidepanelFixedWidth = 22

var (
	sidepanelBorderStyle = lipgloss.NewStyle().
				BorderRight(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(subtle)

	// Both selected styles use Reverse so the selection stays visible in
	// NO_COLOR mode (reverse is a terminal attribute, not a colour). The
	// focused variant adds Bold to give a subtle hint that keyboard input
	// now drives the sidepanel.
	sidepanelSelectedStyle = lipgloss.NewStyle().Reverse(true)

	sidepanelFocusedSelectedStyle = lipgloss.NewStyle().
					Reverse(true).
					Bold(true)
)

// sidepanelItems returns the current list of sidepanel rows. The host list
// is derived from the access buffer so it grows as traffic comes in; sorting
// alphabetically keeps the order stable across refreshes.
func (a *App) sidepanelItems() []sidepanelItem {
	items := []sidepanelItem{
		{kind: logSelRuntime, label: "Runtime"},
		{kind: logSelAccess, label: "Access"},
	}
	for _, h := range accessLogHosts(a.logBuffer) {
		items = append(items, sidepanelItem{kind: logSelAccessHost, label: h, host: h, indent: 1})
	}
	return items
}

// accessLogHosts returns the unique set of hosts seen in the access buffer,
// sorted alphabetically. Delegates to LogBuffer.UniqueHosts so the walk
// happens under the buffer's read lock without copying all entries.
func accessLogHosts(buf *model.LogBuffer) []string {
	if buf == nil {
		return nil
	}
	return buf.UniqueHosts()
}

// sidepanelIndex finds the row index matching the active selection. Returns
// 0 when the selection is stale (e.g. a selected host disappeared after a
// buffer clear): the caller is expected to repair sel accordingly.
func sidepanelIndex(items []sidepanelItem, sel logSel) int {
	for i, item := range items {
		if item.kind != sel.kind {
			continue
		}
		if sel.kind == logSelAccessHost && item.host != sel.host {
			continue
		}
		return i
	}
	return 0
}

// normalizeLogSel repairs a selection that no longer exists in the item
// list, falling back to the Access aggregate. Called before every render
// and navigation so downstream code can assume a valid selection.
func normalizeLogSel(items []sidepanelItem, sel logSel) logSel {
	for _, item := range items {
		if item.kind != sel.kind {
			continue
		}
		if sel.kind == logSelAccessHost && item.host != sel.host {
			continue
		}
		return sel
	}
	return logSel{kind: logSelAccess}
}

// moveSidepanel walks the sidepanel list by delta rows and updates the
// persisted selection. `delta > 0` goes down. Clamped at both ends.
//
// Entering the sidepanel (via ←/h) already resumes live follow, so the
// frozen state is always cleared by the time we get here: resumeLogs at
// the end is defence-in-depth, a no-op in the common case.
func (a *App) moveSidepanel(delta int) {
	items := a.sidepanelItems()
	if len(items) == 0 {
		return
	}
	a.logSel = normalizeLogSel(items, a.logSel)
	idx := sidepanelIndex(items, a.logSel) + delta
	if idx < 0 {
		idx = 0
	}
	if idx >= len(items) {
		idx = len(items) - 1
	}
	item := items[idx]
	a.logSel = logSel{kind: item.kind, host: item.host}
	a.resumeLogs()
}

// selectHost moves the sidepanel selection onto the given host, or to the
// Access aggregate when the host is not currently in the buffer. Used by the
// Caddy tab's `l` shortcut to drill into a host's access logs.
func (a *App) selectHost(host string) {
	if host == "" {
		a.logSel = logSel{kind: logSelAccess}
		return
	}
	items := a.sidepanelItems()
	for _, item := range items {
		if item.kind == logSelAccessHost && item.host == host {
			a.logSel = logSel{kind: logSelAccessHost, host: host}
			return
		}
	}
	// Host not seen yet (no traffic in buffer): remember the intent anyway
	// so the selection sticks once Caddy starts emitting logs for that host.
	a.logSel = logSel{kind: logSelAccessHost, host: host}
}

// renderSidepanel draws the tree column. `focused` controls the selection
// highlight: a reverse-video bar when the sidepanel owns keyboard focus,
// a coloured label otherwise (so the user can still see where they left off
// when focus is on the table).
func renderSidepanel(items []sidepanelItem, selectedIdx int, focused bool, width, height int) string {
	innerWidth := width - 1 // one column for the right border
	if innerWidth < 4 {
		innerWidth = 4
	}

	lines := make([]string, 0, height)
	// Blank row to align the first sidepanel entry with the table's column
	// labels, and a separator row matching the table header's bottom border.
	lines = append(lines, strings.Repeat(" ", innerWidth))
	lines = append(lines, strings.Repeat(" ", innerWidth))

	for i, item := range items {
		// Prefix mirrors the ">" used by the log table rows so NO_COLOR
		// users have a redundant textual cue even when reverse video is
		// attenuated or stripped.
		prefix := " "
		if i == selectedIdx {
			prefix = ">"
		}
		label := prefix + strings.Repeat("  ", item.indent) + item.label
		label = padOrTrunc(label, innerWidth)
		if i == selectedIdx {
			if focused {
				lines = append(lines, sidepanelFocusedSelectedStyle.Render(label))
			} else {
				lines = append(lines, sidepanelSelectedStyle.Render(label))
			}
		} else {
			lines = append(lines, label)
		}
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", innerWidth))
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	body := strings.Join(lines, "\n")
	return sidepanelBorderStyle.Width(innerWidth).Render(body)
}

// padOrTrunc clamps a string to exactly `width` display cells, appending
// spaces or truncating with a trailing ellipsis as needed. Used for both
// header and row labels so the column edges align and the border renders
// cleanly at a fixed width.
func padOrTrunc(s string, width int) string {
	w := lipgloss.Width(s)
	if w == width {
		return s
	}
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	if width <= 1 {
		return strings.Repeat(" ", width)
	}
	return lipgloss.NewStyle().MaxWidth(width-1).Render(s) + "…"
}

// currentLogBuffer returns the buffer backing the active selection. Runtime
// entries live in a separate buffer so a busy server's access log volume
// cannot evict rare startup/TLS/reload lines.
func (a *App) currentLogBuffer() *model.LogBuffer {
	if a.logSel.kind == logSelRuntime {
		return a.runtimeLogBuffer
	}
	return a.logBuffer
}

// logBufferStats returns (len, full) in one call so the tab bar counter can
// tell a naturally-small buffer apart from a wrapped-and-full one when
// totaling access + runtime entries.
func logBufferStats(buf *model.LogBuffer) (int, bool) {
	if buf == nil {
		return 0, false
	}
	return buf.Len(), buf.Full()
}
