package model

import (
	"math"
	"testing"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
)

func TestState_Update_CountsIdleBusy(t *testing.T) {
	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true},
				{Index: 1, IsWaiting: true},
				{Index: 2, IsWaiting: true},
				{Index: 3, IsBusy: true},
				{Index: 4, State: "inactive"},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	var s State
	s.Update(snap)

	if s.Derived.TotalBusy != 2 {
		t.Errorf("TotalBusy: expected 2, got %d", s.Derived.TotalBusy)
	}
	if s.Derived.TotalIdle != 2 {
		t.Errorf("TotalIdle: expected 2, got %d", s.Derived.TotalIdle)
	}
}

func TestState_Update_CrashCount(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"a": {Crashes: 3},
				"b": {Crashes: 1},
			},
		},
	}

	var s State
	s.Update(snap)

	if s.Derived.TotalCrashes != 4 {
		t.Errorf("TotalCrashes: expected 4, got %v", s.Derived.TotalCrashes)
	}
}

func TestState_Update_RPSAndAvgTime(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 100, RequestTime: 10.0},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 200, RequestTime: 20.0},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	// 100 requests in 2 seconds = 50 RPS
	if math.Abs(s.Derived.RPS-50) > 0.5 {
		t.Errorf("RPS: expected ~50, got %v", s.Derived.RPS)
	}

	// 10s of request time for 100 requests = 100ms avg
	if math.Abs(s.Derived.AvgTime-100) > 1 {
		t.Errorf("AvgTime: expected ~100ms, got %v", s.Derived.AvgTime)
	}
}

func TestState_Update_NoPreviousSnapshot(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 100},
			},
		},
	}

	var s State
	s.Update(snap)

	if s.Derived.RPS != 0 {
		t.Errorf("RPS should be 0 without previous snapshot, got %v", s.Derived.RPS)
	}
}

func TestState_Update_CaddyFallbackRPS(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestDurationCount: 100,
			HTTPRequestDurationSum:   5.0,
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestDurationCount: 300,
			HTTPRequestDurationSum:   15.0,
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	// 200 requests in 2 seconds = 100 RPS
	if math.Abs(s.Derived.RPS-100) > 0.5 {
		t.Errorf("RPS (Caddy fallback): expected ~100, got %v", s.Derived.RPS)
	}

	// 10s of request time for 200 requests = 50ms avg
	if math.Abs(s.Derived.AvgTime-50) > 1 {
		t.Errorf("AvgTime (Caddy fallback): expected ~50ms, got %v", s.Derived.AvgTime)
	}
}

func TestState_Update_FrankenPHPTakesPriorityOverCaddy(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 100, RequestTime: 10.0},
			},
			HTTPRequestDurationCount: 500,
			HTTPRequestDurationSum:   50.0,
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 200, RequestTime: 20.0},
			},
			HTTPRequestDurationCount: 1000,
			HTTPRequestDurationSum:   100.0,
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	// FrankenPHP: 100 reqs in 1s = 100 RPS, avg = 10s/100 = 100ms
	// Caddy would give: 500 reqs in 1s = 500 RPS, this should NOT be used
	if math.Abs(s.Derived.RPS-100) > 0.5 {
		t.Errorf("RPS should use FrankenPHP metrics (100), got %v", s.Derived.RPS)
	}
}

func TestFormatUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h 30m"},
		{25 * time.Hour, "1d 1h"},
		{3*24*time.Hour + 14*time.Hour, "3d 14h"},
	}

	for _, tt := range tests {
		got := FormatUptime(tt.d)
		if got != tt.want {
			t.Errorf("FormatUptime(%v): expected %q, got %q", tt.d, tt.want, got)
		}
	}
}

func TestSortField_Next(t *testing.T) {
	s := SortByIndex
	seen := make(map[SortField]bool)
	for range 7 {
		seen[s] = true
		s = s.Next()
	}
	if len(seen) != 7 {
		t.Errorf("Next() should cycle through 7 values, got %d", len(seen))
	}
	if s != SortByIndex {
		t.Errorf("Next() should cycle back to SortByIndex, got %v", s)
	}
}

func TestSortField_Prev(t *testing.T) {
	s := SortByIndex
	seen := make(map[SortField]bool)
	for range 7 {
		seen[s] = true
		s = s.Prev()
	}
	if len(seen) != 7 {
		t.Errorf("Prev() should cycle through 7 values, got %d", len(seen))
	}
	if s != SortByIndex {
		t.Errorf("Prev() should cycle back to SortByIndex, got %v", s)
	}
}

func TestSortField_NextPrev_Inverse(t *testing.T) {
	for _, start := range sortFieldOrder {
		if start.Next().Prev() != start {
			t.Errorf("Next().Prev() should return to %v, got %v", start, start.Next().Prev())
		}
		if start.Prev().Next() != start {
			t.Errorf("Prev().Next() should return to %v, got %v", start, start.Prev().Next())
		}
	}
}
