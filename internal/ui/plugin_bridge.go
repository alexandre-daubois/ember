package ui

import (
	"context"
	"fmt"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/pkg/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

type pluginGroup struct {
	p           plugin.Plugin
	fetcher     plugin.Fetcher
	exporter    plugin.Exporter
	avail       plugin.Availability
	tabAvail    plugin.TabAvailability
	wasAvail    bool
	wasTabAvail map[string]bool
	data        any
	err         error
	fetching    bool
}

type pluginTab struct {
	group    *pluginGroup
	renderer plugin.Renderer
	tabID    tab
	tabKey   string
	tabName  string
}

func newPluginTabs(p plugin.Plugin, startID tab) ([]*pluginTab, *pluginGroup) {
	g := &pluginGroup{p: p, wasAvail: true}
	if f, ok := p.(plugin.Fetcher); ok {
		g.fetcher = f
	}
	if e, ok := p.(plugin.Exporter); ok {
		g.exporter = e
	}
	if a, ok := p.(plugin.Availability); ok {
		g.avail = a
	}

	var tabs []*pluginTab

	if mr, ok := p.(plugin.MultiRenderer); ok {
		if ta, ok := p.(plugin.TabAvailability); ok {
			g.tabAvail = ta
			g.wasTabAvail = make(map[string]bool)
		}
		for i, desc := range safePluginTabs(mr) {
			r := safePluginRendererForTab(mr, desc.Key)
			if r != nil {
				tabs = append(tabs, &pluginTab{
					group:    g,
					renderer: r,
					tabID:    startID + tab(i),
					tabKey:   desc.Key,
					tabName:  desc.Name,
				})
				if g.wasTabAvail != nil {
					g.wasTabAvail[desc.Key] = true
				}
			}
		}
	} else if r, ok := p.(plugin.Renderer); ok {
		tabs = append(tabs, &pluginTab{
			group:    g,
			renderer: r,
			tabID:    startID,
			tabName:  p.Name(),
		})
	}

	return tabs, g
}

type pluginFetchMsg struct {
	groupIndex int
	data       any
	err        error
}

func doPluginFetch(ctx context.Context, groupIndex int, f plugin.Fetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := plugin.SafeFetch(ctx, f)
		return pluginFetchMsg{groupIndex: groupIndex, data: data, err: err}
	}
}

func safePluginUpdate(r plugin.Renderer, data any, w, h int) (_ plugin.Renderer, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("plugin panic during Update: %v", rec)
		}
	}()
	return r.Update(data, w, h), nil
}

func safePluginView(r plugin.Renderer, w, h int) (s string) {
	defer func() {
		if rec := recover(); rec != nil {
			s = fmt.Sprintf("plugin error: %v", rec)
		}
	}()
	return r.View(w, h)
}

func safePluginHandleKey(r plugin.Renderer, msg tea.KeyMsg) (consumed bool, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("plugin panic during HandleKey: %v", rec)
		}
	}()
	return r.HandleKey(msg), nil
}

func safePluginStatusCount(r plugin.Renderer) (_ string, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("plugin panic during StatusCount: %v", rec)
		}
	}()
	return r.StatusCount(), nil
}

func safePluginHelpBindings(r plugin.Renderer) (_ []plugin.HelpBinding, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("plugin panic during HelpBindings: %v", rec)
		}
	}()
	return r.HelpBindings(), nil
}

func safeOnMetrics(sub plugin.MetricsSubscriber, snap *fetcher.Snapshot) {
	defer func() {
		recover() //nolint:errcheck // fire-and-forget: don't crash Ember if a subscriber panics
	}()
	sub.OnMetrics(snap)
}

func safePluginAvailable(a plugin.Availability) (avail bool) {
	defer func() {
		if rec := recover(); rec != nil {
			avail = true
		}
	}()
	return a.Available()
}

func safePluginTabAvailable(ta plugin.TabAvailability, key string) (avail bool) {
	defer func() {
		if rec := recover(); rec != nil {
			avail = true
		}
	}()
	return ta.TabAvailable(key)
}

// safePluginTabs recovers from panics in MultiRenderer.Tabs. A panicking
// plugin produces no tabs; other plugins keep running.
func safePluginTabs(mr plugin.MultiRenderer) (descs []plugin.TabDescriptor) {
	defer func() {
		if rec := recover(); rec != nil {
			descs = nil
		}
	}()
	return mr.Tabs()
}

// safePluginRendererForTab recovers from panics in MultiRenderer.RendererForTab.
// A panic for one key drops that tab; the other tabs of the same plugin keep
// working.
func safePluginRendererForTab(mr plugin.MultiRenderer, key string) (r plugin.Renderer) {
	defer func() {
		if rec := recover(); rec != nil {
			r = nil
		}
	}()
	return mr.RendererForTab(key)
}
