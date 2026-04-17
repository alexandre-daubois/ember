package ui

import (
	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
)

// renderLogsTab is the View() entry point for the Logs tab. It snapshots the
// current LogBuffer through the active filter and feeds it to renderLogTable.
// When paused, the view is rendered from a frozen snapshot taken at the
// moment of pause; filter changes still take effect against that snapshot so
// the user can slice the captured window freely.
func (a *App) renderLogsTab(width int) string {
	if a.logBuffer == nil {
		return greyStyle.Render(" Logs unavailable: Caddy is not local and --log-listen was not set.\n" +
			" Pass --log-listen :PORT and make sure Caddy can reach this address. See docs/logs.md.")
	}

	height := a.pageSize()
	filter := a.activeLogFilter()

	var entries []fetcher.LogEntry
	var sourceLen int
	if a.logPaused {
		entries = filterEntriesWithLimit(a.logPausedSnapshot, filter, height)
		sourceLen = len(a.logPausedSnapshot)
	} else {
		entries = a.logBuffer.Snapshot(filter, height)
		sourceLen = a.logBuffer.Len()
	}

	filterLabel := a.logFilterLabel()
	banner := buildLogsBanner(filterLabel, a.logPaused)

	emptyHint := "Waiting for log lines (it can take up to 30s for the first lines to appear)..."
	if a.logSource != "" {
		emptyHint = "Listening on " + a.logSource + " — waiting for log lines (it can take up to 30s)..."
	}
	if filterLabel != "" && sourceLen > 0 && len(entries) == 0 {
		emptyHint = "No matching log lines (filter: " + filterLabel + ")"
	}

	return renderLogTable(entries, a.cursor, width, height, banner != "", banner, emptyHint)
}

// filterEntriesWithLimit applies a LogFilter to a pre-captured slice and
// returns at most limit matching entries, preserving order. Used to re-filter
// the paused snapshot without taking a fresh buffer snapshot.
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

// buildLogsBanner assembles the filter/paused indicator shown above the log
// table. Pause is surfaced as its own "PAUSED" prefix rather than mixed into
// the filter descriptor.
func buildLogsBanner(filterLabel string, paused bool) string {
	switch {
	case paused && filterLabel != "":
		return " PAUSED · filter: " + filterLabel
	case paused:
		return " PAUSED"
	case filterLabel != "":
		return " filter: " + filterLabel
	default:
		return ""
	}
}

func (a *App) activeLogFilter() model.LogFilter {
	return model.LogFilter{Search: a.filter}
}

func (a *App) logFilterLabel() string {
	return a.filter
}

// handleLogsListKey processes keystrokes when the Logs tab is in list mode.
func (a *App) handleLogsListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if cmd, ok := a.handleTabSwitch(key); ok {
		return a, cmd
	}

	maxIdx := a.logVisibleLen() - 1
	if maxIdx < 0 {
		maxIdx = 0
	}
	moveCursor(key, &a.cursor, maxIdx, a.pageSize())

	switch key {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "/":
		a.mode = viewFilter
		a.filter = ""
	case "p":
		if !a.logPaused && a.logBuffer != nil {
			// Cache the full, unfiltered contents so the user can change the
			// filter (status band, search) after pausing and see the frozen
			// window re-sliced rather than a flash of live data.
			a.logPausedSnapshot = a.logBuffer.Snapshot(model.LogFilter{}, 0)
		} else {
			a.logPausedSnapshot = nil
		}
		a.logPaused = !a.logPaused
	case "c":
		if a.logBuffer != nil {
			a.logBuffer.Clear()
			a.logPausedSnapshot = nil
			a.cursor = 0
			a.status = "log buffer cleared"
		}
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
	}
	return a, nil
}

// logVisibleLen returns the number of entries currently shown so the cursor
// can be clamped. It mirrors the source selection in renderLogsTab so the
// cursor stays within the rendered rows.
func (a *App) logVisibleLen() int {
	if a.logBuffer == nil {
		return 0
	}
	height := a.pageSize()
	filter := a.activeLogFilter()
	if a.logPaused {
		return len(filterEntriesWithLimit(a.logPausedSnapshot, filter, height))
	}
	return len(a.logBuffer.Snapshot(filter, height))
}

// logsHelpBindings returns the bindings shown at the bottom of the Logs tab.
func logsHelpBindings(paused bool) []binding {
	pauseLabel := "pause"
	if paused {
		pauseLabel = "resume"
	}
	return []binding{
		{"↑/↓", "navigate"},
		{"/", "filter"},
		{"p", pauseLabel},
		{"c", "clear"},
		{"Tab/S-Tab", "switch"},
		{"q", "quit"},
	}
}
