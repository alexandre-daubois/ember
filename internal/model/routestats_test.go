package model

import (
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
)

func makeAccessEntry(method, uri string, status int, durSec float64, ts time.Time) fetcher.LogEntry {
	return fetcher.LogEntry{
		Timestamp: ts,
		Logger:    "http.log.access.log0",
		Method:    method,
		URI:       uri,
		Status:    status,
		Duration:  durSec,
	}
}

func TestSortRoutes(t *testing.T) {
	now := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	mk := func(method, pattern string, count int, sumMs, maxMs float64, last time.Time) RouteStat {
		return RouteStat{
			Key:           RouteKey{Method: method, Pattern: pattern},
			Count:         count,
			DurationSumMs: sumMs,
			DurationMaxMs: maxMs,
			LastSeen:      last,
		}
	}
	a := mk("GET", "/a", 10, 100, 50, now.Add(1*time.Second))  // avg 10, max 50
	b := mk("GET", "/b", 5, 250, 80, now.Add(2*time.Second))   // avg 50, max 80
	c := mk("POST", "/a", 20, 200, 30, now.Add(3*time.Second)) // avg 10, max 30, latest

	t.Run("count", func(t *testing.T) {
		s := []RouteStat{a, b, c}
		SortRoutes(s, SortByRouteCount)
		if s[0].Key != c.Key || s[1].Key != a.Key || s[2].Key != b.Key {
			t.Errorf("bad order: %+v %+v %+v", s[0].Key, s[1].Key, s[2].Key)
		}
	})
	t.Run("avg", func(t *testing.T) {
		s := []RouteStat{a, b, c}
		SortRoutes(s, SortByRouteAvg)
		// b (avg 50) > a (avg 10, GET /a) > c (avg 10, POST /a) — ties broken by pattern then method.
		if s[0].Key != b.Key {
			t.Errorf("expected b first, got %+v", s[0].Key)
		}
		if s[1].Key != a.Key || s[2].Key != c.Key {
			t.Errorf("tie-break wrong: %+v %+v", s[1].Key, s[2].Key)
		}
	})
	t.Run("max", func(t *testing.T) {
		s := []RouteStat{a, b, c}
		SortRoutes(s, SortByRouteMax)
		if s[0].Key != b.Key {
			t.Errorf("expected b (max 80) first, got %+v", s[0].Key)
		}
	})
	t.Run("pattern", func(t *testing.T) {
		s := []RouteStat{a, b, c}
		SortRoutes(s, SortByRoutePattern)
		// /a < /b ; within /a, GET < POST alphabetically.
		if s[0].Key != a.Key || s[1].Key != c.Key || s[2].Key != b.Key {
			t.Errorf("bad pattern order: %+v %+v %+v", s[0].Key, s[1].Key, s[2].Key)
		}
	})
}

func TestRouteSortField_Cycle(t *testing.T) {
	// Cycle follows the visual column order: Count → Pattern → Avg → Max.
	order := []RouteSortField{SortByRouteCount, SortByRoutePattern, SortByRouteAvg, SortByRouteMax}
	// Forward and backward cycles must be inverses of each other and wrap
	// around at both ends, so we walk a full lap in each direction.
	got := SortByRouteCount
	for i := 0; i < len(order); i++ {
		want := order[(i+1)%len(order)]
		got = got.Next()
		if got != want {
			t.Errorf("Next from %v = %v, want %v", order[i], got, want)
		}
	}
	got = SortByRouteCount
	wantBack := []RouteSortField{SortByRouteMax, SortByRouteAvg, SortByRoutePattern, SortByRouteCount}
	for i, want := range wantBack {
		got = got.Prev()
		if got != want {
			t.Errorf("Prev step %d = %v, want %v", i, got, want)
		}
	}
}

func TestRouteSortField_NextPrevOnInvalid(t *testing.T) {
	// An out-of-range value must not loop forever and must fall back to the
	// default sort: callers persist the field between renders, and a future
	// removal of an enum value should not strand the cursor.
	invalid := RouteSortField(999)
	if got := invalid.Next(); got != SortByRouteCount {
		t.Errorf("Next on invalid = %v, want SortByRouteCount", got)
	}
	if got := invalid.Prev(); got != SortByRouteCount {
		t.Errorf("Prev on invalid = %v, want SortByRouteCount", got)
	}
}

func TestRouteSortField_String(t *testing.T) {
	tests := []struct {
		field RouteSortField
		want  string
	}{
		{SortByRouteCount, "count"},
		{SortByRoutePattern, "pattern"},
		{SortByRouteAvg, "avg"},
		{SortByRouteMax, "max"},
		{RouteSortField(999), "count"}, // unknown values fall back to the default
	}
	for _, tt := range tests {
		if got := tt.field.String(); got != tt.want {
			t.Errorf("RouteSortField(%d).String() = %q, want %q", tt.field, got, tt.want)
		}
	}
}

func TestSortRoutes_TieBreakOnCount(t *testing.T) {
	// Same count, different host → host is the secondary key for the
	// numeric sorts, so the order must be stable across refreshes.
	stats := []RouteStat{
		{Key: RouteKey{Host: "b.example", Method: "GET", Pattern: "/a"}, Count: 5},
		{Key: RouteKey{Host: "a.example", Method: "GET", Pattern: "/a"}, Count: 5},
	}
	SortRoutes(stats, SortByRouteCount)
	if stats[0].Key.Host != "a.example" {
		t.Errorf("expected a.example first on tie, got %q", stats[0].Key.Host)
	}
}

func TestSortRoutes_AvgFallsThroughOnZeroCount(t *testing.T) {
	// Two zero-count buckets have the same AvgMs (0), so the secondary
	// keys (host/pattern/method) must take over instead of the comparison
	// silently swapping rows.
	stats := []RouteStat{
		{Key: RouteKey{Method: "GET", Pattern: "/b"}},
		{Key: RouteKey{Method: "GET", Pattern: "/a"}},
	}
	SortRoutes(stats, SortByRouteAvg)
	if stats[0].Key.Pattern != "/a" {
		t.Errorf("expected /a first on tied avg, got %q", stats[0].Key.Pattern)
	}
}

func TestRouteStat_AvgMsZero(t *testing.T) {
	s := RouteStat{}
	if s.AvgMs() != 0 {
		t.Errorf("AvgMs on zero RouteStat = %f", s.AvgMs())
	}
}

func TestSortRoutes_PatternTieBreaksOnHostThenMethod(t *testing.T) {
	// Same pattern, different hosts and methods: the secondary keys for
	// SortByRoutePattern are Host (then Method), so the order must walk
	// alphabetically through both — we need to see Host break the tie
	// first, then Method break the remaining tie within a single host.
	stats := []RouteStat{
		{Key: RouteKey{Host: "b.example", Method: "GET", Pattern: "/x"}},
		{Key: RouteKey{Host: "a.example", Method: "POST", Pattern: "/x"}},
		{Key: RouteKey{Host: "a.example", Method: "GET", Pattern: "/x"}},
	}
	SortRoutes(stats, SortByRoutePattern)
	if stats[0].Key.Host != "a.example" || stats[0].Key.Method != "GET" {
		t.Errorf("first row = %+v, want a.example GET", stats[0].Key)
	}
	if stats[1].Key.Host != "a.example" || stats[1].Key.Method != "POST" {
		t.Errorf("second row = %+v, want a.example POST", stats[1].Key)
	}
	if stats[2].Key.Host != "b.example" {
		t.Errorf("third row = %+v, want b.example", stats[2].Key)
	}
}
