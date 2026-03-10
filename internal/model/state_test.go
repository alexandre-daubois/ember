package model

import (
	"testing"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
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

	assert.Equal(t, 2, s.Derived.TotalBusy)
	assert.Equal(t, 2, s.Derived.TotalIdle)
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

	assert.Equal(t, float64(4), s.Derived.TotalCrashes)
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
	assert.InDelta(t, 50, s.Derived.RPS, 0.5)
	// 10s of request time for 100 requests = 100ms avg
	assert.InDelta(t, 100, s.Derived.AvgTime, 1)
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

	assert.Equal(t, float64(0), s.Derived.RPS)
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
	assert.InDelta(t, 100, s.Derived.RPS, 0.5)
	// 10s of request time for 200 requests = 50ms avg
	assert.InDelta(t, 50, s.Derived.AvgTime, 1)
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

	// FrankenPHP: 100 reqs in 1s = 100 RPS
	// Caddy would give: 500 reqs in 1s = 500 RPS, this should NOT be used
	assert.InDelta(t, 100, s.Derived.RPS, 0.5)
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
		assert.Equal(t, tt.want, got, "FormatUptime(%v)", tt.d)
	}
}

func TestSortField_Next(t *testing.T) {
	s := SortByIndex
	seen := make(map[SortField]bool)
	for range 7 {
		seen[s] = true
		s = s.Next()
	}
	assert.Len(t, seen, 7)
	assert.Equal(t, SortByIndex, s, "Next() should cycle back to SortByIndex")
}

func TestSortField_Prev(t *testing.T) {
	s := SortByIndex
	seen := make(map[SortField]bool)
	for range 7 {
		seen[s] = true
		s = s.Prev()
	}
	assert.Len(t, seen, 7)
	assert.Equal(t, SortByIndex, s, "Prev() should cycle back to SortByIndex")
}

func TestSortField_NextPrev_Inverse(t *testing.T) {
	for _, start := range sortFieldOrder {
		assert.Equal(t, start, start.Next().Prev(), "Next().Prev() should return to %v", start)
		assert.Equal(t, start, start.Prev().Next(), "Prev().Next() should return to %v", start)
	}
}
