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
