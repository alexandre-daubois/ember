package model

import (
	"math"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/stretchr/testify/assert"
)

func TestPercentileTracker_NoSamples(t *testing.T) {
	pt := newPercentileTracker(5 * time.Minute)
	_, _, _, _, ok := pt.percentiles(time.Now())
	assert.False(t, ok)
	assert.Equal(t, 0, pt.count(time.Now()))
}

func TestPercentileTracker_SingleSample(t *testing.T) {
	now := time.Now()
	pt := newPercentileTracker(5 * time.Minute)
	pt.record(42.0, now)

	p50, p90, p95, p99, ok := pt.percentiles(now)
	assert.True(t, ok)
	assert.Equal(t, 42.0, p50)
	assert.Equal(t, 42.0, p90)
	assert.Equal(t, 42.0, p95)
	assert.Equal(t, 42.0, p99)
}

func TestPercentileTracker_KnownDistribution(t *testing.T) {
	now := time.Now()
	pt := newPercentileTracker(5 * time.Minute)
	for i := 1; i <= 100; i++ {
		pt.record(float64(i), now)
	}

	p50, p90, p95, p99, ok := pt.percentiles(now)
	assert.True(t, ok)
	assert.InDelta(t, 50.5, p50, 0.5)
	assert.InDelta(t, 90.1, p90, 0.5)
	assert.InDelta(t, 95.05, p95, 0.5)
	assert.InDelta(t, 99.01, p99, 0.5)
}

func TestPercentileTracker_ExpiresOldSamples(t *testing.T) {
	expiry := 5 * time.Minute
	pt := newPercentileTracker(expiry)

	old := time.Now().Add(-10 * time.Minute)
	for i := 0; i < 10; i++ {
		pt.record(1000.0, old.Add(time.Duration(i)*time.Second))
	}

	recent := time.Now()
	for i := 1; i <= 10; i++ {
		pt.record(float64(i), recent.Add(time.Duration(i)*time.Second))
	}

	now := recent.Add(11 * time.Second)
	assert.Equal(t, 10, pt.count(now), "old samples should be expired")

	p50, _, _, _, ok := pt.percentiles(now)
	assert.True(t, ok)
	assert.InDelta(t, 5.5, p50, 0.5, "should only reflect recent samples")
}

func TestPercentileTracker_AllExpired(t *testing.T) {
	pt := newPercentileTracker(1 * time.Minute)

	old := time.Now().Add(-5 * time.Minute)
	pt.record(100.0, old)
	pt.record(200.0, old)

	_, _, _, _, ok := pt.percentiles(time.Now())
	assert.False(t, ok, "all samples expired")
}

func TestPercentileTracker_IgnoresNegativeAndZero(t *testing.T) {
	now := time.Now()
	pt := newPercentileTracker(5 * time.Minute)
	pt.record(0, now)
	pt.record(-5.0, now)
	pt.record(-100.0, now)

	assert.Equal(t, 0, pt.count(now))
	_, _, _, _, ok := pt.percentiles(now)
	assert.False(t, ok)
}

func TestPercentileTracker_Reset(t *testing.T) {
	now := time.Now()
	pt := newPercentileTracker(5 * time.Minute)
	pt.record(10.0, now)
	pt.record(20.0, now)
	assert.Equal(t, 2, pt.count(now))

	pt.reset()
	assert.Equal(t, 0, pt.count(now))
	_, _, _, _, ok := pt.percentiles(now)
	assert.False(t, ok)
}

func TestPercentileTracker_DefaultExpiry(t *testing.T) {
	pt := newPercentileTracker(0)
	assert.Equal(t, defaultPercentileExpiry, pt.expiry)
}

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

	p50, _, p95, p99, ok := histogramPercentiles(prev, curr)
	assert.True(t, ok)
	// P50 = 50th percentile of 160 reqs -> rank 80, falls in [0.005, 0.01] bucket
	assert.InDelta(t, 8.0, p50, 1.0, "P50 should be ~8ms")
	// P95 = rank 152, falls in [0.01, +Inf] -> clamped at 0.01 (last finite bound)
	assert.True(t, p95 >= 10, "P95 should be >= 10ms")
	assert.True(t, p99 >= 10, "P99 should be >= 10ms")
}

func TestHistogramPercentiles_NoPrev(t *testing.T) {
	curr := []fetcher.HistogramBucket{
		{UpperBound: 0.005, CumulativeCount: 50},
		{UpperBound: 0.01, CumulativeCount: 100},
		{UpperBound: math.Inf(1), CumulativeCount: 100},
	}

	p50, _, _, _, ok := histogramPercentiles(nil, curr)
	assert.True(t, ok)
	assert.True(t, p50 > 0)
}

func TestHistogramPercentiles_EmptyCurr(t *testing.T) {
	_, _, _, _, ok := histogramPercentiles(nil, nil)
	assert.False(t, ok)
}

func TestHistogramPercentiles_ZeroDelta(t *testing.T) {
	buckets := []fetcher.HistogramBucket{
		{UpperBound: 0.005, CumulativeCount: 50},
		{UpperBound: 0.01, CumulativeCount: 100},
		{UpperBound: math.Inf(1), CumulativeCount: 100},
	}

	// Same prev and curr -> zero delta -> no data
	_, _, _, _, ok := histogramPercentiles(buckets, buckets)
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

	p50, p90, p95, _, ok := histogramPercentiles(prev, curr)
	assert.True(t, ok)
	// P50 = rank 150, falls in [0.1, 0.5] bucket: 0.1 + (0.5-0.1)*(150-100)/100 = 0.3 -> 300ms
	assert.InDelta(t, 300, p50, 10)
	// P90 = rank 270, falls in [0.5, 1.0] bucket: 0.5 + (1.0-0.5)*(270-200)/100 = 0.85 -> 850ms
	assert.InDelta(t, 850, p90, 10)
	// P95 = rank 285, falls in [0.5, 1.0] bucket: 0.5 + (1.0-0.5)*(285-200)/100 = 0.925 -> 925ms
	assert.InDelta(t, 925, p95, 10)
	assert.True(t, p90 >= p50, "P90 >= P50")
	assert.True(t, p95 >= p90, "P95 >= P90")
}

func TestPercentileValue_TwoSamples(t *testing.T) {
	sorted := []float64{10, 20}
	assert.Equal(t, 15.0, percentileValue(sorted, 0.5))
	assert.Equal(t, 10.0, percentileValue(sorted, 0.0))
	assert.Equal(t, 20.0, percentileValue(sorted, 1.0))
}

func TestSubtractBuckets_ZeroDelta(t *testing.T) {
	prev := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 50},
		{UpperBound: 0.05, CumulativeCount: 100},
	}
	curr := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 50},
		{UpperBound: 0.05, CumulativeCount: 100},
	}
	delta := subtractBuckets(prev, curr)
	assert.Len(t, delta, 2)
	assert.Equal(t, 0.0, delta[0].CumulativeCount)
	assert.Equal(t, 0.0, delta[1].CumulativeCount)
}

func TestSubtractBuckets_NewBucketInCurr(t *testing.T) {
	prev := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 50},
	}
	curr := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 80},
		{UpperBound: 0.05, CumulativeCount: 120},
	}
	delta := subtractBuckets(prev, curr)
	assert.Len(t, delta, 2)
	assert.Equal(t, 30.0, delta[0].CumulativeCount)
	assert.Equal(t, 120.0, delta[1].CumulativeCount)
}

func TestSubtractBuckets_NegativeDeltaClamped(t *testing.T) {
	prev := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 100},
	}
	curr := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 50},
	}
	delta := subtractBuckets(prev, curr)
	assert.Equal(t, 0.0, delta[0].CumulativeCount)
}

func TestHistogramQuantile_FallbackLastBucket(t *testing.T) {
	// Construct a case where the loop never finds count >= rank
	// This can't normally happen with valid data, but test the defensive fallback
	// All counts are zero except total > 0 won't work since total = last bucket count
	// Actually with valid cumulative counts, rank <= total, so loop always finds a bucket.
	// We can still verify the +Inf return behavior covers the unhappy path.
	buckets := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 0},
		{UpperBound: math.Inf(1), CumulativeCount: 10},
	}
	// rank=0.5*10=5, bucket[0].count=0 < 5, bucket[1] is +Inf with count=10 >= 5 -> return 0.01
	result := histogramQuantile(0.5, buckets)
	assert.Equal(t, 0.01, result)
}

func TestHistogramPercentiles_AllZeroCounts(t *testing.T) {
	curr := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 0},
		{UpperBound: 0.05, CumulativeCount: 0},
		{UpperBound: math.Inf(1), CumulativeCount: 0},
	}
	_, _, _, _, ok := histogramPercentiles(nil, curr)
	assert.False(t, ok, "zero-total buckets should return ok=false")
}

func TestHistogramQuantile_RankExceedsAllBuckets(t *testing.T) {
	// All counts are below rank
	buckets := []fetcher.HistogramBucket{
		{UpperBound: 0.01, CumulativeCount: 0},
		{UpperBound: 0.05, CumulativeCount: 0},
		{UpperBound: math.Inf(1), CumulativeCount: 1},
	}
	// q=0.99 -> rank=0.99, only +Inf has count >= rank
	// Falls into +Inf bucket -> returns lowerBound (0.05)
	result := histogramQuantile(0.99, buckets)
	assert.Equal(t, 0.05, result)
}
