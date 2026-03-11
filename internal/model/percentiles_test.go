package model

import (
	"math"
	"testing"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
)

func TestPercentileTracker_NoSamples(t *testing.T) {
	pt := NewPercentileTracker(5 * time.Minute)
	_, _, _, ok := pt.Percentiles(time.Now())
	assert.False(t, ok)
	assert.Equal(t, 0, pt.Count(time.Now()))
}

func TestPercentileTracker_SingleSample(t *testing.T) {
	now := time.Now()
	pt := NewPercentileTracker(5 * time.Minute)
	pt.Record(42.0, now)

	p50, p95, p99, ok := pt.Percentiles(now)
	assert.True(t, ok)
	assert.Equal(t, 42.0, p50)
	assert.Equal(t, 42.0, p95)
	assert.Equal(t, 42.0, p99)
}

func TestPercentileTracker_KnownDistribution(t *testing.T) {
	now := time.Now()
	pt := NewPercentileTracker(5 * time.Minute)
	for i := 1; i <= 100; i++ {
		pt.Record(float64(i), now)
	}

	p50, p95, p99, ok := pt.Percentiles(now)
	assert.True(t, ok)
	assert.InDelta(t, 50.5, p50, 0.5)
	assert.InDelta(t, 95.05, p95, 0.5)
	assert.InDelta(t, 99.01, p99, 0.5)
}

func TestPercentileTracker_ExpiresOldSamples(t *testing.T) {
	expiry := 5 * time.Minute
	pt := NewPercentileTracker(expiry)

	old := time.Now().Add(-10 * time.Minute)
	for i := 0; i < 10; i++ {
		pt.Record(1000.0, old.Add(time.Duration(i)*time.Second))
	}

	recent := time.Now()
	for i := 1; i <= 10; i++ {
		pt.Record(float64(i), recent.Add(time.Duration(i)*time.Second))
	}

	now := recent.Add(11 * time.Second)
	assert.Equal(t, 10, pt.Count(now), "old samples should be expired")

	p50, _, _, ok := pt.Percentiles(now)
	assert.True(t, ok)
	assert.InDelta(t, 5.5, p50, 0.5, "should only reflect recent samples")
}

func TestPercentileTracker_AllExpired(t *testing.T) {
	pt := NewPercentileTracker(1 * time.Minute)

	old := time.Now().Add(-5 * time.Minute)
	pt.Record(100.0, old)
	pt.Record(200.0, old)

	_, _, _, ok := pt.Percentiles(time.Now())
	assert.False(t, ok, "all samples expired")
}

func TestPercentileTracker_IgnoresNegativeAndZero(t *testing.T) {
	now := time.Now()
	pt := NewPercentileTracker(5 * time.Minute)
	pt.Record(0, now)
	pt.Record(-5.0, now)
	pt.Record(-100.0, now)

	assert.Equal(t, 0, pt.Count(now))
	_, _, _, ok := pt.Percentiles(now)
	assert.False(t, ok)
}

func TestPercentileTracker_Reset(t *testing.T) {
	now := time.Now()
	pt := NewPercentileTracker(5 * time.Minute)
	pt.Record(10.0, now)
	pt.Record(20.0, now)
	assert.Equal(t, 2, pt.Count(now))

	pt.Reset()
	assert.Equal(t, 0, pt.Count(now))
	_, _, _, ok := pt.Percentiles(now)
	assert.False(t, ok)
}

func TestPercentileTracker_DefaultExpiry(t *testing.T) {
	pt := NewPercentileTracker(0)
	assert.Equal(t, DefaultPercentileExpiry, pt.expiry)
}

// --- Histogram-based percentile tests ---

func TestHistogramPercentiles_Basic(t *testing.T) {
	// Simulate: 50 reqs < 5ms, 50 reqs < 10ms, 60 reqs < +Inf (total 160)
	prev := []fetcher.HistogramBucket{
		{UpperBound: 0.005, CumulativeCount: 0},
		{UpperBound: 0.01, CumulativeCount: 0},
		{UpperBound: math.Inf(1), CumulativeCount: 0},
	}
	curr := []fetcher.HistogramBucket{
		{UpperBound: 0.005, CumulativeCount: 50},
		{UpperBound: 0.01, CumulativeCount: 100},
		{UpperBound: math.Inf(1), CumulativeCount: 160},
	}

	p50, p95, p99, ok := HistogramPercentiles(prev, curr)
	assert.True(t, ok)
	// P50 = 50th percentile of 160 reqs → rank 80, falls in [0.005, 0.01] bucket
	assert.InDelta(t, 8.0, p50, 1.0, "P50 should be ~8ms")
	// P95 = rank 152, falls in [0.01, +Inf] → clamped at 0.01 (last finite bound)
	assert.True(t, p95 >= 10, "P95 should be >= 10ms")
	assert.True(t, p99 >= 10, "P99 should be >= 10ms")
}

func TestHistogramPercentiles_NoPrev(t *testing.T) {
	curr := []fetcher.HistogramBucket{
		{UpperBound: 0.005, CumulativeCount: 50},
		{UpperBound: 0.01, CumulativeCount: 100},
		{UpperBound: math.Inf(1), CumulativeCount: 100},
	}

	p50, _, _, ok := HistogramPercentiles(nil, curr)
	assert.True(t, ok)
	assert.True(t, p50 > 0)
}

func TestHistogramPercentiles_EmptyCurr(t *testing.T) {
	_, _, _, ok := HistogramPercentiles(nil, nil)
	assert.False(t, ok)
}

func TestHistogramPercentiles_ZeroDelta(t *testing.T) {
	buckets := []fetcher.HistogramBucket{
		{UpperBound: 0.005, CumulativeCount: 50},
		{UpperBound: 0.01, CumulativeCount: 100},
		{UpperBound: math.Inf(1), CumulativeCount: 100},
	}

	// Same prev and curr → zero delta → no data
	_, _, _, ok := HistogramPercentiles(buckets, buckets)
	assert.False(t, ok)
}

func TestHistogramPercentiles_UniformDistribution(t *testing.T) {
	prev := []fetcher.HistogramBucket{
		{UpperBound: 0.1, CumulativeCount: 0},
		{UpperBound: 0.5, CumulativeCount: 0},
		{UpperBound: 1.0, CumulativeCount: 0},
		{UpperBound: math.Inf(1), CumulativeCount: 0},
	}
	// 100 reqs in [0, 0.1], 100 in [0.1, 0.5], 100 in [0.5, 1.0], 0 in [1.0, +Inf]
	curr := []fetcher.HistogramBucket{
		{UpperBound: 0.1, CumulativeCount: 100},
		{UpperBound: 0.5, CumulativeCount: 200},
		{UpperBound: 1.0, CumulativeCount: 300},
		{UpperBound: math.Inf(1), CumulativeCount: 300},
	}

	p50, p95, _, ok := HistogramPercentiles(prev, curr)
	assert.True(t, ok)
	// P50 = rank 150, falls in [0.1, 0.5] bucket: 0.1 + (0.5-0.1)*(150-100)/100 = 0.3 → 300ms
	assert.InDelta(t, 300, p50, 10)
	// P95 = rank 285, falls in [0.5, 1.0] bucket: 0.5 + (1.0-0.5)*(285-200)/100 = 0.925 → 925ms
	assert.InDelta(t, 925, p95, 10)
}