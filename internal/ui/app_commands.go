package ui

import (
	"context"
	"fmt"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/pkg/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

func (a *App) doFetch() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), globalFetchTimeout)
		defer cancel()
		snap, err := a.fetcher.Fetch(ctx)
		return fetchMsg{snap: snap, err: err}
	}
}

func (a *App) doRestart() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), globalFetchTimeout)
		defer cancel()
		if r, ok := a.fetcher.(restarter); ok {
			return restartResultMsg{err: r.RestartWorkers(ctx)}
		}
		return restartResultMsg{}
	}
}

func (a *App) doFetchConfig() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), globalFetchTimeout)
		defer cancel()
		if cf, ok := a.fetcher.(configFetcher); ok {
			raw, err := cf.FetchConfig(ctx)
			return configFetchMsg{raw: raw, err: err}
		}
		return configFetchMsg{err: fmt.Errorf("config inspection not supported")}
	}
}

func (a *App) doFetchCertificates() tea.Cmd {
	// capture hosts on the main goroutine to avoid a data race with Update().
	var hosts []string
	for _, hd := range a.state.HostDerived {
		hosts = append(hosts, hd.Host)
	}

	return func() tea.Msg {
		cf, ok := a.fetcher.(certFetcher)
		if !ok {
			return certFetchMsg{err: fmt.Errorf("certificate inspection not supported")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), globalFetchTimeout)
		defer cancel()
		all := make([]fetcher.CertificateInfo, 0)

		all = append(all, cf.FetchPKICertificates(ctx)...)

		if len(hosts) > 0 {
			tlsCerts := cf.DialTLSCertificates(ctx, hosts)
			all = append(all, tlsCerts...)
		}

		return certFetchMsg{certs: all}
	}
}

func (a *App) doFetchRPConfig() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), globalFetchTimeout)
		defer cancel()
		cf, ok := a.fetcher.(configFetcher)
		if !ok {
			return rpConfigFetchMsg{err: fmt.Errorf("config inspection not supported")}
		}
		raw, err := cf.FetchConfig(ctx)
		if err != nil {
			return rpConfigFetchMsg{err: err}
		}
		return rpConfigFetchMsg{configs: fetcher.ParseReverseProxyConfigs(raw)}
	}
}

func (a *App) doPluginFetches() []tea.Cmd {
	var cmds []tea.Cmd
	for i, g := range a.pluginGroups {
		if g.fetcher != nil && !g.fetching {
			g.fetching = true
			cmds = append(cmds, doPluginFetch(a.ctx, i, g.fetcher))
		}
	}
	return cmds
}

func (a *App) pluginExports() []plugin.PluginExport {
	var exports []plugin.PluginExport
	for _, g := range a.pluginGroups {
		if g.exporter != nil {
			exports = append(exports, plugin.PluginExport{
				Exporter: g.exporter,
				Data:     g.data,
			})
		}
	}
	return exports
}

func (a *App) notifyMetricsSubscribers(snap *fetcher.Snapshot) {
	for _, g := range a.pluginGroups {
		if sub, ok := g.p.(plugin.MetricsSubscriber); ok {
			plugin.SafeOnMetrics(sub, snap)
		}
	}
}
