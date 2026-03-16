package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	panel := renderDetailPanel(thread, 44, 25, nil)

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
	panel := renderDetailPanel(thread, 44, 25, nil)

	assert.Contains(t, panel, "Thread #2")
	assert.Contains(t, panel, "IDLE")
	assert.Contains(t, panel, "5000ms")
}

func TestRenderDetailPanel_NameTruncation(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index: 0,
		Name:  strings.Repeat("A", 100),
	}
	panel := renderDetailPanel(thread, 30, 15, nil)

	assert.Contains(t, panel, "…")
}

func TestRenderDetailPanel_WorkerScript(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:  3,
		Name:   "Worker PHP Thread - /app/worker.php",
		IsBusy: true,
	}
	panel := renderDetailPanel(thread, 44, 25, nil)

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
	panel := renderDetailPanel(thread, 44, 25, nil)

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
	panel := renderDetailPanel(thread, 44, 25, nil)

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
	assert.Equal(t, "1.0h", formatDuration(time.Hour))
	assert.Equal(t, "1.0d", formatDuration(24*time.Hour))
	assert.Equal(t, "0ms", formatDuration(0))
	assert.Equal(t, "9000ms", formatDuration(9*time.Second))
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

func TestRenderDetailPanel_MemorySparkline(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:       1,
		Name:        "Thread",
		IsWaiting:   true,
		MemoryUsage: 10 * 1024 * 1024,
	}
	samples := []int64{5 * 1024 * 1024, 6 * 1024 * 1024, 8 * 1024 * 1024, 10 * 1024 * 1024}
	panel := renderDetailPanel(thread, 44, 25, samples)

	assert.Contains(t, panel, "10 MB")
	plain := stripANSI(panel)
	for _, block := range []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"} {
		if strings.Contains(plain, block) {
			return
		}
	}
	t.Error("expected sparkline blocks in detail panel")
}

func TestRenderDetailPanel_NoSparklineWithoutSamples(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:       1,
		Name:        "Thread",
		IsWaiting:   true,
		MemoryUsage: 10 * 1024 * 1024,
	}
	panel := renderDetailPanel(thread, 44, 25, nil)
	plain := stripANSI(panel)
	for _, block := range []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇"} {
		assert.NotContains(t, plain, block, "should not have sparkline blocks without samples")
	}
}

func TestRenderHostDetailPanel_BasicInfo(t *testing.T) {
	h := model.HostDerived{
		Host:          "api.example.com",
		RPS:           42.5,
		InFlight:      3,
		AvgTime:       12.5,
		TotalRequests: 5000,
	}
	panel := renderHostDetailPanel(h, 44, 30)

	assert.Contains(t, panel, "api.example.com")
	assert.Contains(t, panel, "Traffic")
	assert.Contains(t, panel, "42/s")
	assert.Contains(t, panel, "5,000")
	assert.Contains(t, panel, "Latency")
	assert.Contains(t, panel, "12.5ms")
}

func TestRenderHostDetailPanel_WithPercentiles(t *testing.T) {
	h := model.HostDerived{
		Host:           "test.com",
		HasPercentiles: true,
		P50:            5.0,
		P90:            15.0,
		P95:            25.0,
		P99:            50.0,
		AvgTime:        10.0,
	}
	panel := renderHostDetailPanel(h, 44, 30)

	assert.Contains(t, panel, "P50")
	assert.Contains(t, panel, "5.0ms")
	assert.Contains(t, panel, "P90")
	assert.Contains(t, panel, "15.0ms")
	assert.Contains(t, panel, "P95")
	assert.Contains(t, panel, "25.0ms")
	assert.Contains(t, panel, "P99")
	assert.Contains(t, panel, "50.0ms")
}

func TestRenderHostDetailPanel_NoPercentiles(t *testing.T) {
	h := model.HostDerived{
		Host:           "test.com",
		HasPercentiles: false,
		AvgTime:        0,
	}
	panel := renderHostDetailPanel(h, 44, 30)

	assert.Contains(t, panel, "Latency")
	assert.Contains(t, panel, "—")
}

func TestRenderHostDetailPanel_StatusCodes(t *testing.T) {
	h := model.HostDerived{
		Host: "test.com",
		StatusCodes: map[int]float64{
			200: 40.0,
			301: 5.0,
			404: 2.5,
			500: 0.5,
		},
	}
	panel := renderHostDetailPanel(h, 44, 30)

	assert.Contains(t, panel, "Status Codes")
	assert.Contains(t, panel, "200")
	assert.Contains(t, panel, "301")
	assert.Contains(t, panel, "404")
	assert.Contains(t, panel, "500")
}

func TestRenderHostDetailPanel_Methods(t *testing.T) {
	h := model.HostDerived{
		Host:        "test.com",
		MethodRates: map[string]float64{"GET": 40.0, "POST": 10.0},
	}
	panel := renderHostDetailPanel(h, 44, 30)

	assert.Contains(t, panel, "Methods")
	assert.Contains(t, panel, "GET")
	assert.Contains(t, panel, "POST")
	assert.Contains(t, panel, "80%")
	assert.Contains(t, panel, "20%")
}

func TestRenderHostDetailPanel_ResponseSize(t *testing.T) {
	h := model.HostDerived{
		Host:            "test.com",
		AvgResponseSize: 4096,
	}
	panel := renderHostDetailPanel(h, 44, 30)

	assert.Contains(t, panel, "Response Size")
	assert.Contains(t, panel, "4 KB")
}

func TestRenderHostDetailPanel_StarHost(t *testing.T) {
	h := model.HostDerived{
		Host: "*",
	}
	panel := renderHostDetailPanel(h, 44, 30)

	assert.Contains(t, panel, "* (All traffic)")
}

func TestRenderHostDetailPanel_Footer(t *testing.T) {
	h := model.HostDerived{Host: "test.com"}
	panel := renderHostDetailPanel(h, 44, 30)

	assert.Contains(t, panel, "Esc")
	assert.Contains(t, panel, "close")
}

func TestHostDetailPanel_SideLayout(t *testing.T) {
	app := newAppWithHosts([]model.HostDerived{
		{Host: "example.com", RPS: 10, StatusCodes: map[int]float64{200: 10}},
		{Host: "api.com", RPS: 5, StatusCodes: map[int]float64{200: 5}},
	})
	app.mode = viewDetail
	app.width = 130
	app.height = 30

	view := app.View()
	require.NotEmpty(t, view)
	assert.Contains(t, view, "example.com")
	assert.Contains(t, view, "Traffic")
}

func TestHostDetailPanel_BottomLayout(t *testing.T) {
	app := newAppWithHosts([]model.HostDerived{
		{Host: "example.com", RPS: 10, StatusCodes: map[int]float64{200: 10}},
	})
	app.mode = viewDetail
	app.width = 70
	app.height = 30

	view := app.View()
	require.NotEmpty(t, view)
	assert.Contains(t, view, "example.com")
}

func TestHostDetailPanel_EnterOpensDetail(t *testing.T) {
	app := newAppWithHosts([]model.HostDerived{
		{Host: "example.com", StatusCodes: map[int]float64{}},
	})
	app.mode = viewList

	app.handleListKey(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Equal(t, viewDetail, app.mode)
}

func TestHostDetailPanel_RestartNotAvailable(t *testing.T) {
	app := newAppWithHosts([]model.HostDerived{
		{Host: "example.com", StatusCodes: map[int]float64{}},
	})
	app.mode = viewDetail

	app.handleDetailKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	assert.Equal(t, viewDetail, app.mode, "r should not trigger restart on Caddy tab")
}

func newAppWithHosts(hosts []model.HostDerived) *App {
	hostMetrics := make(map[string]*fetcher.HostMetrics)
	for _, h := range hosts {
		hostMetrics[h.Host] = &fetcher.HostMetrics{
			Host:        h.Host,
			StatusCodes: make(map[int]float64),
			Methods:     make(map[string]float64),
		}
	}
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers:        map[string]*fetcher.WorkerMetrics{},
			Hosts:          hostMetrics,
			HasHTTPMetrics: true,
		},
	}
	app := &App{
		activeTab: TabCaddy,
		tabs:      []Tab{TabCaddy},
		tabStates: map[Tab]*tabState{TabCaddy: {}},
		width:     120,
		height:    30,
		history:   newHistoryStore(),
	}
	app.state.Update(snap)
	app.state.HostDerived = hosts
	return app
}
