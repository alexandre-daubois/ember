package model

import (
	"math"
	"slices"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
)

const DefaultPercentileExpiry = 5 * time.Minute

type timedSample struct {
	at    time.Time
	value float64
}

type PercentileTracker struct {
	samples []timedSample
	expiry  time.Duration
}

func NewPercentileTracker(expiry time.Duration) *PercentileTracker {
	if expiry <= 0 {
		expiry = DefaultPercentileExpiry
	}
	return &PercentileTracker{expiry: expiry}
}

func (pt *PercentileTracker) Record(durationMs float64, at time.Time) {
	if durationMs <= 0 {
		return
	}
	pt.trim(at)
	pt.samples = append(pt.samples, timedSample{at: at, value: durationMs})
}

func (pt *PercentileTracker) Percentiles(now time.Time) (p50, p95, p99 float64, ok bool) {
	pt.trim(now)
	n := len(pt.samples)
	if n == 0 {
		return 0, 0, 0, false
	}

	sorted := make([]float64, n)
	for i, s := range pt.samples {
		sorted[i] = s.value
	}
	slices.Sort(sorted)

	p50 = percentileValue(sorted, 0.50)
	p95 = percentileValue(sorted, 0.95)
	p99 = percentileValue(sorted, 0.99)
	return p50, p95, p99, true
}

func (pt *PercentileTracker) Count(now time.Time) int {
	pt.trim(now)
	return len(pt.samples)
}

func (pt *PercentileTracker) Reset() {
	pt.samples = pt.samples[:0]
}

func (pt *PercentileTracker) trim(now time.Time) {
	cutoff := now.Add(-pt.expiry)
	i := 0
	for i < len(pt.samples) && pt.samples[i].at.Before(cutoff) {
		i++
	}
	if i > 0 {
		pt.samples = pt.samples[i:]
	}
}

func percentileValue(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	idx := p * float64(n-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= n {
		return sorted[n-1]
	}
	frac := idx - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}

// HistogramPercentiles computes P50/P95/P99 from the delta of two Prometheus
// histogram bucket snapshots using linear interpolation within buckets
// (same algorithm as Prometheus histogram_quantile).
// Returns values in milliseconds and ok=true if computable.
func HistogramPercentiles(prev, curr []fetcher.HistogramBucket) (p50, p95, p99 float64, ok bool) {
	delta := subtractBuckets(prev, curr)
	if len(delta) == 0 {
		return 0, 0, 0, false
	}

	p50 = histogramQuantile(0.50, delta)
	p95 = histogramQuantile(0.95, delta)
	p99 = histogramQuantile(0.99, delta)

	if math.IsNaN(p50) || math.IsInf(p50, 0) {
		return 0, 0, 0, false
	}

	// Prometheus buckets are in seconds → convert to milliseconds
	return p50 * 1000, p95 * 1000, p99 * 1000, true
}

// subtractBuckets computes the delta (curr - prev) for matching buckets.
func subtractBuckets(prev, curr []fetcher.HistogramBucket) []fetcher.HistogramBucket {
	if len(curr) == 0 {
		return nil
	}
	if len(prev) == 0 {
		return curr
	}

	prevMap := make(map[float64]float64, len(prev))
	for _, b := range prev {
		prevMap[b.UpperBound] = b.CumulativeCount
	}

	delta := make([]fetcher.HistogramBucket, 0, len(curr))
	for _, b := range curr {
		d := b.CumulativeCount - prevMap[b.UpperBound]
		if d < 0 {
			d = 0
		}
		delta = append(delta, fetcher.HistogramBucket{
			UpperBound:      b.UpperBound,
			CumulativeCount: d,
		})
	}
	return delta
}

// histogramQuantile implements Prometheus's histogram_quantile function.
// Buckets must be sorted by UpperBound with cumulative counts.
func histogramQuantile(q float64, buckets []fetcher.HistogramBucket) float64 {
	if len(buckets) == 0 {
		return math.NaN()
	}

	total := buckets[len(buckets)-1].CumulativeCount
	if total == 0 {
		return math.NaN()
	}

	rank := q * total

	// Find the bucket where the quantile falls
	for i, b := range buckets {
		if b.CumulativeCount < rank {
			continue
		}

		// Determine the lower bound and count of the previous bucket
		lowerBound := 0.0
		lowerCount := 0.0
		if i > 0 {
			lowerBound = buckets[i-1].UpperBound
			lowerCount = buckets[i-1].CumulativeCount
		}

		// Skip +Inf bucket for interpolation
		if math.IsInf(b.UpperBound, 1) {
			return lowerBound
		}

		// Linear interpolation within the bucket
		bucketCount := b.CumulativeCount - lowerCount
		if bucketCount == 0 {
			return lowerBound
		}

		return lowerBound + (b.UpperBound-lowerBound)*(rank-lowerCount)/bucketCount
	}

	return buckets[len(buckets)-1].UpperBound
}