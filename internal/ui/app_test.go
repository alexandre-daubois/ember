package ui

import (
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
)

func newAppWithThreads(threads []fetcher.ThreadDebugState) *App {
	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: threads,
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}
	app := &App{
		activeTab:     tabFrankenPHP,
		tabs:          []tab{tabCaddy, tabFrankenPHP},
		tabStates:     map[tab]*tabState{tabCaddy: {}, tabFrankenPHP: {}},
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
	app.state.Update(snap)

	recovery := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{ThreadDebugStates: threads},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	app.Update(fetchMsg{snap: recovery})

	assert.False(t, app.stale)
	assert.False(t, app.state.Derived.HasPercentiles)
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
		activeTab:     tabCaddy,
		tabs:          []tab{tabCaddy},
		tabStates:     map[tab]*tabState{tabCaddy: {}},
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
	assert.Equal(t, tabFrankenPHP, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tabCaddy, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tabFrankenPHP, app.activeTab)
}

func TestTabSwitch_ShiftTabKey(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}})
	assert.Equal(t, tabFrankenPHP, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tabCaddy, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tabFrankenPHP, app.activeTab)
}

func TestTabSwitch_ShiftTabReversesTab(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}})
	assert.Equal(t, tabFrankenPHP, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyTab})
	assert.Equal(t, tabCaddy, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, tabFrankenPHP, app.activeTab, "Shift+Tab should reverse Tab direction")
}

func TestTabSwitch_ShiftTabPreservesCursorPerTab(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0}, {Index: 1}, {Index: 2},
	})
	app.cursor = 2

	app.handleListKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, 0, app.cursor, "Caddy tab should start at cursor 0")

	app.handleListKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	assert.Equal(t, 2, app.cursor, "FrankenPHP tab should restore cursor")
}

func TestTabSwitch_NumberKeys(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}})

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	assert.Equal(t, tabCaddy, app.activeTab)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	assert.Equal(t, tabFrankenPHP, app.activeTab)
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
		activeTab: tabCaddy,
		tabs:      []tab{tabCaddy},
		tabStates: map[tab]*tabState{tabCaddy: {}},
	}

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	assert.Equal(t, tabCaddy, app.activeTab)
}

func TestEnableFrankenPHP_OnFetch(t *testing.T) {
	app := &App{
		activeTab:     tabCaddy,
		tabs:          []tab{tabCaddy},
		tabStates:     map[tab]*tabState{tabCaddy: {}},
		hasFrankenPHP: false,
		history:       newHistoryStore(),
	}
	app.state.Update(&fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	})

	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}},
		},
		Metrics:       fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		HasFrankenPHP: true,
	}
	app.Update(fetchMsg{snap: snap})

	assert.True(t, app.hasFrankenPHP, "should enable FrankenPHP flag")
	assert.Contains(t, app.tabs, tabFrankenPHP, "should add FrankenPHP tab")
	assert.NotNil(t, app.tabStates[tabFrankenPHP], "should initialize FrankenPHP tab state")
}

func TestHandleKey_DispatchesToFilter(t *testing.T) {
	app := &App{mode: viewFilter}
	app.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Equal(t, viewList, app.mode, "handleKey should dispatch to handleFilterKey in filter mode")
}

func TestHandleKey_DispatchesToDetail(t *testing.T) {
	app := &App{mode: viewDetail}
	app.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Equal(t, viewList, app.mode, "handleKey should dispatch to handleDetailKey in detail mode")
}

func TestHandleKey_DispatchesToConfirm(t *testing.T) {
	app := &App{mode: viewConfirmRestart}
	app.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Equal(t, viewList, app.mode, "handleKey should dispatch to handleConfirmKey in confirm mode")
}

func TestHandleKey_DispatchesToGraph(t *testing.T) {
	app := &App{mode: viewGraph, prevMode: viewList}
	app.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Equal(t, viewList, app.mode, "handleKey should dispatch to handleGraphKey in graph mode")
}

func TestHandleKey_DispatchesToHelp(t *testing.T) {
	app := &App{mode: viewHelp, prevMode: viewList}
	app.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Equal(t, viewList, app.mode, "handleKey should dispatch to handleHelpKey in help mode")
}

func TestHandleKey_DefaultToList(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0}, {Index: 1},
	})
	app.mode = viewList
	app.cursor = 0
	app.handleKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, app.cursor, "handleKey should dispatch to handleListKey by default")
}

func TestHandleFilterKey_EscCancelsFilter(t *testing.T) {
	app := &App{mode: viewFilter, filter: "test"}
	app.handleFilterKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Equal(t, viewList, app.mode)
	assert.Equal(t, "", app.filter, "esc should clear filter")
}

func TestHandleFilterKey_EnterConfirmsFilter(t *testing.T) {
	app := &App{mode: viewFilter, filter: "test", cursor: 5}
	app.handleFilterKey(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, viewList, app.mode)
	assert.Equal(t, "test", app.filter, "enter should keep filter")
	assert.Equal(t, 0, app.cursor, "enter should reset cursor")
}

func TestHandleFilterKey_BackspaceRemovesLastChar(t *testing.T) {
	app := &App{mode: viewFilter, filter: "test"}
	app.handleFilterKey(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "tes", app.filter)
}

func TestHandleFilterKey_BackspaceOnEmptyFilter(t *testing.T) {
	app := &App{mode: viewFilter, filter: ""}
	app.handleFilterKey(tea.KeyMsg{Type: tea.KeyBackspace})
	assert.Equal(t, "", app.filter)
}

func TestHandleFilterKey_TypeCharacter(t *testing.T) {
	app := &App{mode: viewFilter, filter: "te", cursor: 5}
	app.handleFilterKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	assert.Equal(t, "tes", app.filter)
	assert.Equal(t, 0, app.cursor, "typing should reset cursor")
}

func TestHandleConfirmKey_YConfirmsRestart(t *testing.T) {
	app := &App{mode: viewConfirmRestart}
	_, cmd := app.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	assert.Equal(t, viewList, app.mode)
	assert.Contains(t, app.status, "restarting")
	assert.NotNil(t, cmd, "y should trigger a restart command")
}

func TestHandleConfirmKey_YUpperConfirmsRestart(t *testing.T) {
	app := &App{mode: viewConfirmRestart}
	_, cmd := app.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})
	assert.Equal(t, viewList, app.mode)
	assert.Contains(t, app.status, "restarting")
	assert.NotNil(t, cmd)
}

func TestHandleConfirmKey_AnyOtherKeyCancels(t *testing.T) {
	app := &App{mode: viewConfirmRestart, status: "old"}
	app.handleConfirmKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	assert.Equal(t, viewList, app.mode)
	assert.Equal(t, "", app.status, "canceling should clear status")
}

func TestHandleConfirmKey_EscCancels(t *testing.T) {
	app := &App{mode: viewConfirmRestart}
	app.handleConfirmKey(tea.KeyMsg{Type: tea.KeyEscape})
	assert.Equal(t, viewList, app.mode)
}

func TestRenderConfirmOverlay_ContainsPrompt(t *testing.T) {
	overlay := renderConfirmOverlay("base content", 80, 24)
	assert.Contains(t, overlay, "Restart all workers?")
	assert.Contains(t, overlay, "[y]")
}

func TestHandleListKey_SlashEntersFilterMode(t *testing.T) {
	app := &App{mode: viewList, filter: "old"}
	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	assert.Equal(t, viewFilter, app.mode)
	assert.Equal(t, "", app.filter, "/ should reset filter")
}

func TestHandleListKey_PTogglesPause(t *testing.T) {
	app := &App{mode: viewList}
	assert.False(t, app.paused)
	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.True(t, app.paused)
	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	assert.False(t, app.paused)
}

func TestHandleListKey_RTriggersRestartOnFrankenPHP(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{{Index: 0}})
	app.mode = viewList
	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Equal(t, viewConfirmRestart, app.mode, "r should trigger confirm restart on FrankenPHP tab")
}

func TestHandleListKey_RNoOpOnCaddyTab(t *testing.T) {
	app := &App{
		mode:      viewList,
		activeTab: tabCaddy,
		tabs:      []tab{tabCaddy},
		tabStates: map[tab]*tabState{tabCaddy: {}},
	}
	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Equal(t, viewList, app.mode, "r should not trigger restart on Caddy tab")
}

func TestHandleListKey_SortCycling(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{{Index: 0}})
	app.sortBy = model.SortByIndex

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	assert.Equal(t, model.SortByState, app.sortBy, "s should advance sort field")

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	assert.Equal(t, model.SortByIndex, app.sortBy, "S should reverse sort field")
}

func TestEnableFrankenPHP_NoDoubleAdd(t *testing.T) {
	app := &App{
		activeTab:     tabCaddy,
		tabs:          []tab{tabCaddy, tabFrankenPHP},
		tabStates:     map[tab]*tabState{tabCaddy: {}, tabFrankenPHP: {}},
		hasFrankenPHP: true,
		history:       newHistoryStore(),
	}
	app.state.Update(&fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	})

	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}},
		},
		Metrics:       fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		HasFrankenPHP: true,
	}
	app.Update(fetchMsg{snap: snap})

	assert.Len(t, app.tabs, 2, "should not duplicate FrankenPHP tab")
}
