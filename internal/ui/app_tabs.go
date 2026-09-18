package ui

import (
	"slices"

	"github.com/alexandre-daubois/ember/internal/fetcher"
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
		a.resumeMercure()
	}
}

func (a *App) switchTabCmd() tea.Cmd {
	var cmd tea.Cmd
	switch {
	case a.activeTab == tabConfig && a.configRoot == nil:
		cmd = a.doFetchConfig()
	case a.activeTab == tabCertificates && a.certificates == nil:
		cmd = a.doFetchCertificates()
	case a.activeTab == tabUpstreams && a.rpConfigs == nil:
		cmd = a.doFetchRPConfig()
	case a.activeTab == tabMercure && a.mercureConfigs == nil:
		cmd = a.doFetchMercureConfig()
	}
	if feed := a.mercureFeedCmd(); feed != nil {
		if cmd == nil {
			return feed
		}
		return tea.Batch(cmd, feed)
	}
	return cmd
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
	a.insertTab(tabFrankenPHP)
}

func (a *App) enableUpstreams() {
	a.hasUpstreams = true
	a.insertTab(tabUpstreams)
}

func (a *App) enableMercure() {
	a.hasMercure = true
	a.insertTab(tabMercure)
}

// Plugin tabs always trail the built-in ones.
func (a *App) insertTab(target tab) {
	if slices.Contains(a.tabs, target) {
		return
	}
	rank := slices.Index(coreTabOrder, target)
	at := len(a.tabs)
	for i, t := range a.tabs {
		if r := slices.Index(coreTabOrder, t); r < 0 || r > rank {
			at = i
			break
		}
	}
	a.tabs = slices.Insert(a.tabs, at, target)
	a.tabStates[target] = &tabState{}
}

// Without the `metrics` global option the Caddy tab of a hub-only Caddy only
// ever reads zero.
func (a *App) syncCaddyTab(snap *fetcher.Snapshot) {
	if !a.hasMercure {
		return
	}
	want := a.hasFrankenPHP || snap.Metrics.HasHTTPMetrics
	idx := slices.Index(a.tabs, tabCaddy)
	switch {
	case want && idx < 0:
		a.insertTab(tabCaddy)
	case !want && idx >= 0:
		if a.activeTab == tabCaddy {
			a.switchTab(tabMercure)
		}
		a.tabs = slices.Delete(a.tabs, idx, idx+1)
		delete(a.tabStates, tabCaddy)
	}
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
