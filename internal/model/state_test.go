package model

import (
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

var dummyThreads = fetcher.ThreadsResponse{
	ThreadDebugStates: []fetcher.ThreadDebugState{{Index: 0, IsWaiting: true}},
}

func TestState_Update_RPSAndAvgTime(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 100, RequestTime: 10.0},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads:   dummyThreads,
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
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestDurationCount: 100,
			HTTPRequestDurationSum:   5.0,
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads:   dummyThreads,
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
		Threads:   dummyThreads,
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
		Threads:   dummyThreads,
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

func TestState_Update_NoSpikeAfterStaleMetrics(t *testing.T) {
	now := time.Now()

	// normal snapshot with real metrics
	good := &fetcher.Snapshot{
		FetchedAt: now.Add(-3 * time.Second),
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 1000000, RequestTime: 100.0},
			},
		},
	}

	// stale snapshot: metrics failed (Workers nil, counts zero)
	stale := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads:   fetcher.ThreadsResponse{},
		Metrics:   fetcher.MetricsSnapshot{},
	}

	// recovery: fresh data with much higher counters
	recovery := &fetcher.Snapshot{
		FetchedAt: now,
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 201000000, RequestTime: 20100.0},
			},
		},
	}

	var s State
	s.Update(good)

	// simulate stale: replace metrics and FetchedAt on Current (as app.go stale path does)
	s.Current.Metrics = stale.Metrics
	s.Current.FetchedAt = stale.FetchedAt

	s.Update(recovery)

	// prevCount is 0 (stale metrics), so RPS must be 0, NOT 200M
	assert.Equal(t, float64(0), s.Derived.RPS, "RPS should be 0 after stale recovery, not a spike")
}

func TestState_Update_NoSpikeAfterStaleMetricsWithData(t *testing.T) {
	now := time.Now()

	// normal snapshot
	good := &fetcher.Snapshot{
		FetchedAt: now.Add(-3 * time.Second),
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 1000000, RequestTime: 100.0},
			},
		},
	}

	// stale but metrics endpoint succeeded, counters are fresh
	staleFresh := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads:   fetcher.ThreadsResponse{},
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 200999950, RequestTime: 20099.0},
			},
		},
	}

	recovery := &fetcher.Snapshot{
		FetchedAt: now,
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 201000000, RequestTime: 20100.0},
			},
		},
	}

	var s State
	s.Update(good)

	// simulate a stale path: update metrics and FetchedAt to latest values
	s.Current.Metrics = staleFresh.Metrics
	s.Current.FetchedAt = staleFresh.FetchedAt

	s.Update(recovery)

	// delta = 201000000 - 200999950 = 50 reqs
	// dt = 1s (Previous.FetchedAt from staleFresh, Current.FetchedAt from recovery)
	// RPS = 50/1 = 50
	assert.InDelta(t, 50, s.Derived.RPS, 1, "RPS should reflect only the small delta, not the full gap")
}

func TestState_Update_NoSpikeWhenDtTooSmall(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-50 * time.Millisecond),
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 1000000, RequestTime: 100.0},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 1000100, RequestTime: 101.0},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	// dt = 50ms < 100ms threshold, rate computation should be skipped
	assert.Equal(t, float64(0), s.Derived.RPS, "RPS should be 0 when dt < 100ms")
	assert.Equal(t, float64(0), s.Derived.AvgTime, "AvgTime should be 0 when dt < 100ms")
}

func TestState_Update_BurstResponsesNoSpike(t *testing.T) {
	now := time.Now()

	// simulate burst: 3 snapshots arriving within milliseconds
	snap1 := &fetcher.Snapshot{
		FetchedAt: now.Add(-10 * time.Millisecond),
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 200000000, RequestTime: 20000.0},
			},
		},
	}

	snap2 := &fetcher.Snapshot{
		FetchedAt: now.Add(-5 * time.Millisecond),
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 200000100, RequestTime: 20001.0},
			},
		},
	}

	snap3 := &fetcher.Snapshot{
		FetchedAt: now,
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"w": {RequestCount: 200000200, RequestTime: 20002.0},
			},
		},
	}

	var s State
	s.Update(snap1)
	s.Update(snap2)

	assert.Equal(t, float64(0), s.Derived.RPS, "RPS should be 0 between burst responses (dt < 100ms)")

	s.Update(snap3)

	assert.Equal(t, float64(0), s.Derived.RPS, "RPS should still be 0 between burst responses")
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

func TestSortField_Next_InvalidValue(t *testing.T) {
	invalid := SortField(999)
	assert.Equal(t, SortByIndex, invalid.Next())
}

func TestSortField_Prev_InvalidValue(t *testing.T) {
	invalid := SortField(999)
	assert.Equal(t, SortByIndex, invalid.Prev())
}

func TestHostSortField_Next_InvalidValue(t *testing.T) {
	invalid := HostSortField(999)
	assert.Equal(t, SortByHost, invalid.Next())
}

func TestHostSortField_Prev_InvalidValue(t *testing.T) {
	invalid := HostSortField(999)
	assert.Equal(t, SortByHost, invalid.Prev())
}

func TestSortField_NextPrev_Inverse(t *testing.T) {
	for _, start := range sortFieldOrder {
		assert.Equal(t, start, start.Next().Prev(), "Next().Prev() should return to %v", start)
		assert.Equal(t, start, start.Prev().Next(), "Prev().Next() should return to %v", start)
	}
}

func TestState_Update_DetectsCompletedRequest(t *testing.T) {
	now := time.Now()
	reqStart := now.Add(-200 * time.Millisecond).UnixMilli()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestStartedAt: reqStart},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 100, RequestTime: 10.0},
		}},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsWaiting: true},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 101, RequestTime: 10.2},
		}},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.True(t, s.Derived.HasPercentiles)
	expectedMs := float64(now.UnixMilli() - reqStart)
	assert.InDelta(t, expectedMs, s.Derived.P50, 1)
}

func TestState_Update_DetectsRequestSwitch(t *testing.T) {
	now := time.Now()
	reqStart1 := now.Add(-500 * time.Millisecond).UnixMilli()
	reqStart2 := now.Add(-50 * time.Millisecond).UnixMilli()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestStartedAt: reqStart1},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 100, RequestTime: 10.0},
		}},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestStartedAt: reqStart2},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 101, RequestTime: 10.5},
		}},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.True(t, s.Derived.HasPercentiles)
	expectedMs := float64(now.UnixMilli() - reqStart1)
	assert.InDelta(t, expectedMs, s.Derived.P50, 1)
}

func TestState_Update_NoPreviousNoPercentiles(t *testing.T) {
	snap := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestStartedAt: time.Now().UnixMilli()},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	var s State
	s.Update(snap)

	assert.False(t, s.Derived.HasPercentiles)
}

func TestState_Update_StillBusySameRequest(t *testing.T) {
	now := time.Now()
	reqStart := now.Add(-500 * time.Millisecond).UnixMilli()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestStartedAt: reqStart},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 100, RequestTime: 10.0},
		}},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestStartedAt: reqStart},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 100, RequestTime: 10.0},
		}},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.False(t, s.Derived.HasPercentiles)
}

func TestState_Update_MultipleCompletedRequests(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestStartedAt: now.Add(-100 * time.Millisecond).UnixMilli()},
				{Index: 1, IsBusy: true, RequestStartedAt: now.Add(-200 * time.Millisecond).UnixMilli()},
				{Index: 2, IsBusy: true, RequestStartedAt: now.Add(-300 * time.Millisecond).UnixMilli()},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 100, RequestTime: 10.0},
		}},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsWaiting: true},
				{Index: 1, IsWaiting: true},
				{Index: 2, IsWaiting: true},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 103, RequestTime: 10.6},
		}},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.True(t, s.Derived.HasPercentiles)
	assert.Equal(t, 3, s.percentiles.count(now))
	assert.Greater(t, s.Derived.P50, 0.0, "P50 must be populated from tracker")
	assert.Greater(t, s.Derived.P90, 0.0, "P90 must be populated from tracker")
	assert.Greater(t, s.Derived.P95, 0.0, "P95 must be populated from tracker")
	assert.Greater(t, s.Derived.P99, 0.0, "P99 must be populated from tracker")
	assert.GreaterOrEqual(t, s.Derived.P90, s.Derived.P50, "P90 >= P50")
	assert.GreaterOrEqual(t, s.Derived.P95, s.Derived.P90, "P95 >= P90")
}

func TestState_Update_HistogramTakesPriorityOverThreadBased(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestStartedAt: now.Add(-5 * time.Second).UnixMilli()},
			},
		},
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			DurationBuckets: []fetcher.HistogramBucket{
				{UpperBound: 0.01, CumulativeCount: 0},
				{UpperBound: 0.05, CumulativeCount: 0},
				{UpperBound: 0.1, CumulativeCount: 0},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsWaiting: true},
			},
		},
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			DurationBuckets: []fetcher.HistogramBucket{
				{UpperBound: 0.01, CumulativeCount: 50},
				{UpperBound: 0.05, CumulativeCount: 90},
				{UpperBound: 0.1, CumulativeCount: 100},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.True(t, s.Derived.HasPercentiles)
	// Histogram says P50 falls in [0.01, 0.05] bucket → ~20ms
	// Thread-based would give ~5000ms (very different)
	// If histogram wins, P50 should be in the low ms range
	assert.Less(t, s.Derived.P50, 100.0, "should use histogram, not thread-based")
}

func TestState_Update_MidpointEstimation(t *testing.T) {
	now := time.Now()
	// Request started 3 seconds ago, well before both snapshots
	reqStart := now.Add(-3 * time.Second).UnixMilli()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestStartedAt: reqStart},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 100, RequestTime: 10.0},
		}},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsWaiting: true},
			},
		},
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{
			"w": {RequestCount: 101, RequestTime: 10.2},
		}},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.True(t, s.Derived.HasPercentiles)
	// Midpoint of [now-1s, now] = now-500ms
	// Duration = (now-500ms) - (now-3s) = 2500ms
	assert.InDelta(t, 2500, s.Derived.P50, 5)
}

func TestHostSortField_NextPrev_Cycle(t *testing.T) {
	s := SortByHost
	seen := make(map[HostSortField]bool)
	for range len(hostSortFieldOrder) {
		seen[s] = true
		s = s.Next()
	}
	assert.Len(t, seen, len(hostSortFieldOrder))
	assert.Equal(t, SortByHost, s, "Next() should cycle back")
}

func TestHostSortField_PrevNext_Inverse(t *testing.T) {
	for _, start := range hostSortFieldOrder {
		assert.Equal(t, start, start.Next().Prev(), "Next().Prev() should return to %v", start)
		assert.Equal(t, start, start.Prev().Next(), "Prev().Next() should return to %v", start)
	}
}

func TestHostSortField_String(t *testing.T) {
	tests := []struct {
		field HostSortField
		want  string
	}{
		{SortByHost, "host"},
		{SortByHostRPS, "rps"},
		{SortByHostAvg, "avg"},
		{SortByHostInFlight, "in-flight"},
		{SortByHost2xx, "2xx"},
		{SortByHost4xx, "4xx"},
		{SortByHost5xx, "5xx"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.field.String())
	}
}

func TestCertSortField_String(t *testing.T) {
	tests := []struct {
		field CertSortField
		want  string
	}{
		{SortByCertDomain, "domain"},
		{SortByCertExpiry, "expiry"},
		{SortByCertSource, "source"},
		{SortByCertIssuer, "issuer"},
		{CertSortField(99), "domain"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.field.String())
	}
}

func TestCertSortField_NextPrev_Cycle(t *testing.T) {
	s := SortByCertDomain
	seen := make(map[CertSortField]bool)
	for range len(certSortFieldOrder) {
		seen[s] = true
		s = s.Next()
	}
	assert.Len(t, seen, len(certSortFieldOrder))
	assert.Equal(t, SortByCertDomain, s, "Next() should cycle back")
}

func TestCertSortField_PrevNext_Inverse(t *testing.T) {
	for _, start := range certSortFieldOrder {
		assert.Equal(t, start, start.Next().Prev(), "Next().Prev() should return to %v", start)
		assert.Equal(t, start, start.Prev().Next(), "Prev().Next() should return to %v", start)
	}
}

func TestCertSortField_Next_InvalidValue(t *testing.T) {
	assert.Equal(t, SortByCertDomain, CertSortField(999).Next())
}

func TestCertSortField_Prev_InvalidValue(t *testing.T) {
	assert.Equal(t, SortByCertDomain, CertSortField(999).Prev())
}

func TestComputeStatusCodeRates(t *testing.T) {
	curr := map[int]float64{200: 100, 404: 20, 500: 5}
	prev := map[int]float64{200: 80, 404: 10}
	dt := 2.0

	rates := computeStatusCodeRates(curr, prev, dt)
	assert.InDelta(t, 10, rates[200], 0.01)  // (100-80)/2
	assert.InDelta(t, 5, rates[404], 0.01)   // (20-10)/2
	assert.InDelta(t, 2.5, rates[500], 0.01) // (5-0)/2
}

func TestComputeStatusCodeRates_Empty(t *testing.T) {
	assert.Nil(t, computeStatusCodeRates(nil, nil, 1.0))
	assert.Nil(t, computeStatusCodeRates(map[int]float64{}, map[int]float64{}, 1.0))
	assert.Nil(t, computeStatusCodeRates(map[int]float64{200: 10}, nil, 0))
}

func TestComputeStatusCodeRates_NoDelta(t *testing.T) {
	same := map[int]float64{200: 50}
	assert.Nil(t, computeStatusCodeRates(same, same, 1.0))
}

func TestComputeHostDerived_RPSAndAvg(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"example.com": {
					Host:          "example.com",
					DurationCount: 100,
					DurationSum:   5.0,
					InFlight:      3,
					StatusCodes:   map[int]float64{200: 80, 404: 20},
				},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"example.com": {
					Host:          "example.com",
					DurationCount: 200,
					DurationSum:   15.0,
					InFlight:      5,
					StatusCodes:   map[int]float64{200: 160, 404: 40},
				},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.Len(t, s.HostDerived, 1)
	hd := s.HostDerived[0]
	assert.Equal(t, "example.com", hd.Host)
	assert.InDelta(t, 50, hd.RPS, 0.5)    // 100 reqs / 2s
	assert.InDelta(t, 100, hd.AvgTime, 1) // (10s / 100 reqs) * 1000
	assert.Equal(t, float64(5), hd.InFlight)
	assert.InDelta(t, 40, hd.StatusCodes[200], 1) // (160-80)/2
	assert.InDelta(t, 10, hd.StatusCodes[404], 1) // (40-20)/2
}

func TestComputeHostDerived_WithPercentiles(t *testing.T) {
	now := time.Now()

	bucketsPrev := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 0},
		{UpperBound: 0.05, CumulativeCount: 0},
		{UpperBound: 0.1, CumulativeCount: 0},
		{UpperBound: 1e308, CumulativeCount: 0},
	}
	bucketsCurr := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 50},
		{UpperBound: 0.05, CumulativeCount: 90},
		{UpperBound: 0.1, CumulativeCount: 100},
		{UpperBound: 1e308, CumulativeCount: 100},
	}

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:            "test.com",
					DurationCount:   0,
					DurationSum:     0,
					DurationBuckets: bucketsPrev,
					StatusCodes:     map[int]float64{},
				},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:            "test.com",
					DurationCount:   100,
					DurationSum:     5.0,
					DurationBuckets: bucketsCurr,
					StatusCodes:     map[int]float64{},
				},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.Len(t, s.HostDerived, 1)
	hd := s.HostDerived[0]
	assert.True(t, hd.HasPercentiles)
	assert.True(t, hd.P50 > 0, "P50 should be computed")
	assert.True(t, hd.P90 > 0, "P90 should be computed")
	assert.True(t, hd.P95 > 0, "P95 should be computed")
	assert.True(t, hd.P99 > 0, "P99 should be computed")
	assert.True(t, hd.P90 >= hd.P50, "P90 >= P50")
	assert.True(t, hd.P95 >= hd.P90, "P95 >= P90")
	assert.True(t, hd.P99 >= hd.P95, "P99 >= P95")
}

func TestComputeHostDerived_WithTTFB(t *testing.T) {
	now := time.Now()

	ttfbPrev := []fetcher.HistogramBucket{
		{UpperBound: 0.005, CumulativeCount: 0},
		{UpperBound: 0.01, CumulativeCount: 0},
		{UpperBound: 0.05, CumulativeCount: 0},
		{UpperBound: 1e308, CumulativeCount: 0},
	}
	ttfbCurr := []fetcher.HistogramBucket{
		{UpperBound: 0.005, CumulativeCount: 40},
		{UpperBound: 0.01, CumulativeCount: 80},
		{UpperBound: 0.05, CumulativeCount: 100},
		{UpperBound: 1e308, CumulativeCount: 100},
	}

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:        "test.com",
					TTFBBuckets: ttfbPrev,
					StatusCodes: map[int]float64{},
				},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:          "test.com",
					DurationCount: 100,
					DurationSum:   5.0,
					TTFBBuckets:   ttfbCurr,
					StatusCodes:   map[int]float64{},
				},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	require.Len(t, s.HostDerived, 1)
	hd := s.HostDerived[0]
	assert.True(t, hd.HasTTFB)
	assert.True(t, hd.TTFBP50 > 0, "TTFB P50")
	assert.True(t, hd.TTFBP90 > 0, "TTFB P90")
	assert.True(t, hd.TTFBP90 >= hd.TTFBP50, "TTFB P90 >= P50")
}

func TestComputeHostDerived_NoTTFBWithoutBuckets(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {Host: "test.com", DurationCount: 10, DurationSum: 1, StatusCodes: map[int]float64{}},
			},
		},
	}
	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {Host: "test.com", DurationCount: 20, DurationSum: 2, StatusCodes: map[int]float64{}},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	require.Len(t, s.HostDerived, 1)
	assert.False(t, s.HostDerived[0].HasTTFB)
}

func TestComputeHostDerived_NoPrevious(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:        "test.com",
					InFlight:    3,
					StatusCodes: map[int]float64{},
				},
			},
		},
	}

	var s State
	s.Update(snap)

	assert.Len(t, s.HostDerived, 1)
	hd := s.HostDerived[0]
	assert.Equal(t, float64(0), hd.RPS)
	assert.Equal(t, float64(3), hd.InFlight)
	assert.False(t, hd.HasPercentiles)
}

func TestComputeHostDerived_NewHostNotInPrevious(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"old.com": {Host: "old.com", DurationCount: 100, DurationSum: 5, StatusCodes: map[int]float64{}},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"old.com": {Host: "old.com", DurationCount: 200, DurationSum: 15, StatusCodes: map[int]float64{}},
				"new.com": {Host: "new.com", DurationCount: 50, DurationSum: 2, InFlight: 1, StatusCodes: map[int]float64{}},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.Len(t, s.HostDerived, 2)

	var newHost *HostDerived
	for i := range s.HostDerived {
		if s.HostDerived[i].Host == "new.com" {
			newHost = &s.HostDerived[i]
		}
	}
	assert.NotNil(t, newHost)
	assert.Equal(t, float64(0), newHost.RPS, "new host with no previous should have 0 RPS")
	assert.Equal(t, float64(1), newHost.InFlight)
}

func TestComputeMethodRates(t *testing.T) {
	curr := map[string]float64{"GET": 100, "POST": 20, "PUT": 5}
	prev := map[string]float64{"GET": 80, "POST": 10}
	dt := 2.0

	rates := computeMethodRates(curr, prev, dt)
	assert.InDelta(t, 10, rates["GET"], 0.01)  // (100-80)/2
	assert.InDelta(t, 5, rates["POST"], 0.01)  // (20-10)/2
	assert.InDelta(t, 2.5, rates["PUT"], 0.01) // (5-0)/2
}

func TestComputeMethodRates_Empty(t *testing.T) {
	assert.Nil(t, computeMethodRates(nil, nil, 1.0))
	assert.Nil(t, computeMethodRates(map[string]float64{}, map[string]float64{}, 1.0))
	assert.Nil(t, computeMethodRates(map[string]float64{"GET": 10}, nil, 0))
}

func TestComputeMethodRates_NoDelta(t *testing.T) {
	same := map[string]float64{"GET": 50}
	assert.Nil(t, computeMethodRates(same, same, 1.0))
}

func TestComputeHostDerived_MethodRates(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:          "test.com",
					DurationCount: 100,
					DurationSum:   5.0,
					Methods:       map[string]float64{"GET": 80, "POST": 20},
					StatusCodes:   map[int]float64{},
				},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:          "test.com",
					DurationCount: 200,
					DurationSum:   15.0,
					Methods:       map[string]float64{"GET": 160, "POST": 40},
					StatusCodes:   map[int]float64{},
				},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	require.Len(t, s.HostDerived, 1)
	hd := s.HostDerived[0]
	assert.InDelta(t, 40, hd.MethodRates["GET"], 0.5)  // (160-80)/2
	assert.InDelta(t, 10, hd.MethodRates["POST"], 0.5) // (40-20)/2
}

func TestComputeHostDerived_AvgResponseSize(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:              "test.com",
					ResponseSizeSum:   500000,
					ResponseSizeCount: 100,
					StatusCodes:       map[int]float64{},
				},
			},
		},
	}

	var s State
	s.Update(snap)

	require.Len(t, s.HostDerived, 1)
	hd := s.HostDerived[0]
	assert.InDelta(t, 5000, hd.AvgResponseSize, 1) // 500000/100
}

func TestComputeHostDerived_AvgRequestSize(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:             "test.com",
					RequestSizeSum:   250000,
					RequestSizeCount: 100,
					StatusCodes:      map[int]float64{},
				},
			},
		},
	}

	var s State
	s.Update(snap)

	require.Len(t, s.HostDerived, 1)
	assert.InDelta(t, 2500, s.HostDerived[0].AvgRequestSize, 1)
}

func TestComputeHostDerived_AvgRequestSize_ZeroCount(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:        "test.com",
					StatusCodes: map[int]float64{},
				},
			},
		},
	}

	var s State
	s.Update(snap)

	require.Len(t, s.HostDerived, 1)
	assert.Equal(t, float64(0), s.HostDerived[0].AvgRequestSize)
}

func TestComputeHostDerived_TotalRequests(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:          "test.com",
					RequestsTotal: 1234,
					StatusCodes:   map[int]float64{},
				},
			},
		},
	}

	var s State
	s.Update(snap)

	require.Len(t, s.HostDerived, 1)
	assert.Equal(t, float64(1234), s.HostDerived[0].TotalRequests)
}

func TestState_Update_DerivedP90FromHistogram(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			DurationBuckets: []fetcher.HistogramBucket{
				{UpperBound: 0.01, CumulativeCount: 0},
				{UpperBound: 0.05, CumulativeCount: 0},
				{UpperBound: 0.1, CumulativeCount: 0},
				{UpperBound: 1e308, CumulativeCount: 0},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			DurationBuckets: []fetcher.HistogramBucket{
				{UpperBound: 0.01, CumulativeCount: 50},
				{UpperBound: 0.05, CumulativeCount: 90},
				{UpperBound: 0.1, CumulativeCount: 100},
				{UpperBound: 1e308, CumulativeCount: 100},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.True(t, s.Derived.HasPercentiles)
	assert.True(t, s.Derived.P50 > 0, "P50")
	assert.True(t, s.Derived.P90 > 0, "P90")
	assert.True(t, s.Derived.P95 > 0, "P95")
	assert.True(t, s.Derived.P99 > 0, "P99")
	assert.True(t, s.Derived.P90 >= s.Derived.P50, "P90 >= P50")
	assert.True(t, s.Derived.P95 >= s.Derived.P90, "P95 >= P90")
}

func TestState_Update_ErrorRate(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestErrorsTotal:   10,
			HTTPRequestDurationCount: 100,
			HTTPRequestDurationSum:   5.0,
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestErrorsTotal:   20,
			HTTPRequestDurationCount: 200,
			HTTPRequestDurationSum:   10.0,
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.InDelta(t, 5, s.Derived.ErrorRate, 0.5)
}

func TestState_Update_ErrorRate_NoPrevious(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers:                map[string]*fetcher.WorkerMetrics{},
			HTTPRequestErrorsTotal: 10,
		},
	}

	var s State
	s.Update(snap)

	assert.Equal(t, float64(0), s.Derived.ErrorRate)
}

func TestState_Update_ErrorRate_NoDelta(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestErrorsTotal:   10,
			HTTPRequestDurationCount: 100,
			HTTPRequestDurationSum:   5.0,
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Threads:   dummyThreads,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestErrorsTotal:   10,
			HTTPRequestDurationCount: 200,
			HTTPRequestDurationSum:   10.0,
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.Equal(t, float64(0), s.Derived.ErrorRate)
}

func TestComputeHostDerived_ErrorRate(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:          "test.com",
					DurationCount: 100,
					DurationSum:   5.0,
					ErrorsTotal:   5,
					StatusCodes:   map[int]float64{},
				},
			},
		},
	}

	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"test.com": {
					Host:          "test.com",
					DurationCount: 200,
					DurationSum:   10.0,
					ErrorsTotal:   15,
					StatusCodes:   map[int]float64{},
				},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	require.Len(t, s.HostDerived, 1)
	assert.InDelta(t, 5, s.HostDerived[0].ErrorRate, 0.5)
}

func TestState_CopyForExport_NilsPercentiles(t *testing.T) {
	snap := &fetcher.Snapshot{
		Threads: dummyThreads,
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	var s State
	s.Update(snap)
	require.NotNil(t, s.percentiles)

	cp := s.CopyForExport()
	assert.Nil(t, cp.percentiles, "CopyForExport should nil out percentiles")
	assert.NotNil(t, s.percentiles, "original should keep its percentiles")
}

func TestState_CopyForExport_DeepCopiesCurrent(t *testing.T) {
	snap := &fetcher.Snapshot{
		Threads:   dummyThreads,
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		FetchedAt: time.Now(),
	}

	var s State
	s.Update(snap)

	cp := s.CopyForExport()
	require.NotNil(t, cp.Current)
	assert.NotSame(t, s.Current, cp.Current, "Current should be a different pointer")
	assert.Equal(t, s.Current.FetchedAt, cp.Current.FetchedAt)
}

func TestState_CopyForExport_NilsPrevious(t *testing.T) {
	snap1 := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}
	snap2 := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	var s State
	s.Update(snap1)
	s.Update(snap2)
	require.NotNil(t, s.Previous)

	cp := s.CopyForExport()
	assert.Nil(t, cp.Previous, "CopyForExport should nil out Previous")
}

func TestState_CopyForExport_CopiesHostDerived(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-2 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"a.com": {Host: "a.com", DurationCount: 10, DurationSum: 1, StatusCodes: map[int]float64{}},
			},
		},
	}
	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"a.com": {Host: "a.com", DurationCount: 20, DurationSum: 2, StatusCodes: map[int]float64{}},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	cp := s.CopyForExport()
	require.Len(t, cp.HostDerived, 1)
	assert.Equal(t, "a.com", cp.HostDerived[0].Host)

	// Mutating the copy should not affect the original
	cp.HostDerived[0].Host = "mutated.com"
	assert.Equal(t, "a.com", s.HostDerived[0].Host)
}

func TestState_CopyForExport_DeepCopiesHostDerivedMaps(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	var s State
	s.Update(snap)
	s.HostDerived = []HostDerived{
		{
			Host:        "a.com",
			StatusCodes: map[int]float64{200: 10, 404: 2},
			MethodRates: map[string]float64{"GET": 8, "POST": 4},
		},
	}

	cp := s.CopyForExport()
	require.Len(t, cp.HostDerived, 1)

	// Mutating copied maps must not affect originals
	cp.HostDerived[0].StatusCodes[500] = 99
	cp.HostDerived[0].MethodRates["DELETE"] = 77

	assert.Equal(t, map[int]float64{200: 10, 404: 2}, s.HostDerived[0].StatusCodes)
	assert.Equal(t, map[string]float64{"GET": 8, "POST": 4}, s.HostDerived[0].MethodRates)
}

func TestState_CopyForExport_NilMapsStayNil(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	var s State
	s.Update(snap)
	s.HostDerived = []HostDerived{
		{Host: "b.com", StatusCodes: nil, MethodRates: nil},
	}

	cp := s.CopyForExport()
	require.Len(t, cp.HostDerived, 1)
	assert.Nil(t, cp.HostDerived[0].StatusCodes)
	assert.Nil(t, cp.HostDerived[0].MethodRates)
}

func TestState_CopyForExport_NilHostDerived(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
	}

	var s State
	s.Update(snap)
	assert.Nil(t, s.HostDerived)

	cp := s.CopyForExport()
	assert.Nil(t, cp.HostDerived)
}

func TestState_CopyForExport_DeepCopiesSnapshotWorkers(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{
				"/app/worker.php": {Worker: "/app/worker.php", Crashes: 3},
			},
		},
	}

	var s State
	s.Update(snap)
	cp := s.CopyForExport()

	cp.Current.Metrics.Workers["/app/worker.php"].Crashes = 999
	assert.Equal(t, 3.0, s.Current.Metrics.Workers["/app/worker.php"].Crashes,
		"mutating copy should not affect original")
}

func TestState_CopyForExport_DeepCopiesSnapshotHosts(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Hosts: map[string]*fetcher.HostMetrics{
				"example.com": {
					Host:        "example.com",
					StatusCodes: map[int]float64{200: 10},
					Methods:     map[string]float64{"GET": 5},
					DurationBuckets: []fetcher.HistogramBucket{
						{UpperBound: 0.01, CumulativeCount: 50},
					},
				},
			},
		},
	}

	var s State
	s.Update(snap)
	cp := s.CopyForExport()

	cp.Current.Metrics.Hosts["example.com"].StatusCodes[200] = 999
	cp.Current.Metrics.Hosts["example.com"].Methods["GET"] = 999
	cp.Current.Metrics.Hosts["example.com"].DurationBuckets[0].CumulativeCount = 999
	cp.Current.Metrics.Hosts["new.com"] = &fetcher.HostMetrics{Host: "new.com"}

	assert.Equal(t, 10.0, s.Current.Metrics.Hosts["example.com"].StatusCodes[200],
		"mutating copy StatusCodes should not affect original")
	assert.Equal(t, 5.0, s.Current.Metrics.Hosts["example.com"].Methods["GET"],
		"mutating copy Methods should not affect original")
	assert.Equal(t, 50.0, s.Current.Metrics.Hosts["example.com"].DurationBuckets[0].CumulativeCount,
		"mutating copy DurationBuckets should not affect original")
	assert.NotContains(t, s.Current.Metrics.Hosts, "new.com",
		"adding to copy Hosts map should not affect original")
}

func TestState_CopyForExport_DeepCopiesSnapshotBuckets(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			DurationBuckets: []fetcher.HistogramBucket{
				{UpperBound: 0.01, CumulativeCount: 100},
				{UpperBound: 0.05, CumulativeCount: 200},
			},
		},
	}

	var s State
	s.Update(snap)
	cp := s.CopyForExport()

	cp.Current.Metrics.DurationBuckets[0].CumulativeCount = 999
	assert.Equal(t, 100.0, s.Current.Metrics.DurationBuckets[0].CumulativeCount,
		"mutating copy DurationBuckets should not affect original")
}

func TestState_Update_CounterReset_DiscardsPrevious(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestDurationCount: 1000,
			HTTPRequestDurationSum:   50.0,
			HTTPRequestsTotal:        1000,
		},
	}
	// caddy restarted: counters reset to zero and start climbing again
	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestDurationCount: 5,
			HTTPRequestDurationSum:   0.25,
			HTTPRequestsTotal:        5,
		},
	}

	var s State
	s.Update(prev)
	require.NotNil(t, s.Current)

	s.Update(curr)

	assert.Nil(t, s.Previous, "Previous should be discarded on counter reset")
	assert.Equal(t, float64(0), s.Derived.RPS, "RPS should be zero after reset (no previous)")
}

func TestState_Update_CounterReset_WorkerMetrics(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{"w": {RequestCount: 500}},
			HTTPRequestDurationCount: 500,
		},
	}
	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{"w": {RequestCount: 2}},
			HTTPRequestDurationCount: 2,
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.Nil(t, s.Previous)
	assert.Equal(t, float64(0), s.Derived.RPS)
}

func TestState_Update_NoFalseReset(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestDurationCount: 100,
			HTTPRequestsTotal:        100,
		},
	}
	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestDurationCount: 200,
			HTTPRequestsTotal:        200,
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.NotNil(t, s.Previous, "Normal increment should not trigger reset")
	assert.True(t, s.Derived.RPS > 0, "RPS should be computed normally")
}

func TestState_Update_CounterReset_HostMetrics(t *testing.T) {
	now := time.Now()

	prev := &fetcher.Snapshot{
		FetchedAt: now.Add(-1 * time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestDurationCount: 1000,
			HTTPRequestsTotal:        1000,
			Hosts: map[string]*fetcher.HostMetrics{
				"api": {Host: "api", DurationCount: 500, DurationSum: 25.0, StatusCodes: map[int]float64{200: 500}, Methods: map[string]float64{"GET": 500}},
			},
		},
	}
	curr := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers:                  map[string]*fetcher.WorkerMetrics{},
			HTTPRequestDurationCount: 3,
			HTTPRequestsTotal:        3,
			Hosts: map[string]*fetcher.HostMetrics{
				"api": {Host: "api", DurationCount: 3, DurationSum: 0.15, StatusCodes: map[int]float64{200: 3}, Methods: map[string]float64{"GET": 3}},
			},
		},
	}

	var s State
	s.Update(prev)
	s.Update(curr)

	assert.Nil(t, s.Previous, "Counter reset should be detected")
	require.Len(t, s.HostDerived, 1)
	assert.Equal(t, float64(0), s.HostDerived[0].RPS, "Per-host RPS should be zero after reset")
}

func TestDetectCounterReset_NoPrevious(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{HTTPRequestDurationCount: 100},
	}
	var s State
	assert.False(t, s.detectCounterReset(snap))
}

func TestDetectCounterReset_HTTPRequestsTotal(t *testing.T) {
	var s State
	s.Current = &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{HTTPRequestsTotal: 1000},
	}
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{HTTPRequestsTotal: 5},
	}
	assert.True(t, s.detectCounterReset(snap))
}

func TestDetectCounterReset_ZeroPreviousIgnored(t *testing.T) {
	var s State
	s.Current = &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{HTTPRequestDurationCount: 0},
	}
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{HTTPRequestDurationCount: 0},
	}
	assert.False(t, s.detectCounterReset(snap), "zero-to-zero should not be a reset")
}

func TestSortField_String(t *testing.T) {
	tests := []struct {
		field SortField
		want  string
	}{
		{SortByIndex, "index"},
		{SortByState, "state"},
		{SortByMethod, "method"},
		{SortByURI, "uri"},
		{SortByTime, "time"},
		{SortByMemory, "memory"},
		{SortByRequests, "requests"},
		{SortField(99), "index"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.field.String())
	}
}

func TestResetPercentiles(t *testing.T) {
	snap1 := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestCount: 10},
			},
		},
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		FetchedAt: time.Now().Add(-2 * time.Second),
	}
	snap2 := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsWaiting: true, RequestCount: 11},
			},
		},
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		FetchedAt: time.Now(),
	}

	var s State
	s.Update(snap1)
	s.Update(snap2)

	s.ResetPercentiles()

	// after reset, the next update should have no percentile data
	snap3 := &fetcher.Snapshot{
		Threads: fetcher.ThreadsResponse{
			ThreadDebugStates: []fetcher.ThreadDebugState{
				{Index: 0, IsBusy: true, RequestCount: 12},
			},
		},
		Metrics:   fetcher.MetricsSnapshot{Workers: map[string]*fetcher.WorkerMetrics{}},
		FetchedAt: time.Now().Add(1 * time.Second),
	}
	s.Update(snap3)
	assert.False(t, s.Derived.HasPercentiles, "percentiles should not be available right after reset")
}

func TestResetPercentiles_NilSafe(t *testing.T) {
	var s State
	assert.NotPanics(t, func() {
		s.ResetPercentiles()
	}, "ResetPercentiles should not panic on uninitialized state")
}

func TestState_UpstreamDerived(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Upstreams: map[string]*fetcher.UpstreamMetrics{
				"10.0.0.1:8080/rp": {Address: "10.0.0.1:8080", Handler: "rp", Healthy: 1},
				"10.0.0.2:8080/rp": {Address: "10.0.0.2:8080", Handler: "rp", Healthy: 0},
			},
		},
	}

	var s State
	s.Update(snap)

	require.Len(t, s.UpstreamDerived, 2)

	byAddr := make(map[string]UpstreamDerived)
	for _, u := range s.UpstreamDerived {
		byAddr[u.Address] = u
	}

	assert.True(t, byAddr["10.0.0.1:8080"].Healthy)
	assert.False(t, byAddr["10.0.0.2:8080"].Healthy)
	assert.False(t, byAddr["10.0.0.1:8080"].HealthChanged)
	assert.False(t, byAddr["10.0.0.2:8080"].HealthChanged)
}

func TestState_UpstreamDerived_HealthChanged(t *testing.T) {
	now := time.Now()

	snap1 := &fetcher.Snapshot{
		FetchedAt: now.Add(-time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Upstreams: map[string]*fetcher.UpstreamMetrics{
				"10.0.0.1:8080/rp": {Address: "10.0.0.1:8080", Handler: "rp", Healthy: 1},
				"10.0.0.2:8080/rp": {Address: "10.0.0.2:8080", Handler: "rp", Healthy: 1},
			},
		},
	}

	snap2 := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Upstreams: map[string]*fetcher.UpstreamMetrics{
				"10.0.0.1:8080/rp": {Address: "10.0.0.1:8080", Handler: "rp", Healthy: 1},
				"10.0.0.2:8080/rp": {Address: "10.0.0.2:8080", Handler: "rp", Healthy: 0},
			},
		},
	}

	var s State
	s.Update(snap1)
	s.Update(snap2)

	byAddr := make(map[string]UpstreamDerived)
	for _, u := range s.UpstreamDerived {
		byAddr[u.Address] = u
	}

	assert.False(t, byAddr["10.0.0.1:8080"].HealthChanged, "should not have changed")
	assert.True(t, byAddr["10.0.0.2:8080"].HealthChanged, "should have changed from healthy to down")
}

func TestState_UpstreamDerived_MultiHandlerSameAddressTrackedIndependently(t *testing.T) {
	now := time.Now()

	snap1 := &fetcher.Snapshot{
		FetchedAt: now.Add(-time.Second),
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Upstreams: map[string]*fetcher.UpstreamMetrics{
				"10.0.0.1:8080/rp_0": {Address: "10.0.0.1:8080", Handler: "rp_0", Healthy: 1},
				"10.0.0.1:8080/rp_1": {Address: "10.0.0.1:8080", Handler: "rp_1", Healthy: 1},
			},
		},
	}

	snap2 := &fetcher.Snapshot{
		FetchedAt: now,
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Upstreams: map[string]*fetcher.UpstreamMetrics{
				"10.0.0.1:8080/rp_0": {Address: "10.0.0.1:8080", Handler: "rp_0", Healthy: 1},
				"10.0.0.1:8080/rp_1": {Address: "10.0.0.1:8080", Handler: "rp_1", Healthy: 0},
			},
		},
	}

	var s State
	s.Update(snap1)
	s.Update(snap2)

	require.Len(t, s.UpstreamDerived, 2)

	byHandler := make(map[string]UpstreamDerived)
	for _, ud := range s.UpstreamDerived {
		byHandler[ud.Handler] = ud
	}

	assert.False(t, byHandler["rp_0"].HealthChanged, "rp_0 stayed healthy")
	assert.True(t, byHandler["rp_1"].HealthChanged, "rp_1 flipped to down and must be flagged")
}

func TestState_UpstreamDerived_NoUpstreams(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
		},
	}

	var s State
	s.Update(snap)
	assert.Nil(t, s.UpstreamDerived)
}

func TestState_CopyForExport_IncludesUpstreams(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers: map[string]*fetcher.WorkerMetrics{},
			Upstreams: map[string]*fetcher.UpstreamMetrics{
				"10.0.0.1:8080/rp": {Address: "10.0.0.1:8080", Handler: "rp", Healthy: 1},
			},
		},
	}

	var s State
	s.Update(snap)

	cp := s.CopyForExport()
	require.Len(t, cp.UpstreamDerived, 1)
	assert.Equal(t, "10.0.0.1:8080", cp.UpstreamDerived[0].Address)

	cp.UpstreamDerived[0] = UpstreamDerived{Address: "modified"}
	assert.NotEqual(t, "modified", s.UpstreamDerived[0].Address, "copy should be independent")
}

func TestUpstreamSortField_Cycle(t *testing.T) {
	f := SortByUpstreamAddress
	f = f.Next()
	assert.Equal(t, SortByUpstreamHealth, f)
	f = f.Next()
	assert.Equal(t, SortByUpstreamAddress, f)

	f = f.Prev()
	assert.Equal(t, SortByUpstreamHealth, f)
}

func TestUpstreamSortField_String(t *testing.T) {
	assert.Equal(t, "address", SortByUpstreamAddress.String())
	assert.Equal(t, "health", SortByUpstreamHealth.String())
}
