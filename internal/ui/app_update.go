package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	tea "github.com/charmbracelet/bubbletea"
)

type tickMsg struct{}
type fetchMsg struct {
	snap *fetcher.Snapshot
	err  error
}
type restartResultMsg struct{ err error }
type metricsServerErrMsg struct{ err error }
type configFetchMsg struct {
	raw json.RawMessage
	err error
}
type certFetchMsg struct {
	certs []fetcher.CertificateInfo
	err   error
}
type rpConfigFetchMsg struct {
	configs []fetcher.ReverseProxyConfig
	err     error
}

// logRefreshMsg drives the Logs tab redraw cadence; the tailer writes into
// the shared LogBuffer asynchronously and the UI re-snapshots on each tick.
type logRefreshMsg struct{}

const logRefreshInterval = 250 * time.Millisecond

func (a *App) scheduleLogRefresh() tea.Cmd {
	return tea.Tick(logRefreshInterval, func(time.Time) tea.Msg {
		return logRefreshMsg{}
	})
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a.handleKey(msg)
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return a, tea.ClearScreen
	case metricsServerErrMsg:
		a.status = "⚠ " + msg.err.Error()
		return a, nil
	case pluginFetchMsg:
		if msg.groupIndex >= 0 && msg.groupIndex < len(a.pluginGroups) {
			g := a.pluginGroups[msg.groupIndex]
			g.fetching = false
			g.err = msg.err
			if msg.err == nil {
				g.data = msg.data

				if g.avail != nil {
					nowAvail := safePluginAvailable(g.avail)
					if nowAvail != g.wasAvail {
						g.wasAvail = nowAvail
						a.updatePluginTabVisibility(g, nowAvail)
						if nowAvail && g.wasTabAvail != nil {
							for k := range g.wasTabAvail {
								g.wasTabAvail[k] = true
							}
						}
					}
				}

				if g.wasAvail && g.tabAvail != nil {
					for _, pt := range a.pluginTabs {
						if pt.group != g || pt.tabKey == "" {
							continue
						}
						nowAvail := safePluginTabAvailable(g.tabAvail, pt.tabKey)
						if nowAvail != g.wasTabAvail[pt.tabKey] {
							g.wasTabAvail[pt.tabKey] = nowAvail
							a.updateSingleTabVisibility(pt, nowAvail)
						}
					}
				}

				for _, pt := range a.pluginTabs {
					if pt.group == g && pt.renderer != nil && msg.data != nil {
						updated, updateErr := safePluginUpdate(pt.renderer, msg.data, a.width, a.height)
						if updateErr == nil && updated != nil {
							pt.renderer = updated
						} else if updateErr != nil {
							g.err = updateErr
						}
					}
				}
			}
		} else {
			a.status = fmt.Sprintf("⚠ plugin fetch: unexpected index %d", msg.groupIndex)
		}
		return a, nil
	case tickMsg:
		if a.paused || a.fetching {
			return a, a.doTick()
		}
		a.fetching = true
		cmds := []tea.Cmd{a.doFetch(), a.doTick()}
		cmds = append(cmds, a.doPluginFetches()...)
		return a, tea.Batch(cmds...)
	case fetchMsg:
		a.fetching = false
		a.viewTime = time.Now()
		a.err = msg.err
		if a.history == nil {
			a.history = newHistoryStore()
		}
		var rpCmd tea.Cmd
		if msg.snap != nil {
			if msg.snap.HasFrankenPHP && !a.hasFrankenPHP {
				a.enableFrankenPHP()
			}
			if len(msg.snap.Metrics.Upstreams) > 0 && !a.hasUpstreams {
				a.enableUpstreams()
				rpCmd = a.doFetchRPConfig()
			}

			wasStale := a.stale
			hasData := len(msg.snap.Threads.ThreadDebugStates) > 0 || msg.snap.Metrics.HasHTTPMetrics || len(msg.snap.Metrics.Upstreams) > 0
			hadData := a.state.Current != nil && (len(a.state.Current.Threads.ThreadDebugStates) > 0 || a.state.Current.Metrics.HasHTTPMetrics || len(a.state.Current.Metrics.Upstreams) > 0)

			if !hasData && hadData {
				a.stale = true
				a.state.Current.Process = msg.snap.Process
				a.state.Current.Metrics = msg.snap.Metrics
				a.state.Current.FetchedAt = msg.snap.FetchedAt
				a.history.appendCPU(msg.snap.Process.CPUPercent)
				staleDur := time.Since(a.lastFresh).Truncate(time.Second)
				if msg.snap.Process.CPUPercent >= 80 {
					a.status = fmt.Sprintf("⚠ High load — data stale %s", staleDur)
				} else {
					a.status = fmt.Sprintf("⚠ Connection lost — data stale %s", staleDur)
				}
				return a, nil
			}

			a.stale = false
			a.lastFresh = time.Now()
			a.state.Update(msg.snap)
			if wasStale {
				a.state.Derived.RPS = 0
				a.state.Derived.AvgTime = 0
				a.state.Derived.HasPercentiles = false
				a.state.ResetPercentiles()
			}
			a.clampCursor()
			if len(msg.snap.Errors) > 0 {
				a.status = "⚠ " + strings.Join(msg.snap.Errors, " | ")
			} else {
				a.status = ""
			}
			a.history.appendRPS(a.state.Derived.RPS)
			a.history.appendCPU(msg.snap.Process.CPUPercent)
			a.history.appendRSS(float64(msg.snap.Process.RSS) / 1024 / 1024)
			a.history.appendQueue(msg.snap.Metrics.QueueDepth)
			a.history.appendBusy(float64(a.state.Derived.TotalBusy))

			activeHosts := make(map[string]struct{}, len(a.state.HostDerived))
			for _, hd := range a.state.HostDerived {
				a.history.appendHostRPS(hd.Host, hd.RPS)
				activeHosts[hd.Host] = struct{}{}
			}
			a.history.pruneHosts(activeHosts)

			for _, t := range msg.snap.Threads.ThreadDebugStates {
				a.history.recordMem(t.Index, t.MemoryUsage)
			}
			activeThreads := make(map[int]struct{}, len(msg.snap.Threads.ThreadDebugStates))
			for _, t := range msg.snap.Threads.ThreadDebugStates {
				activeThreads[t.Index] = struct{}{}
			}
			a.history.pruneMem(activeThreads)

			a.updateDownSince()
			a.notifyMetricsSubscribers(msg.snap)

			if a.config.OnStateUpdate != nil {
				a.config.OnStateUpdate(a.state, a.pluginExports())
			}
		}
		return a, rpCmd
	case restartResultMsg:
		if msg.err != nil {
			a.status = "restart failed: " + msg.err.Error()
		} else {
			a.status = "workers restarted"
		}
		return a, nil
	case configFetchMsg:
		if msg.err != nil {
			a.status = "config fetch failed: " + msg.err.Error()
			return a, nil
		}
		root, err := parseJSONTree(msg.raw)
		if err != nil {
			a.status = "config parse failed: " + err.Error()
			return a, nil
		}
		expandAll(root)
		a.configRoot = root
		a.configCursor = 0
		a.configFilter = ""
		a.configFilterMode = false
		return a, nil
	case certFetchMsg:
		if msg.err != nil {
			a.status = "cert fetch failed: " + msg.err.Error()
			return a, nil
		}
		a.certificates = msg.certs
		a.tabStates[tabCertificates].cursor = 0
		if a.activeTab == tabCertificates {
			a.cursor = 0
		}
		if warn := certExpiryWarning(msg.certs); warn != "" {
			a.status = warn
		}
		return a, nil
	case rpConfigFetchMsg:
		if msg.err != nil {
			a.status = "upstream config fetch failed: " + msg.err.Error()
			return a, nil
		}
		a.rpConfigs = msg.configs
		return a, nil
	case logRefreshMsg:
		return a, a.scheduleLogRefresh()
	}
	return a, nil
}

func (a *App) doTick() tea.Cmd {
	return tea.Tick(a.config.Interval, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}
