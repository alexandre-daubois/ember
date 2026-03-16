package ui

import (
	"testing"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	tea "github.com/charmbracelet/bubbletea"
)

func newAppWithThreads(threads []fetcher.ThreadDebugState) *App {
	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: threads,
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}
	app := &App{
		activeTab:     TabFrankenPHP,
		tabs:          []Tab{TabCaddy, TabFrankenPHP},
		tabStates:     map[Tab]*tabState{TabCaddy: {}, TabFrankenPHP: {}},
		hasFrankenPHP: true,
		history:       newHistoryStore(),
	}
	app.state.Update(snap)
	return app
}

func TestFilteredThreads_NoFilter(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread"},
		{Index: 1, Name: "Regular PHP Thread"},
	})

	result := app.filteredThreads()
	assert.Len(t, result, 2)
}

func TestFilteredThreads_ByName(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread - /app/worker.php"},
		{Index: 1, Name: "Regular PHP Thread"},
		{Index: 2, Name: "Worker PHP Thread - /app/api.php"},
	})
	app.filter = "worker"

	result := app.filteredThreads()
	assert.Len(t, result, 2)
}

func TestFilteredThreads_ByState(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0", State: "ready"},
		{Index: 1, Name: "Thread 1", State: "busy"},
		{Index: 2, Name: "Thread 2", State: "ready"},
	})
	app.filter = "ready"

	result := app.filteredThreads()
	assert.Len(t, result, 2)
}

func TestFilteredThreads_CaseInsensitive(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread"},
		{Index: 1, Name: "Regular PHP Thread"},
	})
	app.filter = "WORKER"

	result := app.filteredThreads()
	assert.Len(t, result, 1)
}

func TestFilteredThreads_ByMethod(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0", CurrentMethod: "GET"},
		{Index: 1, Name: "Thread 1", CurrentMethod: "POST"},
		{Index: 2, Name: "Thread 2", CurrentMethod: ""},
	})
	app.filter = "post"

	result := app.filteredThreads()
	assert.Len(t, result, 1)
	assert.Equal(t, "POST", result[0].CurrentMethod)
}

func TestFilteredThreads_ByURI(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0", CurrentURI: "/api/users"},
		{Index: 1, Name: "Thread 1", CurrentURI: "/api/orders"},
		{Index: 2, Name: "Thread 2", CurrentURI: ""},
	})
	app.filter = "users"

	result := app.filteredThreads()
	assert.Len(t, result, 1)
	assert.Equal(t, "/api/users", result[0].CurrentURI)
}

func TestFilteredThreads_ByPartialURI(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0", CurrentURI: "/api/v1/users/123"},
		{Index: 1, Name: "Thread 1", CurrentURI: "/api/v1/orders"},
		{Index: 2, Name: "Thread 2", CurrentURI: "/health"},
	})
	app.filter = "/api/v1"

	result := app.filteredThreads()
	assert.Len(t, result, 2)
}

func TestFilteredThreads_MethodCaseInsensitive(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0", CurrentMethod: "GET"},
		{Index: 1, Name: "Thread 1", CurrentMethod: "POST"},
	})
	app.filter = "get"

	result := app.filteredThreads()
	assert.Len(t, result, 1)
	assert.Equal(t, 0, result[0].Index)
}

func TestFilteredThreads_MatchesAnyField(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread", CurrentMethod: "POST", CurrentURI: "/upload"},
		{Index: 1, Name: "Regular PHP Thread", CurrentMethod: "GET", CurrentURI: "/api"},
		{Index: 2, Name: "Regular PHP Thread", State: "inactive"},
	})

	app.filter = "worker"
	assert.Len(t, app.filteredThreads(), 1, "should match by name")

	app.filter = "upload"
	assert.Len(t, app.filteredThreads(), 1, "should match by URI")

	app.filter = "post"
	assert.Len(t, app.filteredThreads(), 1, "should match by method")

	app.filter = "inactive"
	assert.Len(t, app.filteredThreads(), 1, "should match by state")
}

func TestFilteredThreads_NoMatch(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread"},
	})
	app.filter = "xyz"

	result := app.filteredThreads()
	assert.Empty(t, result)
}

func TestGraphToggle(t *testing.T) {
	app := &App{mode: viewList}

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	assert.Equal(t, viewGraph, app.mode, "pressing g should switch to graph view")
	assert.Equal(t, viewList, app.prevMode, "prevMode should remember list")
}

func TestGraphEscReturns(t *testing.T) {
	app := &App{mode: viewGraph, prevMode: viewList}

	app.handleGraphKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Equal(t, viewList, app.mode, "Esc should return to previous view")
}

func TestGraphGReturns(t *testing.T) {
	app := &App{mode: viewGraph, prevMode: viewDetail}

	app.handleGraphKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	assert.Equal(t, viewDetail, app.mode, "g should return to previous view")
}

func TestFetchMsg_SkipsWhenAlreadyFetching(t *testing.T) {
	app := &App{fetching: true}
	_, cmd := app.Update(tickMsg{})

	assert.True(t, app.fetching, "fetching should remain true")
	assert.NotNil(t, cmd, "should still return a tick cmd")
}

func TestFetchMsg_SetsFetchingFlag(t *testing.T) {
	app := &App{}

	assert.False(t, app.fetching)
	_, _ = app.Update(tickMsg{})
	assert.True(t, app.fetching, "fetching should be true after tick starts a fetch")
}

func TestFetchMsg_ClearsFetchingFlag(t *testing.T) {
	app := &App{fetching: true}

	_, _ = app.Update(fetchMsg{snap: &fetcher.Snapshot{}})
	assert.False(t, app.fetching, "fetching should be false after fetchMsg received")
}

func TestFetchMsg_RecoveryFromStaleZerosRPS(t *testing.T) {
	threads := []fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}}
	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{ThreadDebugStates: threads},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	app := &App{
		stale: true,
	}
	app.state.Update(snap) // seed initial state

	recovery := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{ThreadDebugStates: threads},
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 200000000},
			},
		},
	}

	app.Update(fetchMsg{snap: recovery})

	assert.False(t, app.stale, "should no longer be stale")
	assert.Equal(t, float64(0), app.state.Derived.RPS, "RPS should be 0 on first tick after stale recovery")
	assert.Equal(t, float64(0), app.state.Derived.AvgTime, "AvgTime should be 0 on first tick after stale recovery")
}

func TestFetchMsg_RecoveryFromStaleResetsPercentiles(t *testing.T) {
	threads := []fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}}
	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{ThreadDebugStates: threads},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	app := &App{
		stale: true,
	}
	now := time.Now()
	app.state.Update(snap)
	app.state.Percentiles.Record(150.0, now)
	app.state.Percentiles.Record(250.0, now)
	assert.Equal(t, 2, app.state.Percentiles.Count(now))

	recovery := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{ThreadDebugStates: threads},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	app.Update(fetchMsg{snap: recovery})

	assert.False(t, app.stale)
	assert.False(t, app.state.Derived.HasPercentiles)
	assert.Equal(t, 0, app.state.Percentiles.Count(now))
}

func TestMemHistory_PopulatedOnFetch(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, IsWaiting: true, MemoryUsage: 5 * 1024 * 1024},
		{Index: 1, IsBusy: true, MemoryUsage: 10 * 1024 * 1024},
	}
	snap := &fetcher.Snapshot{
		Threads:   fetcher.ThreadsResponse{ThreadDebugStates: threads},
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		FetchedAt: time.Now(),
	}
	app := &App{
		history: newHistoryStore(),
	}
	app.state.Update(&fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	})

	app.Update(fetchMsg{snap: snap})

	assert.Len(t, app.history.mem[0], 1)
	assert.Len(t, app.history.mem[1], 1)
	assert.Equal(t, int64(5*1024*1024), app.history.mem[0][0])
	assert.Equal(t, int64(10*1024*1024), app.history.mem[1][0])
}

func TestMemHistory_CappedAtMax(t *testing.T) {
	app := &App{
		history: newHistoryStore(),
	}
	app.state.Update(&fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	})

	for i := 0; i < memHistorySize+20; i++ {
		snap := &fetcher.Snapshot{
			Threads: fetcher.ThreadsResponse{
				ThreadDebugStates: []fetcher.ThreadDebugState{
					{Index: 0, MemoryUsage: int64(i) * 1024 * 1024},
				},
			},
			Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
			FetchedAt: time.Now(),
		}
		app.Update(fetchMsg{snap: snap})
	}

	assert.Len(t, app.history.mem[0], memHistorySize)
}

func TestMemHistory_SkipsZeroMemory(t *testing.T) {
	app := &App{
		history: newHistoryStore(),
	}
	app.state.Update(&fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	})

	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, MemoryUsage: 0},
			},
		},
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		FetchedAt: time.Now(),
	}
	app.Update(fetchMsg{snap: snap})

	assert.Empty(t, app.history.mem[0])
}

func TestPrevThreadMemory(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, MemoryUsage: 5 * 1024 * 1024},
		{Index: 1, MemoryUsage: 10 * 1024 * 1024},
	})

	snap2 := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, MemoryUsage: 6 * 1024 * 1024},
				{Index: 1, MemoryUsage: 12 * 1024 * 1024},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}
	app.state.Update(snap2)

	prev := app.prevThreadMemory()
	assert.Equal(t, int64(5*1024*1024), prev[0])
	assert.Equal(t, int64(10*1024*1024), prev[1])
}

func TestPrevThreadMemory_NilWhenNoPrevious(t *testing.T) {
	app := &App{}
	assert.Nil(t, app.prevThreadMemory())
}

func TestOnStateUpdate_CalledOnFetch(t *testing.T) {
	var called bool
	app := &App{
		history: newHistoryStore(),
		config: Config{
			OnStateUpdate: func(s model.State) {
				called = true
			},
		},
	}
	app.state.Update(&fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	})

	snap := &fetcher.Snapshot{
		Threads:   fetcher.ThreadsResponse{ThreadDebugStates: []fetcher.ThreadDebugState{{Index: 0}}},
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		FetchedAt: time.Now(),
	}
	app.Update(fetchMsg{snap: snap})

	assert.True(t, called, "OnStateUpdate should be called after fetchMsg")
}

func TestOnStateUpdate_NotCalledWhenNil(t *testing.T) {
	app := &App{
		history: newHistoryStore(),
	}
	app.state.Update(&fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	})

	snap := &fetcher.Snapshot{
		Threads:   fetcher.ThreadsResponse{ThreadDebugStates: []fetcher.ThreadDebugState{{Index: 0}}},
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		FetchedAt: time.Now(),
	}
	assert.NotPanics(t, func() {
		app.Update(fetchMsg{snap: snap})
	})
}

func TestFilteredThreads_Sorted(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 2, Name: "Thread 2"},
		{Index: 0, Name: "Thread 0"},
		{Index: 1, Name: "Thread 1"},
	})
	app.sortBy = model.SortByIndex

	result := app.filteredThreads()
	assert.Equal(t, 0, result[0].Index)
	assert.Equal(t, 1, result[1].Index)
	assert.Equal(t, 2, result[2].Index)
}

func TestEmptyFilterResults_FrankenPHP(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread"},
		{Index: 1, Name: "Regular PHP Thread"},
	})
	app.filter = "xyz"
	app.width = 120
	app.height = 40

	output := app.View()
	assert.Contains(t, output, "No matches")
}

func TestEmptyFilterResults_Caddy(t *testing.T) {
	app := &App{
		activeTab:     TabCaddy,
		tabs:          []Tab{TabCaddy},
		tabStates:     map[Tab]*tabState{TabCaddy: {}},
		hasFrankenPHP: false,
		history:       newHistoryStore(),
		width:         120,
		height:        40,
		filter:        "xyz",
	}
	app.state.Update(&fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			HasHTTPMetrics: true,
			Workers:        map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"example.com": {RequestsTotal: 100},
			},
		},
	})

	output := app.View()
	assert.Contains(t, output, "No matches")
}

func TestHome_GoesToStart(t *testing.T) {
	app := newAppWithThreads(make([]fetcher.ThreadDebugState, 10))
	app.cursor = 5

	app.handleListKey(tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, app.cursor)
}

func TestEnd_GoesToEnd(t *testing.T) {
	threads := make([]fetcher.ThreadDebugState, 10)
	for i := range threads {
		threads[i] = fetcher.ThreadDebugState{Index: i}
	}
	app := newAppWithThreads(threads)
	app.cursor = 0

	app.handleListKey(tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, 9, app.cursor)
}

func TestPgDown_MovesByPage(t *testing.T) {
	threads := make([]fetcher.ThreadDebugState, 30)
	for i := range threads {
		threads[i] = fetcher.ThreadDebugState{Index: i}
	}
	app := newAppWithThreads(threads)
	app.cursor = 0
	app.height = 20

	app.handleListKey(tea.KeyMsg{Type: tea.KeyPgDown})
	assert.Equal(t, app.pageSize(), app.cursor)
}

func TestPgUp_ClampsToZero(t *testing.T) {
	threads := make([]fetcher.ThreadDebugState, 10)
	for i := range threads {
		threads[i] = fetcher.ThreadDebugState{Index: i}
	}
	app := newAppWithThreads(threads)
	app.cursor = 3
	app.height = 40

	app.handleListKey(tea.KeyMsg{Type: tea.KeyPgUp})
	assert.Equal(t, 0, app.cursor)
}

func TestHome_DetailMode(t *testing.T) {
	app := newAppWithThreads(make([]fetcher.ThreadDebugState, 10))
	app.cursor = 5
	app.mode = viewDetail

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyHome})
	assert.Equal(t, 0, app.cursor)
}

func TestEnd_DetailMode(t *testing.T) {
	threads := make([]fetcher.ThreadDebugState, 10)
	for i := range threads {
		threads[i] = fetcher.ThreadDebugState{Index: i}
	}
	app := newAppWithThreads(threads)
	app.cursor = 0
	app.mode = viewDetail

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyEnd})
	assert.Equal(t, 9, app.cursor)
}

func TestHelpToggle(t *testing.T) {
	app := &App{mode: viewList}

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Equal(t, viewHelp, app.mode, "pressing ? should switch to help view")
	assert.Equal(t, viewList, app.prevMode, "prevMode should remember list")
}

func TestHelpEscReturns(t *testing.T) {
	app := &App{mode: viewHelp, prevMode: viewList}

	app.handleHelpKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Equal(t, viewList, app.mode, "Esc should return to previous view")
}

func TestGraphQQuits(t *testing.T) {
	app := &App{mode: viewGraph, prevMode: viewList}

	_, cmd := app.handleGraphKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, cmd, "q in graph view should return a quit command")
}

func TestHelpQQuits(t *testing.T) {
	app := &App{mode: viewHelp, prevMode: viewList}

	_, cmd := app.handleHelpKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	assert.NotNil(t, cmd, "q in help view should return a quit command")
}

func TestHelpFromDetailView(t *testing.T) {
	app := &App{mode: viewDetail}

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	assert.Equal(t, viewHelp, app.mode, "pressing ? from detail should switch to help view")
	assert.Equal(t, viewDetail, app.prevMode, "prevMode should remember detail")
}

func TestTabSwitch_TabKey(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}})
	assert.Equal(t, TabFrankenPHP, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, TabCaddy, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, TabFrankenPHP, app.activeTab)
}

func TestTabSwitch_NumberKeys(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}})

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	assert.Equal(t, TabCaddy, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	assert.Equal(t, TabFrankenPHP, app.activeTab)
}

func TestTabSwitch_PreservesCursorPerTab(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0}, {Index: 1}, {Index: 2},
	})
	app.cursor = 2

	app.handleListKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, 0, app.cursor, "Caddy tab should start at cursor 0")

	app.handleListKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, 2, app.cursor, "FrankenPHP tab should restore cursor")
}

func TestTabSwitch_PreservesFilterPerTab(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread - /app/w.php"},
	})
	app.filter = "worker"

	app.handleListKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, "", app.filter, "Caddy tab should start with empty filter")

	app.handleListKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, "worker", app.filter, "FrankenPHP tab should restore filter")
}

func TestTabSwitch_Key2NoOpWithSingleTab(t *testing.T) {
	app := &App{
		activeTab: TabCaddy,
		tabs:      []Tab{TabCaddy},
		tabStates: map[Tab]*tabState{TabCaddy: {}},
	}

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	assert.Equal(t, TabCaddy, app.activeTab)
}
