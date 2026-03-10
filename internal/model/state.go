package model

import (
	"fmt"
	"time"

	"github.com/alexandredaubois/frankentop/internal/fetcher"
)

type SortField int

const (
	SortByIndex SortField = iota
	SortByState
	SortByMemory
	SortByRequests
	SortByTime
)

func (s SortField) String() string {
	switch s {
	case SortByState:
		return "state"
	case SortByMemory:
		return "memory"
	case SortByRequests:
		return "requests"
	case SortByTime:
		return "time"
	default:
		return "index"
	}
}

func (s SortField) Next() SortField {
	return (s + 1) % 5
}

type State struct {
	Current  *fetcher.Snapshot
	Previous *fetcher.Snapshot
	Derived  DerivedMetrics
}

type DerivedMetrics struct {
	RPS          float64
	AvgTime      float64
	TotalIdle    int
	TotalBusy    int
	TotalCrashes float64
}

func (s *State) Update(snap *fetcher.Snapshot) {
	s.Previous = s.Current
	s.Current = snap
	s.Derived = s.computeDerived()
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

	if s.Previous == nil {
		return d
	}

	dt := s.Current.FetchedAt.Sub(s.Previous.FetchedAt).Seconds()
	if dt <= 0 {
		return d
	}

	var currCount, prevCount, currTime, prevTime float64
	for _, w := range s.Current.Metrics.Workers {
		currCount += w.RequestCount
		currTime += w.RequestTime
	}
	for _, w := range s.Previous.Metrics.Workers {
		prevCount += w.RequestCount
		prevTime += w.RequestTime
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
