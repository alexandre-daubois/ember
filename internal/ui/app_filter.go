package ui

import (
	"strings"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
)

func (a *App) filteredThreads() []fetcher.ThreadDebugState {
	if a.state.Current == nil {
		return nil
	}
	threads := sortThreads(a.state.Current.Threads.ThreadDebugStates, a.sortBy, a.viewTime)
	if a.filter == "" {
		return threads
	}
	f := strings.ToLower(a.filter)
	var result []fetcher.ThreadDebugState
	for _, t := range threads {
		if strings.Contains(strings.ToLower(t.Name), f) ||
			strings.Contains(strings.ToLower(t.State), f) ||
			strings.Contains(strings.ToLower(t.CurrentMethod), f) ||
			strings.Contains(strings.ToLower(t.CurrentURI), f) {
			result = append(result, t)
		}
	}
	return result
}

func (a *App) filteredHosts() []model.HostDerived {
	hosts := sortHosts(a.state.HostDerived, a.hostSortBy)
	if a.filter == "" {
		return hosts
	}
	f := strings.ToLower(a.filter)
	var result []model.HostDerived
	for _, h := range hosts {
		if strings.Contains(strings.ToLower(h.Host), f) {
			result = append(result, h)
		}
	}
	return result
}

func (a *App) filteredUpstreams() []model.UpstreamDerived {
	upstreams := sortUpstreams(a.state.UpstreamDerived, a.upstreamSortBy)
	if a.filter == "" {
		return upstreams
	}
	f := strings.ToLower(a.filter)
	var result []model.UpstreamDerived
	for _, u := range upstreams {
		if strings.Contains(strings.ToLower(u.Address), f) ||
			strings.Contains(strings.ToLower(u.Handler), f) {
			result = append(result, u)
		}
	}
	return result
}

func (a *App) pageSize() int {
	ps := a.height - 10
	if ps < 1 {
		ps = 1
	}
	return ps
}

func (a *App) listLen() int {
	switch a.activeTab {
	case tabConfig:
		return len(flattenVisible(a.configRoot))
	case tabCertificates:
		return len(a.filteredCerts())
	case tabCaddy:
		return len(a.filteredHosts())
	case tabUpstreams:
		return len(a.filteredUpstreams())
	case tabFrankenPHP:
		return len(a.filteredThreads())
	default:
		return 0
	}
}

func (a *App) clampCursor() {
	if a.activeTab == tabConfig || a.activeTab == tabLogs {
		return
	}
	var count int
	switch a.activeTab {
	case tabCertificates:
		count = len(a.filteredCerts())
	case tabCaddy:
		count = len(a.filteredHosts())
	case tabUpstreams:
		count = len(a.filteredUpstreams())
	case tabFrankenPHP:
		count = len(a.filteredThreads())
	default:
		a.cursor = 0
		return
	}
	maximum := count - 1
	if maximum < 0 {
		maximum = 0
	}
	if a.cursor > maximum {
		a.cursor = maximum
	}
}

func (a *App) prevThreadMemory() map[int]int64 {
	if a.state.Previous == nil {
		return nil
	}
	m := make(map[int]int64, len(a.state.Previous.Threads.ThreadDebugStates))
	for _, t := range a.state.Previous.Threads.ThreadDebugStates {
		m[t.Index] = t.MemoryUsage
	}
	return m
}

// upstreamKey builds a stable identifier matching the one used by the Prometheus
// parser: just the address when the handler label is absent (current Caddy behavior),
// or address/handler when present. Using the same key here keeps downSince tracking
// in sync with multi-handler configurations exporting the same upstream twice.
func upstreamKey(ud model.UpstreamDerived) string {
	if ud.Handler == "" {
		return ud.Address
	}
	return ud.Address + "/" + ud.Handler
}

func (a *App) updateDownSince() {
	now := a.viewTime

	active := make(map[string]struct{})
	for _, ud := range a.state.UpstreamDerived {
		key := upstreamKey(ud)
		active[key] = struct{}{}
		if !ud.Healthy {
			if _, tracked := a.downSince[key]; !tracked {
				a.downSince[key] = now
			}
		} else {
			delete(a.downSince, key)
		}
	}

	for key := range a.downSince {
		if _, ok := active[key]; !ok {
			delete(a.downSince, key)
		}
	}
}
