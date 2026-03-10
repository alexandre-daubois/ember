package ui

import (
	"testing"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSortThreads_ByIndex(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 3},
		{Index: 1},
		{Index: 2},
	}
	sorted := sortThreads(threads, model.SortByIndex)
	for i, s := range sorted {
		assert.Equal(t, i+1, s.Index, "position %d", i)
	}
}

func TestSortThreads_ByState(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, IsWaiting: true},
		{Index: 1, IsBusy: true},
		{Index: 2, State: "inactive"},
	}
	sorted := sortThreads(threads, model.SortByState)

	assert.True(t, sorted[0].IsBusy, "first should be busy")
	assert.True(t, sorted[1].IsWaiting, "second should be idle")
	assert.False(t, sorted[2].IsBusy || sorted[2].IsWaiting, "third should be inactive")
}

func TestSortThreads_ByMemory(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, MemoryUsage: 100},
		{Index: 1, MemoryUsage: 300},
		{Index: 2, MemoryUsage: 200},
	}
	sorted := sortThreads(threads, model.SortByMemory)

	assert.Equal(t, int64(300), sorted[0].MemoryUsage, "first should have highest memory")
	assert.Equal(t, int64(100), sorted[2].MemoryUsage, "last should have lowest memory")
}

func TestSortThreads_PreservesOriginal(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 3},
		{Index: 1},
	}
	sortThreads(threads, model.SortByIndex)

	assert.Equal(t, 3, threads[0].Index, "original slice should not be modified")
}

func TestFormatThreadRow_BusyWithRequestInfo(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:         0,
		IsBusy:        true,
		CurrentMethod: "POST",
		CurrentURI:    "/api/v1/users",
		MemoryUsage:   18 * 1024 * 1024,
		RequestCount:  4201,
	}
	row := formatThreadRow(thread, 120, renderOpts{}, false)

	assert.Contains(t, row, "POST")
	assert.Contains(t, row, "/api/v1/users")
	assert.Contains(t, row, "18 MB")
	assert.Contains(t, row, "4,201")
}

func TestFormatThreadRow_IdleShowsDashes(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:     1,
		IsWaiting: true,
	}
	row := formatThreadRow(thread, 120, renderOpts{}, false)

	assert.Contains(t, row, "—", "idle row should contain dash placeholders")
}

func TestFormatThreadRow_URITruncation(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:      0,
		IsBusy:     true,
		CurrentURI: "/api/v1/very/long/path/that/exceeds/limit",
	}
	row := formatThreadRow(thread, 120, renderOpts{}, false)

	assert.NotContains(t, row, "exceeds/limit", "long URI should be truncated")
	assert.Contains(t, row, "…", "truncated URI should end with ellipsis")
}

func TestSortThreads_ByMethod(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, CurrentMethod: "POST"},
		{Index: 1, CurrentMethod: "GET"},
		{Index: 2, CurrentMethod: ""},
	}
	sorted := sortThreads(threads, model.SortByMethod)

	assert.Equal(t, "", sorted[0].CurrentMethod, "first should be empty method")
	assert.Equal(t, "GET", sorted[1].CurrentMethod, "second should be GET")
	assert.Equal(t, "POST", sorted[2].CurrentMethod, "third should be POST")
}

func TestSortThreads_ByURI(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, CurrentURI: "/api/z"},
		{Index: 1, CurrentURI: "/api/a"},
		{Index: 2, CurrentURI: ""},
	}
	sorted := sortThreads(threads, model.SortByURI)

	assert.Equal(t, "", sorted[0].CurrentURI, "first should be empty URI")
	assert.Equal(t, "/api/a", sorted[1].CurrentURI, "second should be /api/a")
	assert.Equal(t, "/api/z", sorted[2].CurrentURI, "third should be /api/z")
}

func TestSortThreads_ByRequests(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, RequestCount: 100},
		{Index: 1, RequestCount: 500},
		{Index: 2, RequestCount: 250},
	}
	sorted := sortThreads(threads, model.SortByRequests)

	assert.Equal(t, int64(500), sorted[0].RequestCount, "first should have highest requests")
	assert.Equal(t, int64(100), sorted[2].RequestCount, "last should have lowest requests")
}

func TestFormatTime_Idle(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		IsWaiting:                true,
		WaitingSinceMilliseconds: 3200,
	}
	assert.Equal(t, "3.2s idle", formatTime(thread))
}

func TestFormatTime_IdleSubSecond(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		IsWaiting:                true,
		WaitingSinceMilliseconds: 500,
	}
	assert.Equal(t, "500ms idle", formatTime(thread))
}

func TestFormatTime_NoInfo(t *testing.T) {
	thread := fetcher.ThreadDebugState{State: "inactive"}
	assert.Equal(t, "—", formatTime(thread))
}

func TestSortThreads_ByTime(t *testing.T) {
	now := time.Now()
	threads := []fetcher.ThreadDebugState{
		{Index: 0, IsBusy: true, RequestStartedAt: now.Add(-100 * time.Millisecond).UnixMilli()},
		{Index: 1, IsWaiting: true, WaitingSinceMilliseconds: 5000},
		{Index: 2, IsBusy: true, RequestStartedAt: now.Add(-3 * time.Second).UnixMilli()},
		{Index: 3, State: "inactive"},
	}
	sorted := sortThreads(threads, model.SortByTime)

	assert.Equal(t, 1, sorted[0].Index, "first should be idle thread with 5000ms")
	assert.Equal(t, 2, sorted[1].Index, "second should be busy thread running 3s")
	assert.Equal(t, 0, sorted[2].Index, "third should be busy thread running 100ms")
	assert.Equal(t, 3, sorted[3].Index, "last should be inactive thread")
}

func TestThreadElapsedMs_Busy(t *testing.T) {
	started := time.Now().Add(-2 * time.Second).UnixMilli()
	thread := fetcher.ThreadDebugState{IsBusy: true, RequestStartedAt: started}
	elapsed := threadElapsedMs(thread)
	assert.InDelta(t, 2000, elapsed, 500)
}

func TestThreadElapsedMs_Idle(t *testing.T) {
	thread := fetcher.ThreadDebugState{IsWaiting: true, WaitingSinceMilliseconds: 4200}
	assert.Equal(t, int64(4200), threadElapsedMs(thread))
}

func TestThreadElapsedMs_Inactive(t *testing.T) {
	thread := fetcher.ThreadDebugState{State: "inactive"}
	assert.Equal(t, int64(0), threadElapsedMs(thread))
}

func TestFormatNumber(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{42, "42"},
		{999, "999"},
		{1000, "1,000"},
		{1234567, "1,234,567"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, formatNumber(tt.input), "formatNumber(%d)", tt.input)
	}
}

func TestSortThreads_GroupsByScript(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, Name: "Regular PHP Thread"},
		{Index: 1, Name: "Worker PHP Thread - /app/api.php"},
		{Index: 2, Name: "Worker PHP Thread - /app/worker.php"},
		{Index: 3, Name: "Regular PHP Thread"},
	}
	sorted := sortThreads(threads, model.SortByIndex)

	groups := make([]string, len(sorted))
	for i, s := range sorted {
		groups[i] = threadGroup(s)
	}

	for i := 1; i < len(groups); i++ {
		require.GreaterOrEqual(t, groups[i], groups[i-1], "groups not contiguous: %v", groups)
	}
}

func TestThreadGroup(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Worker PHP Thread - /app/worker.php", "(Worker script) /app/worker.php"},
		{"Regular PHP Thread", "threads"},
		{"", "threads"},
	}
	for _, tt := range tests {
		got := threadGroup(fetcher.ThreadDebugState{Name: tt.name})
		assert.Equal(t, tt.want, got, "threadGroup(%q)", tt.name)
	}
}
