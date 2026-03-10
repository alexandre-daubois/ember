package ui

import (
	"testing"

	"github.com/alexandredaubois/frankentop/internal/fetcher"
	"github.com/alexandredaubois/frankentop/internal/model"
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
	if len(result) != 2 {
		t.Errorf("expected 2 threads, got %d", len(result))
	}
}

func TestFilteredThreads_ByName(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread - /app/worker.php"},
		{Index: 1, Name: "Regular PHP Thread"},
		{Index: 2, Name: "Worker PHP Thread - /app/api.php"},
	})
	app.filter = "worker"

	result := app.filteredThreads()
	if len(result) != 2 {
		t.Errorf("expected 2 matches, got %d", len(result))
	}
}

func TestFilteredThreads_ByState(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0", State: "ready"},
		{Index: 1, Name: "Thread 1", State: "busy"},
		{Index: 2, Name: "Thread 2", State: "ready"},
	})
	app.filter = "ready"

	result := app.filteredThreads()
	if len(result) != 2 {
		t.Errorf("expected 2 matches for state 'ready', got %d", len(result))
	}
}

func TestFilteredThreads_CaseInsensitive(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread"},
		{Index: 1, Name: "Regular PHP Thread"},
	})
	app.filter = "WORKER"

	result := app.filteredThreads()
	if len(result) != 1 {
		t.Errorf("expected 1 match (case insensitive), got %d", len(result))
	}
}

func TestFilteredThreads_NoMatch(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Worker PHP Thread"},
	})
	app.filter = "xyz"

	result := app.filteredThreads()
	if len(result) != 0 {
		t.Errorf("expected 0 matches, got %d", len(result))
	}
}

func TestLeakToggle(t *testing.T) {
	app := &App{
		leakWatcher: model.NewLeakWatcher(60, 5),
		leakEnabled: true,
	}

	if !app.leakEnabled {
		t.Fatal("leak should start enabled")
	}

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	if app.leakEnabled {
		t.Error("leak should be disabled after pressing l")
	}
	if app.status != "leak watcher disabled" {
		t.Errorf("unexpected status: %q", app.status)
	}

	app.handleListKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})

	if !app.leakEnabled {
		t.Error("leak should be re-enabled after pressing l again")
	}
	if app.status != "leak watcher enabled" {
		t.Errorf("unexpected status: %q", app.status)
	}
}

func TestFilteredThreads_Sorted(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 2, Name: "Thread 2"},
		{Index: 0, Name: "Thread 0"},
		{Index: 1, Name: "Thread 1"},
	})
	app.sortBy = model.SortByIndex

	result := app.filteredThreads()
	if result[0].Index != 0 || result[1].Index != 1 || result[2].Index != 2 {
		t.Error("filteredThreads should return sorted results")
	}
}
