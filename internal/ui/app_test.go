package ui

import (
	"testing"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tea "github.com/charmbracelet/bubbletea"
)

func newAppWithThreads(threads []fetcher.ThreadDebugState) *App {
	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: threads,
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}
	app := &App{}
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

func TestLeakToggle(t *testing.T) {
	app := &App{
		leakWatcher: model.NewLeakWatcher(60, 5),
		leakEnabled: true,
	}

	require.True(t, app.leakEnabled, "leak should start enabled")

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	assert.False(t, app.leakEnabled, "leak should be disabled after pressing l")
	assert.Equal(t, "leak watcher disabled", app.status)

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	assert.True(t, app.leakEnabled, "leak should be re-enabled after pressing l again")
	assert.Equal(t, "leak watcher enabled", app.status)
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
