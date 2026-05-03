package ui

import (
	"slices"

	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) switchTab(target tab) {
	ts := a.tabStates[a.activeTab]
	ts.cursor = a.cursor
	ts.filter = a.filter

	a.activeTab = target
	ts = a.tabStates[target]
	a.cursor = ts.cursor
	a.filter = ts.filter
	a.clampCursor()
	a.mode = viewList

	// Entering the Logs tab always lands on Runtime with the sidepanel
	// focused, so the user never has to guess where the cursor is after
	// coming back from another tab. Callers that want to land somewhere
	// else (e.g. the Caddy tab's `l` shortcut) override this right after.
	if target == tabLogs {
		a.logSel = logSel{kind: logSelRuntime}
		a.logSidepanelFocused = true
		a.resumeLogs()
	}
}

func (a *App) switchTabCmd() tea.Cmd {
	switch {
	case a.activeTab == tabConfig && a.configRoot == nil:
		return a.doFetchConfig()
	case a.activeTab == tabCertificates && a.certificates == nil:
		return a.doFetchCertificates()
	case a.activeTab == tabUpstreams && a.rpConfigs == nil:
		return a.doFetchRPConfig()
	}
	return nil
}

func (a *App) nextTab() {
	for i, t := range a.tabs {
		if t == a.activeTab {
			a.switchTab(a.tabs[(i+1)%len(a.tabs)])
			return
		}
	}
}

func (a *App) prevTab() {
	for i, t := range a.tabs {
		if t == a.activeTab {
			a.switchTab(a.tabs[(i-1+len(a.tabs))%len(a.tabs)])
			return
		}
	}
}

func (a *App) enableFrankenPHP() {
	a.hasFrankenPHP = true
	newTabs := make([]tab, 0, len(a.tabs)+1)
	for _, t := range a.tabs {
		newTabs = append(newTabs, t)
		if t == tabCaddy {
			newTabs = append(newTabs, tabFrankenPHP)
		}
	}
	a.tabs = newTabs
	a.tabStates[tabFrankenPHP] = &tabState{}
}

func (a *App) enableUpstreams() {
	a.hasUpstreams = true
	after := tabCaddy
	if a.hasFrankenPHP {
		after = tabFrankenPHP
	}
	newTabs := make([]tab, 0, len(a.tabs)+1)
	for _, t := range a.tabs {
		newTabs = append(newTabs, t)
		if t == after {
			newTabs = append(newTabs, tabUpstreams)
		}
	}
	a.tabs = newTabs
	a.tabStates[tabUpstreams] = &tabState{}
}

func (a *App) activePluginTab() *pluginTab {
	for _, pt := range a.pluginTabs {
		if pt.tabID == a.activeTab {
			return pt
		}
	}
	return nil
}

func (a *App) updatePluginTabVisibility(g *pluginGroup, visible bool) {
	for _, pt := range a.pluginTabs {
		if pt.group == g {
			a.updateSingleTabVisibility(pt, visible)
		}
	}
}

func (a *App) updateSingleTabVisibility(pt *pluginTab, visible bool) {
	if visible {
		if !slices.Contains(a.tabs, pt.tabID) {
			inserted := false
			for i, t := range a.tabs {
				if t > pt.tabID {
					a.tabs = slices.Insert(a.tabs, i, pt.tabID)
					inserted = true
					break
				}
			}
			if !inserted {
				a.tabs = append(a.tabs, pt.tabID)
			}
			a.tabStates[pt.tabID] = &tabState{}
		}
	} else {
		idx := slices.Index(a.tabs, pt.tabID)
		if idx >= 0 {
			if a.activeTab == pt.tabID {
				a.switchTab(a.tabs[0])
			}
			a.tabs = slices.Delete(a.tabs, idx, idx+1)
			delete(a.tabStates, pt.tabID)
		}
	}
}
