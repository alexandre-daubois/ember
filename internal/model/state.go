package model

import (
	"fmt"
	"time"

	"github.com/alexandredaubois/ember/internal/fetcher"
)

const percentileExpiry = DefaultPercentileExpiry

type SortField int

const (
	SortByIndex SortField = iota
	SortByState
	SortByMethod
	SortByURI
	SortByTime
	SortByMemory
	SortByRequests
)

var sortFieldOrder = []SortField{SortByIndex, SortByState, SortByMethod, SortByURI, SortByTime, SortByMemory, SortByRequests}

func (s SortField) String() string {
	switch s {
	case SortByState:
		return "state"
	case SortByMethod:
		return "method"
	case SortByURI:
		return "uri"
	case SortByTime:
		return "time"
	case SortByMemory:
		return "memory"
	case SortByRequests:
		return "requests"
	default:
		return "index"
	}
}

func (s SortField) Next() SortField {
	for i, f := range sortFieldOrder {
		if f == s {
			return sortFieldOrder[(i+1)%len(sortFieldOrder)]
		}
	}
	return SortByIndex
}

func (s SortField) Prev() SortField {
	for i, f := range sortFieldOrder {
		if f == s {
			return sortFieldOrder[(i-1+len(sortFieldOrder))%len(sortFieldOrder)]
		}
	}
	return SortByIndex
}

type State struct {
	Current     *fetcher.Snapshot
	Previous    *fetcher.Snapshot
	Derived     DerivedMetrics
	Percentiles *PercentileTracker
}

type DerivedMetrics struct {
	RPS            float64
	AvgTime        float64
	TotalIdle      int
	TotalBusy      int
	TotalCrashes   float64
	P50            float64
	P95            float64
	P99            float64
	HasPercentiles bool
}

func (s *State) Update(snap *fetcher.Snapshot) {
	if s.Percentiles == nil {
		s.Percentiles = NewPercentileTracker(percentileExpiry)
	}
	s.detectCompletedRequests(snap)
	s.Previous = s.Current
	s.Current = snap
	s.Derived = s.computeDerived()
}

func (s *State) detectCompletedRequests(newSnap *fetcher.Snapshot) {
	if s.Current == nil {
		return
	}

	prevByIndex := make(map[int]fetcher.ThreadDebugState, len(s.Current.Threads.ThreadDebugStates))
	for _, t := range s.Current.Threads.ThreadDebugStates {
		prevByIndex[t.Index] = t
	}

	for _, curr := range newSnap.Threads.ThreadDebugStates {
		prev, ok := prevByIndex[curr.Index]
		if !ok || !prev.IsBusy || prev.RequestStartedAt <= 0 {
			continue
		}

		completed := !curr.IsBusy || curr.RequestStartedAt != prev.RequestStartedAt
		if completed {
			// Estimate when the request ended: midpoint between the two polls
			// reduces max error from interval to interval/2.
			// If the request started after the midpoint (short-lived request),
			// fall back to currentFetchedAt as end estimate.
			endEstimate := (s.Current.FetchedAt.UnixMilli() + newSnap.FetchedAt.UnixMilli()) / 2
			if prev.RequestStartedAt >= endEstimate {
				endEstimate = newSnap.FetchedAt.UnixMilli()
			}
			durationMs := float64(endEstimate - prev.RequestStartedAt)
			s.Percentiles.Record(durationMs, newSnap.FetchedAt)
		}
	}
}

func (s *State) computeDerived() DerivedMetrics {
	if s.Current == nil {
		return DerivedMetrics{}
	}

	var d DerivedMetrics

	for _, t := range s.Current.Threads.ThreadDebugStates {
		if t.IsBusy {
			d.TotalBusy++
		} else if t.IsWaiting {
			d.TotalIdle++
		}
	}

	for _, w := range s.Current.Metrics.Workers {
		d.TotalCrashes += w.Crashes
	}

	// Percentiles: prefer Prometheus histogram buckets, fall back to thread-based tracker
	if s.Previous != nil && len(s.Current.Metrics.DurationBuckets) > 0 && len(s.Previous.Metrics.DurationBuckets) > 0 {
		p50, p95, p99, ok := HistogramPercentiles(s.Previous.Metrics.DurationBuckets, s.Current.Metrics.DurationBuckets)
		if ok {
			d.P50 = p50
			d.P95 = p95
			d.P99 = p99
			d.HasPercentiles = true
		}
	} else if s.Percentiles != nil {
		p50, p95, p99, ok := s.Percentiles.Percentiles(s.Current.FetchedAt)
		if ok {
			d.P50 = p50
			d.P95 = p95
			d.P99 = p99
			d.HasPercentiles = true
		}
	}

	if s.Previous == nil {
		return d
	}

	dt := s.Current.FetchedAt.Sub(s.Previous.FetchedAt).Seconds()
	if dt < 0.1 {
		return d
	}

	// try FrankenPHP worker metrics first
	var currCount, prevCount, currTime, prevTime float64
	for _, w := range s.Current.Metrics.Workers {
		currCount += w.RequestCount
		currTime += w.RequestTime
	}
	for _, w := range s.Previous.Metrics.Workers {
		prevCount += w.RequestCount
		prevTime += w.RequestTime
	}

	// fallback to Caddy HTTP metrics if no FrankenPHP worker metrics
	if currCount == 0 && s.Current.Metrics.HTTPRequestDurationCount > 0 {
		currCount = s.Current.Metrics.HTTPRequestDurationCount
		currTime = s.Current.Metrics.HTTPRequestDurationSum
		prevCount = s.Previous.Metrics.HTTPRequestDurationCount
		prevTime = s.Previous.Metrics.HTTPRequestDurationSum
	}

	// if either snapshot had no metrics data (fetch failed for that tick),
	// the delta is meaningless, so skip rate calculations
	if prevCount == 0 || currCount == 0 {
		return d
	}

	deltaCount := currCount - prevCount
	deltaTime := currTime - prevTime

	if deltaCount > 0 {
		d.RPS = deltaCount / dt
		d.AvgTime = (deltaTime / deltaCount) * 1000 // ms
	}

	return d
}

func FormatUptime(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	mins := int(d.Minutes()) % 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}
