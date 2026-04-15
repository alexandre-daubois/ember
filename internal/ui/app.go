package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
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
	OnStateUpdate    func(model.State)
	MetricsServerErr <-chan error
}

type tab int

const (
	tabCaddy tab = iota
	tabConfig
	tabCertificates
	tabUpstreams
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
}

func NewApp(f fetcher.Fetcher, cfg Config) *App {
	if cfg.NoColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	tabs := []tab{tabCaddy, tabConfig, tabCertificates}
	activeTab := tabCaddy
	if cfg.HasFrankenPHP {
		tabs = append(tabs, tabFrankenPHP)
	}

	ts := make(map[tab]*tabState)
	for _, t := range tabs {
		ts[t] = &tabState{}
	}

	return &App{
		fetcher:       f,
		config:        cfg,
		tabs:          tabs,
		activeTab:     activeTab,
		tabStates:     ts,
		hasFrankenPHP: cfg.HasFrankenPHP,
		history:       newHistoryStore(),
		viewTime:      time.Now(),
		downSince:     make(map[string]time.Time),
	}
}

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

func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{a.doFetch(), a.doTick()}
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
	return tea.Batch(cmds...)
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
	case tickMsg:
		if a.paused || a.fetching {
			return a, a.doTick()
		}
		a.fetching = true
		return a, tea.Batch(a.doFetch(), a.doTick())
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

			if a.config.OnStateUpdate != nil {
				a.config.OnStateUpdate(a.state)
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
	}
	return a, nil
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
	tabBar := renderTabBar(a.tabs, a.activeTab, listWidth, counts)
	help := renderHelp(a.sortBy, a.hostSortBy, a.certSortBy, a.upstreamSortBy, a.paused, listWidth, a.activeTab)

	var threads []fetcher.ThreadDebugState
	var hosts []model.HostDerived
	var contentList string
	switch a.activeTab {
	case tabConfig:
		dashLines := strings.Count(dashboard, "\n") + strings.Count(tabBar, "\n") + 2
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
				contentList = renderCertificateTable(certs, a.cursor, listWidth, a.certSortBy)
			}
		} else {
			contentList = greyStyle.Render(" Loading certificates...")
		}
	case tabFrankenPHP:
		threads = a.filteredThreads()
		if len(threads) == 0 && a.filter != "" {
			contentList = greyStyle.Render(fmt.Sprintf(" No matches for '%s'", a.filter))
		} else {
			contentList = renderWorkerListFromThreads(threads, a.cursor, listWidth, a.sortBy, renderOpts{
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
			contentList = renderUpstreamTable(upstreams, a.cursor, listWidth, a.upstreamSortBy, upstreamRenderOpts{
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
			contentList = renderHostTable(hosts, a.cursor, listWidth, a.hostSortBy, a.history.hostRPS)
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
		dashLines := strings.Count(dashboard, "\n") + strings.Count(tabBar, "\n") + 2
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
		return renderHelpOverlay(a.width, a.height, a.hasUpstreams, a.hasFrankenPHP)
	}

	return base
}

func (a *App) handleTabSwitch(key string) (tea.Cmd, bool) {
	switch key {
	case "tab":
		a.nextTab()
		return a.switchTabCmd(), true
	case "shift+tab":
		a.prevTab()
		return a.switchTabCmd(), true
	case "1":
		if len(a.tabs) > 0 {
			a.switchTab(a.tabs[0])
		}
		return a.switchTabCmd(), true
	case "2":
		if len(a.tabs) > 1 {
			a.switchTab(a.tabs[1])
		}
		return a.switchTabCmd(), true
	case "3":
		if len(a.tabs) > 2 {
			a.switchTab(a.tabs[2])
		}
		return a.switchTabCmd(), true
	case "4":
		if len(a.tabs) > 3 {
			a.switchTab(a.tabs[3])
		}
		return a.switchTabCmd(), true
	case "5":
		if len(a.tabs) > 4 {
			a.switchTab(a.tabs[4])
		}
		return a.switchTabCmd(), true
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
	if a.activeTab == tabConfig {
		return a.handleConfigListKey(msg)
	}
	if a.activeTab == tabCertificates {
		return a.handleCertListKey(msg)
	}
	if a.activeTab == tabUpstreams {
		return a.handleUpstreamListKey(msg)
	}

	key := msg.String()

	if cmd, ok := a.handleTabSwitch(key); ok {
		return a, cmd
	}

	maxIdx := a.listLen() - 1
	if maxIdx < 0 {
		maxIdx = 0
	}
	moveCursor(key, &a.cursor, maxIdx, a.pageSize())

	switch key {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "s":
		if a.activeTab == tabCaddy {
			a.hostSortBy = a.hostSortBy.Next()
		} else {
			a.sortBy = a.sortBy.Next()
		}
	case "S":
		if a.activeTab == tabCaddy {
			a.hostSortBy = a.hostSortBy.Prev()
		} else {
			a.sortBy = a.sortBy.Prev()
		}
	case "p":
		a.paused = !a.paused
	case "enter":
		a.mode = viewDetail
	case "r":
		if a.activeTab == tabFrankenPHP {
			a.mode = viewConfirmRestart
		}
	case "/":
		a.mode = viewFilter
		a.filter = ""
	case "g":
		a.prevMode = a.mode
		a.mode = viewGraph
	case "?":
		a.prevMode = a.mode
		a.mode = viewHelp
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
	case "enter":
		a.mode = viewList
		a.cursor = 0
	case "backspace":
		if len(a.filter) > 0 {
			a.filter = a.filter[:len(a.filter)-1]
		}
	default:
		if len(msg.String()) == 1 {
			a.filter += msg.String()
			a.cursor = 0
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
	default:
		return len(a.filteredThreads())
	}
}

func (a *App) clampCursor() {
	if a.activeTab == tabConfig {
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
	default:
		count = len(a.filteredThreads())
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

func (a *App) enableFrankenPHP() {
	a.hasFrankenPHP = true
	a.tabs = append(a.tabs, tabFrankenPHP)
	a.tabStates[tabFrankenPHP] = &tabState{}
}

func (a *App) enableUpstreams() {
	a.hasUpstreams = true
	newTabs := make([]tab, 0, len(a.tabs)+1)
	for _, t := range a.tabs {
		newTabs = append(newTabs, t)
		if t == tabCaddy {
			newTabs = append(newTabs, tabUpstreams)
		}
	}
	a.tabs = newTabs
	a.tabStates[tabUpstreams] = &tabState{}
}

func (a *App) doTick() tea.Cmd {
	return tea.Tick(a.config.Interval, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

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

func renderConfirmOverlay(base string, width, height int) string {
	popup := boxStyle.Render(
		titleStyle.Render("Restart all workers?") + "\n\n" +
			"  Press [y] to confirm, any other key to cancel",
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup)
}
