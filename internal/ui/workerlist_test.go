package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/alexandredaubois/ember/internal/model"
)

func TestSortThreads_ByIndex(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 3},
		{Index: 1},
		{Index: 2},
	}
	sorted := sortThreads(threads, model.SortByIndex)
	for i, s := range sorted {
		if s.Index != i+1 {
			t.Errorf("position %d: expected index %d, got %d", i, i+1, s.Index)
		}
	}
}

func TestSortThreads_ByState(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, IsWaiting: true},
		{Index: 1, IsBusy: true},
		{Index: 2, State: "inactive"},
	}
	sorted := sortThreads(threads, model.SortByState)

	if !sorted[0].IsBusy {
		t.Error("first should be busy")
	}
	if !sorted[1].IsWaiting {
		t.Error("second should be idle")
	}
	if sorted[2].IsBusy || sorted[2].IsWaiting {
		t.Error("third should be inactive")
	}
}

func TestSortThreads_ByMemory(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, MemoryUsage: 100},
		{Index: 1, MemoryUsage: 300},
		{Index: 2, MemoryUsage: 200},
	}
	sorted := sortThreads(threads, model.SortByMemory)

	if sorted[0].MemoryUsage != 300 {
		t.Errorf("first should have highest memory, got %d", sorted[0].MemoryUsage)
	}
	if sorted[2].MemoryUsage != 100 {
		t.Errorf("last should have lowest memory, got %d", sorted[2].MemoryUsage)
	}
}

func TestSortThreads_PreservesOriginal(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 3},
		{Index: 1},
	}
	sortThreads(threads, model.SortByIndex)

	if threads[0].Index != 3 {
		t.Error("original slice should not be modified")
	}
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

	if !strings.Contains(row, "POST") {
		t.Error("row should contain method POST")
	}
	if !strings.Contains(row, "/api/v1/users") {
		t.Error("row should contain URI")
	}
	if !strings.Contains(row, "18 MB") {
		t.Error("row should contain memory")
	}
	if !strings.Contains(row, "4,201") {
		t.Error("row should contain formatted request count")
	}
}

func TestFormatThreadRow_IdleShowsDashes(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:     1,
		IsWaiting: true,
	}
	row := formatThreadRow(thread, 120, renderOpts{}, false)

	// method and URI should be dashes for idle threads
	if !strings.Contains(row, "—") {
		t.Error("idle row should contain dash placeholders")
	}
}

func TestFormatThreadRow_URITruncation(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		Index:      0,
		IsBusy:     true,
		CurrentURI: "/api/v1/very/long/path/that/exceeds/limit",
	}
	row := formatThreadRow(thread, 120, renderOpts{}, false)

	if strings.Contains(row, "exceeds/limit") {
		t.Error("long URI should be truncated")
	}
	if !strings.Contains(row, "…") {
		t.Error("truncated URI should end with ellipsis")
	}
}

func TestSortThreads_ByRequests(t *testing.T) {
	threads := []fetcher.ThreadDebugState{
		{Index: 0, RequestCount: 100},
		{Index: 1, RequestCount: 500},
		{Index: 2, RequestCount: 250},
	}
	sorted := sortThreads(threads, model.SortByRequests)

	if sorted[0].RequestCount != 500 {
		t.Errorf("first should have highest requests, got %d", sorted[0].RequestCount)
	}
	if sorted[2].RequestCount != 100 {
		t.Errorf("last should have lowest requests, got %d", sorted[2].RequestCount)
	}
}

func TestFormatTime_Idle(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		IsWaiting:                true,
		WaitingSinceMilliseconds: 3200,
	}
	got := formatTime(thread)
	if got != "3.2s idle" {
		t.Errorf("expected '3.2s idle', got %q", got)
	}
}

func TestFormatTime_IdleSubSecond(t *testing.T) {
	thread := fetcher.ThreadDebugState{
		IsWaiting:                true,
		WaitingSinceMilliseconds: 500,
	}
	got := formatTime(thread)
	if got != "500ms idle" {
		t.Errorf("expected '500ms idle', got %q", got)
	}
}

func TestFormatTime_NoInfo(t *testing.T) {
	thread := fetcher.ThreadDebugState{State: "inactive"}
	got := formatTime(thread)
	if got != "—" {
		t.Errorf("expected '—', got %q", got)
	}
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

	if sorted[0].Index != 1 {
		t.Errorf("first should be idle thread with 5000ms, got index %d", sorted[0].Index)
	}
	if sorted[1].Index != 2 {
		t.Errorf("second should be busy thread running 3s, got index %d", sorted[1].Index)
	}
	if sorted[2].Index != 0 {
		t.Errorf("third should be busy thread running 100ms, got index %d", sorted[2].Index)
	}
	if sorted[3].Index != 3 {
		t.Errorf("last should be inactive thread, got index %d", sorted[3].Index)
	}
}

func TestThreadElapsedMs_Busy(t *testing.T) {
	started := time.Now().Add(-2 * time.Second).UnixMilli()
	thread := fetcher.ThreadDebugState{IsBusy: true, RequestStartedAt: started}
	elapsed := threadElapsedMs(thread)
	if elapsed < 1900 || elapsed > 2500 {
		t.Errorf("expected ~2000ms, got %d", elapsed)
	}
}

func TestThreadElapsedMs_Idle(t *testing.T) {
	thread := fetcher.ThreadDebugState{IsWaiting: true, WaitingSinceMilliseconds: 4200}
	elapsed := threadElapsedMs(thread)
	if elapsed != 4200 {
		t.Errorf("expected 4200, got %d", elapsed)
	}
}

func TestThreadElapsedMs_Inactive(t *testing.T) {
	thread := fetcher.ThreadDebugState{State: "inactive"}
	elapsed := threadElapsedMs(thread)
	if elapsed != 0 {
		t.Errorf("expected 0, got %d", elapsed)
	}
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
		got := formatNumber(tt.input)
		if got != tt.want {
			t.Errorf("formatNumber(%d): expected %q, got %q", tt.input, tt.want, got)
		}
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
		if groups[i] < groups[i-1] {
			t.Errorf("groups not contiguous: %v", groups)
			break
		}
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
		if got != tt.want {
			t.Errorf("threadGroup(%q): expected %q, got %q", tt.name, tt.want, got)
		}
	}
}
