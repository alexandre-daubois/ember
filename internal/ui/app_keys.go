package ui

import (
	"strconv"

	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) handleTabSwitch(key string) (tea.Cmd, bool) {
	switch key {
	case "tab":
		a.nextTab()
		return a.switchTabCmd(), true
	case "shift+tab":
		a.prevTab()
		return a.switchTabCmd(), true
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx, _ := strconv.Atoi(key)
		if idx >= 1 && idx <= len(a.tabs) {
			a.switchTab(a.tabs[idx-1])
		}
		return a.switchTabCmd(), true
	case "t":
		// vim-style tab-select prefix on non-plugin tabs. The plugin-tab
		// path lives in handleListKey's switch where the plugin gets a
		// right-of-refusal first.
		a.pendingTabSelect = true
		return nil, true
	}
	return nil, false
}

func moveCursor(key string, cursor *int, maxIdx, pgSize int) {
	switch key {
	case "up", "k":
		if *cursor > 0 {
			*cursor--
		}
	case "down", "j":
		(*cursor)++
		if *cursor > maxIdx {
			*cursor = maxIdx
		}
	case "home":
		*cursor = 0
	case "end":
		*cursor = maxIdx
	case "pgup":
		*cursor -= pgSize
		if *cursor < 0 {
			*cursor = 0
		}
	case "pgdown":
		*cursor += pgSize
		if *cursor > maxIdx {
			*cursor = maxIdx
		}
	}
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch a.mode {
	case viewFilter:
		return a.handleFilterKey(msg)
	case viewDetail:
		return a.handleDetailKey(msg)
	case viewConfirmRestart:
		return a.handleConfirmRestartKey(msg)
	case viewGraph:
		return a.handleGraphKey(msg)
	case viewHelp:
		return a.handleHelpKey(msg)
	default:
		return a.handleListKey(msg)
	}
}

func (a *App) handleGraphKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "g":
		a.mode = a.prevMode
	case "q", "ctrl+c":
		return a, tea.Quit
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
	}
	return a, nil
}

func (a *App) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "?", "esc":
		a.mode = a.prevMode
	case "q", "ctrl+c":
		return a, tea.Quit
	}
	return a, nil
}

func (a *App) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Tab-select mode: the user pressed `t`, the next digit selects a tab.
	// Esc cancels, any other key cancels and falls through to normal routing.
	if a.pendingTabSelect {
		a.pendingTabSelect = false
		s := msg.String()
		if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
			idx := int(s[0] - '0')
			if idx >= 1 && idx <= len(a.tabs) {
				a.switchTab(a.tabs[idx-1])
				return a, a.switchTabCmd()
			}
			return a, nil
		}
		if s == "esc" {
			return a, nil
		}
		// other keys: fall through to normal handling below
	}

	if a.activeTab == tabConfig {
		return a.handleConfigListKey(msg)
	}
	if a.activeTab == tabCertificates {
		return a.handleCertListKey(msg)
	}
	if a.activeTab == tabUpstreams {
		return a.handleUpstreamListKey(msg)
	}
	if a.activeTab == tabLogs {
		return a.handleLogsListKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "tab":
		a.nextTab()
		return a, a.switchTabCmd()
	case "shift+tab":
		a.prevTab()
		return a, a.switchTabCmd()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// If a plugin tab is active, offer digits to the plugin first
		// (right-of-refusal, same contract as `t`). When the plugin consumes
		// the digit, no tab switch happens. When it returns false (the default
		// for plugins without a digit handler), fall through to the normal
		// tab-switch behavior so the keybinding stays useful on plugin tabs.
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			if consumed, _ := safePluginHandleKey(pt.renderer, msg); consumed {
				return a, nil
			}
		}
		idx, _ := strconv.Atoi(msg.String())
		if idx >= 1 && idx <= len(a.tabs) {
			a.switchTab(a.tabs[idx-1])
		}
		return a, a.switchTabCmd()
	case "t":
		// vim-style tab-select prefix. On a plugin tab, give the plugin a
		// right-of-refusal first (in case it owns `t` as a hotkey). When the
		// plugin consumes `t`, no mode is entered.
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			if consumed, _ := safePluginHandleKey(pt.renderer, msg); consumed {
				return a, nil
			}
		}
		a.pendingTabSelect = true
		return a, nil
	case "up", "k":
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
		} else if a.cursor > 0 {
			a.cursor--
		}
	case "down", "j":
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
		} else {
			a.cursor++
			a.clampCursor()
		}
	case "home":
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
		} else {
			a.cursor = 0
		}
	case "end":
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
		} else {
			max := a.listLen() - 1
			if max < 0 {
				max = 0
			}
			a.cursor = max
		}
	case "pgup":
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
		} else {
			a.cursor -= a.pageSize()
			if a.cursor < 0 {
				a.cursor = 0
			}
		}
	case "pgdown":
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
		} else {
			a.cursor += a.pageSize()
			a.clampCursor()
		}
	case "s":
		switch a.activeTab {
		case tabCaddy:
			a.hostSortBy = a.hostSortBy.Next()
		case tabFrankenPHP:
			a.sortBy = a.sortBy.Next()
		default:
			if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
				safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
			}
		}
	case "S":
		switch a.activeTab {
		case tabCaddy:
			a.hostSortBy = a.hostSortBy.Prev()
		case tabFrankenPHP:
			a.sortBy = a.sortBy.Prev()
		default:
			if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
				safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
			}
		}
	case "p":
		a.paused = !a.paused
	case "enter":
		// Only open the detail view when there is a row to show: entering it on
		// an empty list changes nothing visible but traps the user in viewDetail
		// (they would have to press q twice to quit).
		switch {
		case a.activeTab == tabCaddy && len(a.filteredHosts()) > 0:
			a.mode = viewDetail
		case a.activeTab == tabFrankenPHP && len(a.filteredThreads()) > 0:
			a.mode = viewDetail
		default:
			if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
				safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
			}
		}
	case "r":
		if a.activeTab == tabFrankenPHP {
			a.mode = viewConfirmRestart
		} else if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
		}
	case "/":
		if a.activeTab == tabCaddy || a.activeTab == tabFrankenPHP {
			a.mode = viewFilter
			a.filter = ""
		} else if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
		}
	case "g":
		a.prevMode = a.mode
		a.mode = viewGraph
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
	case "l":
		if a.activeTab == tabCaddy && a.logBuffer != nil {
			// Capture the host name before switchTab, which overwrites
			// a.cursor with the Logs tab's saved cursor and would otherwise
			// make us index hosts[] with the wrong value.
			hosts := a.filteredHosts()
			var host string
			if a.cursor >= 0 && a.cursor < len(hosts) {
				host = hosts[a.cursor].Host
			}
			a.switchTab(tabLogs)
			if host != "" {
				a.selectHost(host)
				a.resumeLogs()
				a.cursor = 0
			}
		}
	default:
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			safePluginHandleKey(pt.renderer, msg) //nolint:errcheck // consumed status is informational
		}
	}
	return a, nil
}

func (a *App) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	maxIdx := a.listLen() - 1
	if maxIdx < 0 {
		maxIdx = 0
	}
	moveCursor(key, &a.cursor, maxIdx, a.pageSize())

	switch key {
	case "esc", "q":
		a.mode = viewList
	case "r":
		if a.activeTab == tabFrankenPHP {
			a.mode = viewConfirmRestart
		}
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
	}
	return a, nil
}

func (a *App) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		a.mode = viewList
		a.filter = ""
		a.logScrollOffset = 0
	case "enter":
		a.mode = viewList
		a.cursor = 0
		a.logScrollOffset = 0
	case "backspace":
		if len(a.filter) > 0 {
			a.filter = a.filter[:len(a.filter)-1]
			a.cursor = 0
			a.logScrollOffset = 0
		}
	default:
		if len(msg.String()) == 1 {
			a.filter += msg.String()
			a.cursor = 0
			a.logScrollOffset = 0
		}
	}
	return a, nil
}

func (a *App) handleConfirmRestartKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		a.mode = viewList
		a.status = "restarting workers..."
		return a, a.doRestart()
	default:
		a.mode = viewList
		a.status = ""
	}
	return a, nil
}
