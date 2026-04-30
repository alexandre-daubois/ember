package model

import (
	"sync"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
)

func TestRouteAggregator_TrackAndSnapshot(t *testing.T) {
	agg := NewRouteAggregator()
	now := time.Now()
	// One bucket gets all four status classes so we exercise every branch
	// of the switch in Track and assert the per-class counters separately.
	agg.Track(makeAccessEntry("GET", "/users/1", 200, 0.020, now))
	agg.Track(makeAccessEntry("GET", "/users/2", 301, 0.040, now.Add(time.Second)))
	agg.Track(makeAccessEntry("GET", "/users/3", 404, 0.080, now.Add(2*time.Second)))
	agg.Track(makeAccessEntry("GET", "/users/4", 500, 0.100, now.Add(3*time.Second)))
	agg.Track(makeAccessEntry("POST", "/users", 201, 0.010, now.Add(4*time.Second)))

	snap := agg.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 buckets, got %d", len(snap))
	}
	// Snapshot is unsorted (callers always re-sort by their active key).
	SortRoutes(snap, SortByRouteCount)
	first := snap[0]
	if first.Key.Method != "GET" || first.Key.Pattern != "/users/:id" {
		t.Errorf("expected GET /users/:id first, got %+v", first.Key)
	}
	if first.Count != 4 {
		t.Errorf("Count = %d, want 4", first.Count)
	}
	if first.Status2xx != 1 || first.Status3xx != 1 || first.Status4xx != 1 || first.Status5xx != 1 {
		t.Errorf("status counters = (%d, %d, %d, %d), want (1, 1, 1, 1)",
			first.Status2xx, first.Status3xx, first.Status4xx, first.Status5xx)
	}
}

func TestRouteAggregator_SurvivesRingWraparound(t *testing.T) {
	// The point of having a separate aggregator: counts must not be capped
	// by the access buffer's size. Here we feed 10× the would-be capacity
	// and confirm the count keeps climbing.
	agg := NewRouteAggregator()
	now := time.Now()
	for i := 0; i < 100_000; i++ {
		agg.Track(makeAccessEntry("GET", "/x", 200, 0, now))
	}
	snap := agg.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 bucket, got %d", len(snap))
	}
	if snap[0].Count != 100_000 {
		t.Errorf("Count = %d, want 100000 (aggregator should not be ring-bound)", snap[0].Count)
	}
}

func TestRouteAggregator_SkipsNonAccess(t *testing.T) {
	agg := NewRouteAggregator()
	agg.Track(fetcher.LogEntry{Logger: "caddy", Message: "starting"}) // runtime
	agg.Track(makeAccessEntry("", "/foo", 200, 0, time.Now()))        // empty method
	agg.Track(makeAccessEntry("GET", "", 200, 0, time.Now()))         // empty URI
	agg.Track(fetcher.LogEntry{ParseError: true, Method: "GET", URI: "/p"})
	if got := agg.Snapshot(); len(got) != 0 {
		t.Errorf("expected empty snapshot, got %+v", got)
	}
}

func TestRouteAggregator_BucketCount(t *testing.T) {
	agg := NewRouteAggregator()
	if got := agg.BucketCount(); got != 0 {
		t.Errorf("empty aggregator BucketCount = %d, want 0", got)
	}
	now := time.Now()
	agg.Track(makeAccessEntry("GET", "/a", 200, 0, now))
	agg.Track(makeAccessEntry("GET", "/a", 200, 0, now)) // same bucket
	agg.Track(makeAccessEntry("POST", "/a", 200, 0, now))
	if got := agg.BucketCount(); got != 2 {
		t.Errorf("BucketCount after 3 tracks (2 distinct) = %d, want 2", got)
	}
	agg.Reset()
	if got := agg.BucketCount(); got != 0 {
		t.Errorf("BucketCount after Reset = %d, want 0", got)
	}
}

func TestRouteAggregator_Reset(t *testing.T) {
	agg := NewRouteAggregator()
	agg.Track(makeAccessEntry("GET", "/a", 200, 0, time.Now()))
	agg.Reset()
	if got := agg.Snapshot(); got != nil {
		t.Errorf("after Reset, Snapshot = %v, want nil", got)
	}
	// Aggregator must remain usable after Reset.
	agg.Track(makeAccessEntry("GET", "/b", 200, 0, time.Now()))
	if got := agg.Snapshot(); len(got) != 1 {
		t.Errorf("Track after Reset failed: %v", got)
	}
}

func TestRouteAggregator_Hosts(t *testing.T) {
	agg := NewRouteAggregator()
	if got := agg.Hosts(); got != nil {
		t.Errorf("empty aggregator Hosts() = %v, want nil", got)
	}

	now := time.Now()
	// Two hosts, deliberately tracked out of alphabetical order to ensure
	// the result is sorted, plus a same-host duplicate that must not
	// produce a duplicate entry, plus an empty-host bucket that must be
	// skipped (some access logs do not carry a Host field).
	agg.Track(makeAccessEntry("GET", "/x", 200, 0, now))
	for _, e := range []fetcher.LogEntry{
		{Timestamp: now, Logger: "http.log.access.log0", Host: "b.example", Method: "GET", URI: "/x", Status: 200},
		{Timestamp: now, Logger: "http.log.access.log0", Host: "a.example", Method: "GET", URI: "/x", Status: 200},
		{Timestamp: now, Logger: "http.log.access.log0", Host: "b.example", Method: "POST", URI: "/y", Status: 200},
	} {
		agg.Track(e)
	}

	got := agg.Hosts()
	want := []string{"a.example", "b.example"}
	if len(got) != len(want) {
		t.Fatalf("Hosts() = %v, want %v", got, want)
	}
	for i, h := range want {
		if got[i] != h {
			t.Errorf("Hosts()[%d] = %q, want %q", i, got[i], h)
		}
	}

	agg.Reset()
	if got := agg.Hosts(); got != nil {
		t.Errorf("Hosts() after Reset = %v, want nil", got)
	}

	// All-empty-host case: the buckets exist but none carries a Host, so
	// the sidepanel must still get nil (no spurious empty entry to draw).
	agg.Track(makeAccessEntry("GET", "/x", 200, 0, now))
	if got := agg.Hosts(); got != nil {
		t.Errorf("Hosts() with only empty-host buckets = %v, want nil", got)
	}
}

func TestRouteAggregator_ConcurrentTrackAndSnapshot(t *testing.T) {
	// Smoke test for the lock: one writer, several readers, race detector
	// catches anything wrong (`go test -race`).
	agg := NewRouteAggregator()
	now := time.Now()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Go(func() {
		for i := 0; i < 5_000; i++ {
			agg.Track(makeAccessEntry("GET", "/x", 200, 0, now))
		}
		close(stop)
	})
	for r := 0; r < 4; r++ {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_ = agg.Snapshot()
				}
			}
		})
	}
	wg.Wait()
	got := agg.Snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 bucket after concurrent run, got %d", len(got))
	}
	if got[0].Count != 5_000 {
		t.Errorf("Count after concurrent run = %d, want 5000", got[0].Count)
	}
}
