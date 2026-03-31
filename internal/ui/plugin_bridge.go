package ui

import (
	"context"
	"fmt"

	"github.com/alexandre-daubois/ember/pkg/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

type pluginTab struct {
	p        plugin.Plugin
	fetcher  plugin.Fetcher
	renderer plugin.Renderer
	exporter plugin.Exporter
	data     any
	err      error
	tabID    tab
	fetching bool
}

func newPluginTab(p plugin.Plugin, id tab) *pluginTab {
	pt := &pluginTab{p: p, tabID: id}
	if f, ok := p.(plugin.Fetcher); ok {
		pt.fetcher = f
	}
	if r, ok := p.(plugin.Renderer); ok {
		pt.renderer = r
	}
	if e, ok := p.(plugin.Exporter); ok {
		pt.exporter = e
	}
	return pt
}

type pluginFetchMsg struct {
	index int
	data  any
	err   error
}

func doPluginFetch(ctx context.Context, index int, f plugin.Fetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := plugin.SafeFetch(ctx, f)
		return pluginFetchMsg{index: index, data: data, err: err}
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
