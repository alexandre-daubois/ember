package ui

import (
	"strings"
	"testing"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tea "github.com/charmbracelet/bubbletea"
)

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0 KB"},
		{512, "0 KB"},
		{1024, "1 KB"},
		{1048576, "1 MB"},
		{10485760, "10 MB"},
		{536870912, "512 MB"},
	}

	for _, tt := range tests {
		got := formatBytes(tt.input)
		assert.Equal(t, tt.want, got, "formatBytes(%d)", tt.input)
	}
}

func TestTruncateURI(t *testing.T) {
	assert.Equal(t, "/short", truncateURI("/short", 20))
	assert.Equal(t, "/api/v1/very/long…", truncateURI("/api/v1/very/long/path/here", 18))
	assert.Equal(t, "/ab", truncateURI("/abcdef", 3))
}

func TestRenderDetailPanel_ContainsThreadInfo(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:        5,
		Name:         "Worker PHP Thread - /app/worker.php",
		IsBusy:       true,
		CurrentMethod: "POST",
		CurrentURI:   "/api/users",
		MemoryUsage:  10 * 1024 * 1024,
		RequestCount: 1234,
	}
	panel := renderDetailPanel(thread, model.LeakStatus{}, 40, 20)

	assert.Contains(t, panel, "Thread #5")
	assert.Contains(t, panel, "POST")
	assert.Contains(t, panel, "/api/users")
	assert.Contains(t, panel, "10 MB")
	assert.Contains(t, panel, "1,234")
	assert.Contains(t, panel, "busy")
}

func TestRenderDetailPanel_IdleThread(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:                    2,
		Name:                     "Regular PHP Thread",
		IsWaiting:                true,
		WaitingSinceMilliseconds: 5000,
	}
	panel := renderDetailPanel(thread, model.LeakStatus{}, 40, 20)

	assert.Contains(t, panel, "Thread #2")
	assert.Contains(t, panel, "idle")
	assert.Contains(t, panel, "5.0s")
}

func TestRenderDetailPanel_LeakWarning(t *testing.T) {
	thread := fetcher.ThreadDebugState{Index: 0, IsWaiting: true}
	leaking := model.LeakStatus{Leaking: true, Slope: 100}
	panel := renderDetailPanel(thread, leaking, 40, 20)

	assert.Contains(t, panel, "leak")
}

func TestRenderDetailPanel_NameTruncation(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index: 0,
		Name:  strings.Repeat("A", 100),
	}
	panel := renderDetailPanel(thread, model.LeakStatus{}, 30, 10)

	assert.Contains(t, panel, "…")
}

func TestDetailPanel_SideLayout(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0", IsWaiting: true},
		{Index: 1, Name: "Thread 1", IsBusy: true},
	})
	app.mode = viewDetail
	app.width = 120
	app.height = 30
	app.leakWatcher = model.NewLeakWatcher(10, 5)

	view := app.View()
	require.NotEmpty(t, view)

	assert.Contains(t, view, "Thread #0")
}

func TestDetailPanel_BottomLayout(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0", IsWaiting: true},
		{Index: 1, Name: "Thread 1", IsBusy: true},
	})
	app.mode = viewDetail
	app.width = 70 // below detailSideThreshold
	app.height = 30
	app.leakWatcher = model.NewLeakWatcher(10, 5)

	view := app.View()
	require.NotEmpty(t, view)

	assert.Contains(t, view, "Thread #0")
}

func TestDetailPanel_NavigateWithUpDown(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0"},
		{Index: 1, Name: "Thread 1"},
		{Index: 2, Name: "Thread 2"},
	})
	app.mode = viewDetail
	app.cursor = 0
	app.leakWatcher = model.NewLeakWatcher(10, 5)

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, app.cursor)

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, app.cursor)

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, app.cursor)
}

func TestDetailPanel_EscClosesPanel(t *testing.T) {
	app := &App{
		mode:        viewDetail,
		leakWatcher: model.NewLeakWatcher(10, 5),
	}

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, viewList, app.mode)
}
