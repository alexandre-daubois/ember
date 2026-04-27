package model

import (
	"sort"
	"time"
)

// RouteKey identifies a single aggregation bucket. Host and Method are part
// of the key because two virtual hosts (or `GET` vs `DELETE`) on the same
// path are different operations with different latency profiles, and fusing
// them under one row would be misleading.
type RouteKey struct {
	Host    string
	Method  string
	Pattern string
}

// RouteStat aggregates entries that share a RouteKey. Latency is stored in
// milliseconds (LogEntry.Duration is seconds, multiplied at insertion) so
// the renderer never has to know the source unit.
type RouteStat struct {
	Key           RouteKey
	Count         int
	Status2xx     int
	Status3xx     int
	Status4xx     int
	Status5xx     int
	DurationSumMs float64
	DurationMaxMs float64
	LastSeen      time.Time
}

// AvgMs returns 0 (not NaN) when Count == 0 so callers can render the cell
// without a guard clause.
func (s RouteStat) AvgMs() float64 {
	if s.Count == 0 {
		return 0
	}
	return s.DurationSumMs / float64(s.Count)
}

// RouteSortField cycles in the visual column order (Count → Pattern → Avg →
// Max), skipping Method and the per-status counters which are not sortable.
// Last-seen is intentionally absent: the column is no longer surfaced in the
// UI, so cycling onto an invisible sort key would just confuse users.
type RouteSortField int

const (
	SortByRouteCount RouteSortField = iota
	SortByRoutePattern
	SortByRouteAvg
	SortByRouteMax
)

var routeSortFieldOrder = []RouteSortField{
	SortByRouteCount, SortByRoutePattern, SortByRouteAvg, SortByRouteMax,
}

func (s RouteSortField) String() string {
	switch s {
	case SortByRouteAvg:
		return "avg"
	case SortByRouteMax:
		return "max"
	case SortByRoutePattern:
		return "pattern"
	default:
		return "count"
	}
}

func (s RouteSortField) Next() RouteSortField {
	for i, f := range routeSortFieldOrder {
		if f == s {
			return routeSortFieldOrder[(i+1)%len(routeSortFieldOrder)]
		}
	}
	return SortByRouteCount
}

func (s RouteSortField) Prev() RouteSortField {
	for i, f := range routeSortFieldOrder {
		if f == s {
			return routeSortFieldOrder[(i-1+len(routeSortFieldOrder))%len(routeSortFieldOrder)]
		}
	}
	return SortByRouteCount
}

// SortRoutes sorts in place. Pattern is the secondary key for every numeric
// sort so neighbouring rows do not swap places between two refreshes when
// their primary value is tied.
func SortRoutes(stats []RouteStat, by RouteSortField) {
	sort.SliceStable(stats, func(i, j int) bool {
		a, b := stats[i], stats[j]
		switch by {
		case SortByRouteAvg:
			if a.AvgMs() != b.AvgMs() {
				return a.AvgMs() > b.AvgMs()
			}
		case SortByRouteMax:
			if a.DurationMaxMs != b.DurationMaxMs {
				return a.DurationMaxMs > b.DurationMaxMs
			}
		case SortByRoutePattern:
			if a.Key.Pattern != b.Key.Pattern {
				return a.Key.Pattern < b.Key.Pattern
			}
			if a.Key.Host != b.Key.Host {
				return a.Key.Host < b.Key.Host
			}
			return a.Key.Method < b.Key.Method
		default: // SortByRouteCount
			if a.Count != b.Count {
				return a.Count > b.Count
			}
		}
		if a.Key.Host != b.Key.Host {
			return a.Key.Host < b.Key.Host
		}
		if a.Key.Pattern != b.Key.Pattern {
			return a.Key.Pattern < b.Key.Pattern
		}
		return a.Key.Method < b.Key.Method
	})
}
