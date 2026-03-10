package ui

import (
	"strings"
	"testing"

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
