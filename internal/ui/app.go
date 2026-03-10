package ui

import (
	"context"
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
)

type Config struct {
	Interval      time.Duration
	SlowThreshold time.Duration
	LeakThreshold int
	LeakWindow    int
	NoColor       bool
	Version       string
}

type restarter interface {
	RestartWorkers(ctx context.Context) error
}

const sparklineSize = 15

type App struct {
	fetcher     fetcher.Fetcher
	config      Config
	state       model.State
	leakWatcher *model.LeakWatcher
	leakEnabled bool
	cursor      int
	sortBy      model.SortField
	paused      bool
	width       int
	height      int
	err         error
	mode        viewMode
	filter      string
	status      string
	rpsHistory  []float64
	cpuHistory  []float64
}

func NewApp(f fetcher.Fetcher, cfg Config) *App {
	if cfg.NoColor {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	return &App{
		fetcher:     f,
		config:      cfg,
		leakWatcher: model.NewLeakWatcher(cfg.LeakWindow, cfg.LeakThreshold),
		leakEnabled: true,
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
		if a.paused {
			return a, a.doTick()
		}
		return a, tea.Batch(a.doFetch(), a.doTick())
	case fetchMsg:
		a.err = msg.err
		if msg.snap != nil {
			hasThreads := len(msg.snap.Threads.ThreadDebugStates) > 0
			hadThreads := a.state.Current != nil && len(a.state.Current.Threads.ThreadDebugStates) > 0

			if !hasThreads && hadThreads {
				a.state.Current.Process = msg.snap.Process
				a.cpuHistory = appendSparkline(a.cpuHistory, msg.snap.Process.CPUPercent)
				if msg.snap.Process.CPUPercent >= 80 {
					a.status = "⚠ System under high load — reconnecting…"
				} else {
					a.status = "⚠ Connection lost — reconnecting…"
				}
				return a, nil
			}

			a.state.Update(msg.snap)
			a.clampCursor()
			if len(msg.snap.Errors) > 0 {
				a.status = "⚠ " + strings.Join(msg.snap.Errors, " | ")
			} else {
				a.status = ""
			}
			a.rpsHistory = appendSparkline(a.rpsHistory, a.state.Derived.RPS)
			a.cpuHistory = appendSparkline(a.cpuHistory, msg.snap.Process.CPUPercent)
			if a.leakEnabled {
				for _, t := range msg.snap.Threads.ThreadDebugStates {
					if t.IsWaiting && t.MemoryUsage > 0 {
						a.leakWatcher.Record(t.Index, t.MemoryUsage)
					}
				}
			}
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

func appendSparkline(history []float64, val float64) []float64 {
	history = append(history, val)
	if len(history) > sparklineSize {
		history = history[len(history)-sparklineSize:]
	}
	return history
}

func (a *App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	if a.state.Current != nil && len(a.state.Current.Threads.ThreadDebugStates) == 0 && len(a.state.Current.Errors) > 0 {
		return renderConnectionError(a.state.Current.Errors[0], a.width, a.height)
	}

	dashboard := renderDashboard(&a.state, a.width, a.config.Version, a.rpsHistory, a.cpuHistory)
	help := renderHelp(a.sortBy, a.paused, a.leakEnabled)

	threads := a.filteredThreads()
	workerList := renderWorkerListFromThreads(threads, a.cursor, a.width, renderOpts{
		slowThreshold: a.config.SlowThreshold,
		leakWatcher:   a.leakWatcher,
		leakEnabled:   a.leakEnabled,
	})

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
	if filterLine != "" {
		parts = append(parts, filterLine)
	}
	parts = append(parts, workerList)
	if statusLine != "" {
		parts = append(parts, statusLine)
	}
	parts = append(parts, help)

	base := lipgloss.JoinVertical(lipgloss.Left, parts...)

	switch a.mode {
	case viewDetail:
		if t, ok := a.selectedThread(); ok {
			ls := a.leakWatcher.Status(t.Index)
			return renderDetail(t, ls, a.width, a.height)
		}
	case viewConfirmRestart:
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
	default:
		return a.handleListKey(msg)
	}
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
	case "p":
		a.paused = !a.paused
	case "enter":
		a.mode = viewDetail
	case "r":
		a.mode = viewConfirmRestart
	case "/":
		a.mode = viewFilter
		a.filter = ""
	case "l":
		a.leakEnabled = !a.leakEnabled
		if a.leakEnabled {
			a.status = "leak watcher enabled"
		} else {
			a.status = "leak watcher disabled"
		}
	}
	return a, nil
}

func (a *App) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		a.mode = viewList
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
			strings.Contains(strings.ToLower(t.State), f) {
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
