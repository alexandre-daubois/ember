package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/alexandre-daubois/ember/pkg/plugin"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type viewMode int

const (
	viewList viewMode = iota
	viewDetail
	viewFilter
	viewConfirmRestart
	viewGraph
	viewHelp
)

const (
	sparklineSize      = 15
	graphHistorySize   = 300
	memHistorySize     = 60
	globalFetchTimeout = 10 * time.Second
)

type Config struct {
	Interval         time.Duration
	SlowThreshold    time.Duration
	NoColor          bool
	Version          string
	HasFrankenPHP    bool
	Plugins          []plugin.Plugin
	OnStateUpdate    func(model.State, []plugin.PluginExport)
	MetricsServerErr <-chan error
	LogBuffer        *model.LogBuffer
	RuntimeLogBuffer *model.LogBuffer
	RouteAggregator  *model.RouteAggregator
	LogSource        string // path or description; empty when no source is known
}

type tab int

const (
	tabCaddy tab = iota
	tabConfig
	tabCertificates
	tabUpstreams
	tabLogs
	tabFrankenPHP
)

type tabState struct {
	cursor int
	filter string
}

type restarter interface {
	RestartWorkers(ctx context.Context) error
}

type configFetcher interface {
	FetchConfig(ctx context.Context) (json.RawMessage, error)
}

type certFetcher interface {
	FetchPKICertificates(ctx context.Context) []fetcher.CertificateInfo
	DialTLSCertificates(ctx context.Context, hosts []string) []fetcher.CertificateInfo
}

type App struct {
	fetcher   fetcher.Fetcher
	config    Config
	state     model.State
	cursor    int
	sortBy    model.SortField
	paused    bool
	width     int
	height    int
	err       error
	mode      viewMode
	prevMode  viewMode
	filter    string
	status    string
	history   *historyStore
	stale     bool
	lastFresh time.Time
	fetching  bool
	viewTime  time.Time

	activeTab      tab
	tabs           []tab
	tabStates      map[tab]*tabState
	hasFrankenPHP  bool
	hasUpstreams   bool
	hostSortBy     model.HostSortField
	upstreamSortBy model.UpstreamSortField

	configRoot       *jsonNode
	configCursor     int
	configFilter     string
	configFilterMode bool

	certificates []fetcher.CertificateInfo
	certSortBy   model.CertSortField

	rpConfigs []fetcher.ReverseProxyConfig
	downSince map[string]time.Time

	logBuffer        *model.LogBuffer
	runtimeLogBuffer *model.LogBuffer
	routeAggregator  *model.RouteAggregator
	logSource        string

	logFrozen       bool
	logSnapshot     []fetcher.LogEntry
	logFrozenAt     int64
	logScrollOffset int

	logSel              logSel
	logSidepanelFocused bool
	routeSortBy         model.RouteSortField

	pluginTabs   []*pluginTab
	pluginGroups []*pluginGroup
	ctx          context.Context
	cancel       context.CancelFunc
}

const tabPluginBase tab = 100

func NewApp(f fetcher.Fetcher, cfg Config) *App {
	if cfg.NoColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	tabs := []tab{tabCaddy}
	if cfg.HasFrankenPHP {
		tabs = append(tabs, tabFrankenPHP)
	}
	tabs = append(tabs, tabLogs, tabConfig, tabCertificates)
	activeTab := tabCaddy

	var pluginTabs []*pluginTab
	var pluginGroups []*pluginGroup
	nextID := tabPluginBase
	for _, p := range cfg.Plugins {
		pts, g := newPluginTabs(p, nextID)
		pluginGroups = append(pluginGroups, g)
		for _, pt := range pts {
			pluginTabs = append(pluginTabs, pt)
			tabs = append(tabs, pt.tabID)
		}
		advance := len(pts)
		if advance < 1 {
			advance = 1
		}
		nextID += tab(advance)
	}

	ts := make(map[tab]*tabState)
	for _, t := range tabs {
		ts[t] = &tabState{}
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &App{
		fetcher:          f,
		config:           cfg,
		tabs:             tabs,
		activeTab:        activeTab,
		tabStates:        ts,
		hasFrankenPHP:    cfg.HasFrankenPHP,
		history:          newHistoryStore(),
		viewTime:         time.Now(),
		downSince:        make(map[string]time.Time),
		logBuffer:        cfg.LogBuffer,
		runtimeLogBuffer: cfg.RuntimeLogBuffer,
		routeAggregator:  cfg.RouteAggregator,
		logSource:        cfg.LogSource,
		logSel:           logSel{kind: logSelAccess},
		pluginTabs:       pluginTabs,
		pluginGroups:     pluginGroups,
		ctx:              ctx,
		cancel:           cancel,
	}
}

// Close cancels the app context, signaling plugin fetches to stop.
func (a *App) Close() {
	if a.cancel != nil {
		a.cancel()
	}
}

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.doFetch(), a.doTick()}
	cmds = append(cmds, a.doPluginFetches()...)
	if a.config.MetricsServerErr != nil {
		ch := a.config.MetricsServerErr
		cmds = append(cmds, func() tea.Msg {
			err, ok := <-ch
			if !ok {
				return nil
			}
			return metricsServerErrMsg{err: err}
		})
	}
	if a.logBuffer != nil {
		cmds = append(cmds, a.scheduleLogRefresh())
	}
	return tea.Batch(cmds...)
}

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	if a.state.Current != nil &&
		len(a.state.Current.Threads.ThreadDebugStates) == 0 &&
		!a.state.Current.Metrics.HasHTTPMetrics &&
		len(a.state.Current.Errors) > 0 {
		return renderConnectionError(a.state.Current.Errors[0], a.width, a.height)
	}

	sidePanel := a.mode == viewDetail && a.width >= detailSideThreshold
	panelWidth := 0
	if sidePanel {
		panelWidth = detailPanelWidth
		if panelWidth > a.width/2 {
			panelWidth = a.width / 2
		}
	}
	listWidth := a.width - panelWidth

	dashboard := renderDashboard(&a.state, listWidth, a.config.Version, lastN(a.history.rps, sparklineSize), lastN(a.history.cpu, sparklineSize), a.stale, a.paused, a.hasFrankenPHP)
	counts := make(map[tab]string)
	if a.state.Current != nil {
		if hostCount := len(a.state.HostDerived); hostCount > 0 {
			counts[tabCaddy] = fmt.Sprintf("%d hosts", hostCount)
		}
		if a.hasFrankenPHP {
			threadCount := len(a.state.Current.Threads.ThreadDebugStates)
			if threadCount > 0 {
				counts[tabFrankenPHP] = fmt.Sprintf("%d threads", threadCount)
			}
		}
	}
	if a.certificates != nil {
		counts[tabCertificates] = fmt.Sprintf("%d certs", len(a.certificates))
	}
	if a.hasUpstreams {
		counts[tabUpstreams] = fmt.Sprintf("%d upstreams", len(a.state.UpstreamDerived))
	}
	accessLen, accessFull := logBufferStats(a.logBuffer)
	runtimeLen, runtimeFull := logBufferStats(a.runtimeLogBuffer)
	total := accessLen + runtimeLen
	if total > 0 {
		if accessFull || runtimeFull {
			counts[tabLogs] = fmt.Sprintf("%d+", total)
		} else {
			counts[tabLogs] = fmt.Sprintf("%d", total)
		}
	}
	for _, pt := range a.pluginTabs {
		if pt.renderer != nil {
			c, _ := safePluginStatusCount(pt.renderer)
			if c != "" {
				counts[pt.tabID] = c
			}
		}
	}
	tabBar := renderTabBar(a.tabs, a.activeTab, listWidth, counts, a.pluginTabs)
	help := renderHelp(a.sortBy, a.hostSortBy, a.certSortBy, a.upstreamSortBy, a.routeSortBy, a.paused, listWidth, a.activeTab, a.logFrozen, a.isRoutesView())

	var threads []fetcher.ThreadDebugState
	var hosts []model.HostDerived
	var contentList string
	dashLines := strings.Count(dashboard, "\n") + strings.Count(tabBar, "\n") + 2
	contentAreaHeight := a.height - dashLines - 4
	if a.mode == viewFilter {
		contentAreaHeight--
	}
	if contentAreaHeight < 5 {
		contentAreaHeight = 5
	}
	switch a.activeTab {
	case tabConfig:
		configAreaHeight := a.height - dashLines - 4
		if configAreaHeight < 5 {
			configAreaHeight = 5
		}
		if a.configRoot != nil {
			contentList = renderConfigTree(a.configRoot, a.configCursor, listWidth, configAreaHeight, a.configFilter, a.configFilterMode)
		} else {
			contentList = greyStyle.Render(" Loading config...")
		}
	case tabCertificates:
		if a.certificates != nil {
			certs := a.filteredCerts()
			if len(certs) == 0 && a.filter != "" {
				contentList = greyStyle.Render(fmt.Sprintf(" No matches for '%s'", a.filter))
			} else {
				contentList = renderCertificateTable(certs, a.cursor, listWidth, contentAreaHeight, a.certSortBy)
			}
		} else {
			contentList = greyStyle.Render(" Loading certificates...")
		}
	case tabFrankenPHP:
		threads = a.filteredThreads()
		if len(threads) == 0 && a.filter != "" {
			contentList = greyStyle.Render(fmt.Sprintf(" No matches for '%s'", a.filter))
		} else {
			contentList = renderWorkerListFromThreads(threads, a.cursor, listWidth, contentAreaHeight, a.sortBy, renderOpts{
				slowThreshold: a.config.SlowThreshold,
				prevMemory:    a.prevThreadMemory(),
				viewTime:      a.viewTime,
			})
		}
	case tabUpstreams:
		upstreams := a.filteredUpstreams()
		if len(upstreams) == 0 && a.filter != "" {
			contentList = greyStyle.Render(fmt.Sprintf(" No matches for '%s'", a.filter))
		} else {
			contentList = renderUpstreamTable(upstreams, a.cursor, listWidth, contentAreaHeight, a.upstreamSortBy, upstreamRenderOpts{
				rpConfigs: a.rpConfigs,
				downSince: a.downSince,
				viewTime:  a.viewTime,
			})
		}
	case tabCaddy:
		hosts = a.filteredHosts()
		if len(hosts) == 0 && a.filter != "" {
			contentList = greyStyle.Render(fmt.Sprintf(" No matches for '%s'", a.filter))
		} else {
			contentList = renderHostTable(hosts, a.cursor, listWidth, contentAreaHeight, a.hostSortBy, a.history.hostRPS)
		}
	case tabLogs:
		contentList = a.renderLogsTab(listWidth, contentAreaHeight)
	default:
		if pt := a.activePluginTab(); pt != nil && pt.renderer != nil {
			contentList = safePluginView(pt.renderer, listWidth, a.height-10)
			if pt.group.err != nil && contentList == "" {
				contentList = greyStyle.Render(" " + pt.group.err.Error())
			}
		}
	}

	var statusLine string
	if a.status != "" {
		statusLine = helpStyle.Render(" " + a.status)
	} else if a.err != nil {
		statusLine = helpStyle.Render(" ⚠ " + a.err.Error())
	}

	var filterLine string
	if a.mode == viewFilter {
		filterLine = " Filter: " + a.filter + "█"
	}

	parts := []string{dashboard, tabBar}
	switch a.mode {
	case viewGraph:
		graphAreaHeight := a.height - dashLines - 2
		if graphAreaHeight < 5 {
			graphAreaHeight = 5
		}
		parts = append(parts, renderGraphPanels(listWidth, graphAreaHeight, a.history.cpu, a.history.rps, a.history.rss, a.history.queue, a.history.busy, a.hasFrankenPHP))
		parts = append(parts, helpStyle.Width(listWidth).Render(" "+helpKeyStyle.Render("g/Esc")+" back  "+helpKeyStyle.Render("q")+" quit"))
	default:
		if filterLine != "" && a.activeTab != tabConfig {
			parts = append(parts, filterLine)
		}
		parts = append(parts, contentList)
		if statusLine != "" {
			parts = append(parts, statusLine)
		}
		parts = append(parts, help)
	}

	base := lipgloss.JoinVertical(lipgloss.Left, parts...)

	if a.mode == viewDetail {
		if a.activeTab == tabCaddy {
			if a.cursor >= 0 && a.cursor < len(hosts) {
				h := hosts[a.cursor]
				if sidePanel {
					panel := renderHostDetailPanel(h, panelWidth, a.height)
					return lipgloss.JoinHorizontal(lipgloss.Top, base, panel)
				}
				panel := renderHostDetailPanel(h, a.width, detailPanelHeight+6)
				return lipgloss.JoinVertical(lipgloss.Left, base, panel)
			}
		} else if a.cursor >= 0 && a.cursor < len(threads) {
			t := threads[a.cursor]
			samples := a.history.mem[t.Index]
			if sidePanel {
				panel := renderDetailPanel(t, panelWidth, a.height, samples, a.viewTime)
				return lipgloss.JoinHorizontal(lipgloss.Top, base, panel)
			}
			panel := renderDetailPanel(t, a.width, detailPanelHeight, samples, a.viewTime)
			return lipgloss.JoinVertical(lipgloss.Left, base, panel)
		}
	}

	if a.mode == viewConfirmRestart {
		return renderConfirmOverlay(base, a.width, a.height)
	}

	if a.mode == viewHelp {
		return renderHelpOverlay(a.width, a.height, a.hasUpstreams, a.hasFrankenPHP, a.pluginTabs, a.tabs)
	}

	return base
}

func renderConfirmOverlay(base string, width, height int) string {
	popup := boxStyle.Render(
		titleStyle.Render("Restart all workers?") + "\n\n" +
			"  Press [y] to confirm, any other key to cancel",
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup)
}
