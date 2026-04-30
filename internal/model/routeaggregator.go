package model

import (
	"sort"
	"sync"

	"github.com/alexandre-daubois/ember/internal/fetcher"
)

// RouteAggregator maintains per-route stats over the entire lifetime of an
// Ember session, independently of the access-log ring buffer. The buffer
// caps memory at the most recent 10 000 entries; without a separate
// aggregator the route counts would silently top out at the buffer
// capacity, which is misleading on a busy server.
//
// Thread-safety: Track takes the write lock and Snapshot the read lock, so
// any mix of writers and readers is safe. In production there is a single
// writer (the log tailer goroutine), which keeps contention negligible: the
// tailer batches entries so the lock is held for very short bursts.
type RouteAggregator struct {
	mu      sync.RWMutex
	buckets map[RouteKey]*RouteStat
}

func NewRouteAggregator() *RouteAggregator {
	return &RouteAggregator{buckets: make(map[RouteKey]*RouteStat, 64)}
}

// Track folds one log entry into the aggregator. Non-access logs and entries
// with empty method or URI are skipped so callers can pass every entry the
// tailer sees without pre-filtering.
func (a *RouteAggregator) Track(e fetcher.LogEntry) {
	if !e.IsAccessLog() || e.Method == "" || e.URI == "" {
		return
	}
	key := RouteKey{Host: e.Host, Method: e.Method, Pattern: NormalizeURI(e.URI)}

	a.mu.Lock()
	defer a.mu.Unlock()

	stat, ok := a.buckets[key]
	if !ok {
		stat = &RouteStat{Key: key}
		a.buckets[key] = stat
	}
	stat.Count++
	switch {
	case e.Status >= 500:
		stat.Status5xx++
	case e.Status >= 400:
		stat.Status4xx++
	case e.Status >= 300:
		stat.Status3xx++
	case e.Status >= 200:
		stat.Status2xx++
	}
	ms := e.Duration * 1000
	stat.DurationSumMs += ms
	if ms > stat.DurationMaxMs {
		stat.DurationMaxMs = ms
	}
	if e.Timestamp.After(stat.LastSeen) {
		stat.LastSeen = e.Timestamp
	}
}

// Snapshot returns an unsorted copy of every bucket. The slice is fully
// detached from the aggregator's internal state, so the caller is free to
// mutate it (callers always re-sort by their active sort key, so sorting
// here would be wasted work).
func (a *RouteAggregator) Snapshot() []RouteStat {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if len(a.buckets) == 0 {
		return nil
	}
	out := make([]RouteStat, 0, len(a.buckets))
	for _, s := range a.buckets {
		out = append(out, *s)
	}
	return out
}

// BucketCount returns the number of distinct (host, method, pattern) buckets
// currently tracked. Callers that only need the cardinality should prefer
// this over Snapshot — it avoids the per-bucket copy.
func (a *RouteAggregator) BucketCount() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.buckets)
}

// Hosts returns the set of distinct host values seen so far, sorted
// alphabetically. The sidepanel uses this as its source-of-truth so a host
// disappearing from the aggregator (after Reset) cleanly drops its child.
func (a *RouteAggregator) Hosts() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.buckets) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(a.buckets))
	for k := range a.buckets {
		if k.Host == "" {
			continue
		}
		seen[k.Host] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for h := range seen {
		out = append(out, h)
	}
	sort.Strings(out)
	return out
}

func (a *RouteAggregator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.buckets = make(map[RouteKey]*RouteStat, 64)
}
