package ui

import (
	"strings"
	"testing"

	"github.com/alexandredaubois/ember/internal/fetcher"
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
		Index:         5,
		Name:          "Worker PHP Thread - /app/worker.php",
		IsBusy:        true,
		CurrentMethod: "POST",
		CurrentURI:    "/api/users",
		MemoryUsage:   10 * 1024 * 1024,
		RequestCount:  1234,
	}
	panel := renderDetailPanel(thread, 44, 25)

	assert.Contains(t, panel, "Thread #5")
	assert.Contains(t, panel, "POST")
	assert.Contains(t, panel, "/api/users")
	assert.Contains(t, panel, "10 MB")
	assert.Contains(t, panel, "1,234")
	assert.Contains(t, panel, "BUSY")
}

func TestRenderDetailPanel_IdleThread(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:                    2,
		Name:                     "Regular PHP Thread",
		IsWaiting:                true,
		WaitingSinceMilliseconds: 5000,
	}
	panel := renderDetailPanel(thread, 44, 25)

	assert.Contains(t, panel, "Thread #2")
	assert.Contains(t, panel, "IDLE")
	assert.Contains(t, panel, "5000ms")
}

func TestRenderDetailPanel_NameTruncation(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index: 0,
		Name:  strings.Repeat("A", 100),
	}
	panel := renderDetailPanel(thread, 30, 15)

	assert.Contains(t, panel, "…")
}

func TestRenderDetailPanel_WorkerScript(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:   3,
		Name:    "Worker PHP Thread - /app/worker.php",
		IsBusy:  true,
	}
	panel := renderDetailPanel(thread, 44, 25)

	assert.Contains(t, panel, "Thread #3")
	assert.Contains(t, panel, "worker")
	assert.Contains(t, panel, "/app/worker.php")
}

func TestRenderDetailPanel_SectionHeaders(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:         1,
		Name:          "Thread",
		IsBusy:        true,
		CurrentMethod: "GET",
		CurrentURI:    "/test",
		MemoryUsage:   5 * 1024 * 1024,
		RequestCount:  42,
	}
	panel := renderDetailPanel(thread, 44, 25)

	assert.Contains(t, panel, "Request")
	assert.Contains(t, panel, "Resources")
}

func TestRenderDetailPanel_Memory(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:       1,
		Name:        "Thread",
		IsWaiting:   true,
		MemoryUsage: 5 * 1024 * 1024,
	}
	panel := renderDetailPanel(thread, 44, 25)

	assert.Contains(t, panel, "5 MB")
}

func TestDetailPanel_SideLayout(t *testing.T) {
	app := newAppWithThreads([]fetcher.ThreadDebugState{
		{Index: 0, Name: "Thread 0", IsWaiting: true},
		{Index: 1, Name: "Thread 1", IsBusy: true},
	})
	app.mode = viewDetail
	app.width = 130
	app.height = 30

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
	app.width = 70
	app.height = 30

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

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 1, app.cursor)

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, app.cursor)

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, app.cursor)
}

func TestDetailPanel_EscClosesPanel(t *testing.T) {
	app := &App{
		mode: viewDetail,
	}

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Equal(t, viewList, app.mode)
}

func TestRenderStateBadge(t *testing.T) {
	busy := renderStateBadge(fetcher.ThreadDebugState{IsBusy: true})
	assert.Contains(t, busy, "BUSY")

	idle := renderStateBadge(fetcher.ThreadDebugState{IsWaiting: true})
	assert.Contains(t, idle, "IDLE")

	other := renderStateBadge(fetcher.ThreadDebugState{State: "starting"})
	assert.Contains(t, other, "STARTING")
}

func TestFormatDuration(t *testing.T) {
	assert.Equal(t, "500ms", formatDuration(500*1e6))
	assert.Equal(t, "1500ms", formatDuration(1500*1e6))
	assert.Equal(t, "10.0s", formatDuration(10000*1e6))
	assert.Equal(t, "2.0m", formatDuration(120*1e9))
}

func TestRenderMemSparkline_Empty(t *testing.T) {
	assert.Equal(t, "", renderMemSparkline(nil, 10))
	assert.Equal(t, "", renderMemSparkline([]int64{100}, 10))
}

func TestRenderMemSparkline_Trend(t *testing.T) {
	samples := []int64{100, 200, 300, 400, 500}
	result := renderMemSparkline(samples, 10)
	assert.NotEmpty(t, result)
}
