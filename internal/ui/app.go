package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
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
)

type Config struct {
	Interval      time.Duration
	SlowThreshold time.Duration
	NoColor       bool
	Version       string
}

type restarter interface {
	RestartWorkers(ctx context.Context) error
}

const sparklineSize = 15
const graphHistorySize = 300

type App struct {
	fetcher     fetcher.Fetcher
	config      Config
	state       model.State
	cursor      int
	sortBy      model.SortField
	paused      bool
	width       int
	height      int
	err         error
	mode        viewMode
	prevMode    viewMode
	filter      string
	status      string
	rpsHistory  []float64
	cpuHistory  []float64
	rssHistory  []float64
	queueHistory []float64
	busyHistory  []float64
	stale       bool
	lastFresh   time.Time
	fetching    bool
}

func NewApp(f fetcher.Fetcher, cfg Config) *App {
	if cfg.NoColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	return &App{
		fetcher:     f,
		config:      cfg,
	}
}

type tickMsg struct{}
type fetchMsg struct {
	snap *fetcher.Snapshot
	err  error
}
type restartResultMsg struct{ err error }

func (a *App) Init() tea.Cmd {
	return tea.Batch(a.doFetch(), a.doTick())
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return a.handleKey(msg)
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return a, nil
	case tickMsg:
		if a.paused || a.fetching {
			return a, a.doTick()
		}
		a.fetching = true
		return a, tea.Batch(a.doFetch(), a.doTick())
	case fetchMsg:
		a.fetching = false
		a.err = msg.err
		if msg.snap != nil {
			wasStale := a.stale
			hasThreads := len(msg.snap.Threads.ThreadDebugStates) > 0
			hadThreads := a.state.Current != nil && len(a.state.Current.Threads.ThreadDebugStates) > 0

			if !hasThreads && hadThreads {
				a.stale = true
				a.state.Current.Process = msg.snap.Process
				a.state.Current.Metrics = msg.snap.Metrics
				a.state.Current.FetchedAt = msg.snap.FetchedAt
				a.cpuHistory = appendHistory(a.cpuHistory, msg.snap.Process.CPUPercent, graphHistorySize)
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
				if a.state.Percentiles != nil {
					a.state.Percentiles.Reset()
				}
			}
			a.clampCursor()
			if len(msg.snap.Errors) > 0 {
				a.status = "⚠ " + strings.Join(msg.snap.Errors, " | ")
			} else {
				a.status = ""
			}
			a.rpsHistory = appendHistory(a.rpsHistory, a.state.Derived.RPS, graphHistorySize)
			a.cpuHistory = appendHistory(a.cpuHistory, msg.snap.Process.CPUPercent, graphHistorySize)
			a.rssHistory = appendHistory(a.rssHistory, float64(msg.snap.Process.RSS)/1024/1024, graphHistorySize)
			a.queueHistory = appendHistory(a.queueHistory, msg.snap.Metrics.QueueDepth, graphHistorySize)
			a.busyHistory = appendHistory(a.busyHistory, float64(a.state.Derived.TotalBusy), graphHistorySize)
		}
		return a, nil
	case restartResultMsg:
		if msg.err != nil {
			a.status = "restart failed: " + msg.err.Error()
		} else {
			a.status = "workers restarted"
		}
		return a, nil
	}
	return a, nil
}

func appendHistory(history []float64, val float64, maxSize int) []float64 {
	history = append(history, val)
	if len(history) > maxSize {
		history = history[len(history)-maxSize:]
	}
	return history
}

func lastN(history []float64, n int) []float64 {
	if len(history) <= n {
		return history
	}
	return history[len(history)-n:]
}

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	if a.state.Current != nil && len(a.state.Current.Threads.ThreadDebugStates) == 0 && len(a.state.Current.Errors) > 0 {
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

	dashboard := renderDashboard(&a.state, listWidth, a.config.Version, lastN(a.rpsHistory, sparklineSize), lastN(a.cpuHistory, sparklineSize), a.stale)
	help := renderHelp(a.sortBy, a.paused, listWidth)

	threads := a.filteredThreads()
	totalCount := 0
	if a.state.Current != nil {
		totalCount = len(a.state.Current.Threads.ThreadDebugStates)
	}
	workerList := renderWorkerListFromThreads(threads, a.cursor, listWidth, a.sortBy, renderOpts{
		slowThreshold: a.config.SlowThreshold,
	}, totalCount)

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

	parts := []string{dashboard}
	if a.mode == viewGraph {
		dashLines := strings.Count(dashboard, "\n") + 1
		graphAreaHeight := a.height - dashLines - 2
		if graphAreaHeight < 5 {
			graphAreaHeight = 5
		}
		parts = append(parts, renderGraphPanels(listWidth, graphAreaHeight, a.cpuHistory, a.rpsHistory, a.rssHistory, a.queueHistory, a.busyHistory))
		parts = append(parts, helpStyle.Width(listWidth).Render(" "+helpKeyStyle.Render("g/Esc")+" back  "+helpKeyStyle.Render("q")+" quit"))
	} else {
		if filterLine != "" {
			parts = append(parts, filterLine)
		}
		parts = append(parts, workerList)
		if statusLine != "" {
			parts = append(parts, statusLine)
		}
		parts = append(parts, help)
	}

	base := lipgloss.JoinVertical(lipgloss.Left, parts...)

	if a.mode == viewDetail {
		if t, ok := a.selectedThread(); ok {
			if sidePanel {
				panel := renderDetailPanel(t, panelWidth, a.height)
				return lipgloss.JoinHorizontal(lipgloss.Top, base, panel)
			}
			panel := renderDetailPanel(t, a.width, detailPanelHeight)
			return lipgloss.JoinVertical(lipgloss.Left, base, panel)
		}
	}

	if a.mode == viewConfirmRestart {
		return renderConfirmOverlay(base, a.width, a.height)
	}

	return base
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch a.mode {
	case viewFilter:
		return a.handleFilterKey(msg)
	case viewDetail:
		return a.handleDetailKey(msg)
	case viewConfirmRestart:
		return a.handleConfirmKey(msg)
	case viewGraph:
		return a.handleGraphKey(msg)
	default:
		return a.handleListKey(msg)
	}
}

func (a *App) handleGraphKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "g", "q":
		a.mode = a.prevMode
	case "ctrl+c":
		return a, tea.Quit
	}
	return a, nil
}

func (a *App) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return a, tea.Quit
	case "up", "k":
		if a.cursor > 0 {
			a.cursor--
		}
	case "down", "j":
		a.cursor++
		a.clampCursor()
	case "s":
		a.sortBy = a.sortBy.Next()
	case "S":
		a.sortBy = a.sortBy.Prev()
	case "p":
		a.paused = !a.paused
	case "enter":
		a.mode = viewDetail
	case "r":
		a.mode = viewConfirmRestart
	case "/":
		a.mode = viewFilter
		a.filter = ""
	case "g":
		a.prevMode = a.mode
		a.mode = viewGraph
	}
	return a, nil
}

func (a *App) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.mode = viewList
	case "up", "k":
		if a.cursor > 0 {
			a.cursor--
		}
	case "down", "j":
		a.cursor++
		a.clampCursor()
	case "r":
		a.mode = viewConfirmRestart
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

func (a *App) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	threads := sortThreads(a.state.Current.Threads.ThreadDebugStates, a.sortBy)
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

func (a *App) selectedThread() (fetcher.ThreadDebugState, bool) {
	threads := a.filteredThreads()
	if a.cursor >= 0 && a.cursor < len(threads) {
		return threads[a.cursor], true
	}
	return fetcher.ThreadDebugState{}, false
}

func (a *App) clampCursor() {
	threads := a.filteredThreads()
	maximum := len(threads) - 1
	if maximum < 0 {
		maximum = 0
	}
	if a.cursor > maximum {
		a.cursor = maximum
	}
}

func (a *App) doTick() tea.Cmd {
	return tea.Tick(a.config.Interval, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (a *App) doFetch() tea.Cmd {
	return func() tea.Msg {
		snap, err := a.fetcher.Fetch(context.Background())
		return fetchMsg{snap: snap, err: err}
	}
}

func (a *App) doRestart() tea.Cmd {
	return func() tea.Msg {
		if r, ok := a.fetcher.(restarter); ok {
			return restartResultMsg{err: r.RestartWorkers(context.Background())}
		}
		return restartResultMsg{}
	}
}

func renderConfirmOverlay(base string, width, height int) string {
	popup := boxStyle.Render(
		titleStyle.Render("Restart all workers?") + "\n\n" +
			"  Press [y] to confirm, any other key to cancel",
	)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, popup)
}
