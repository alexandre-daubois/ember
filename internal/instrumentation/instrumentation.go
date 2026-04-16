package instrumentation

import (
	"runtime"
	"sync/atomic"
	"time"
)

// Stage names used to label per-sub-fetch counters. They map 1:1 to the
// three sub-fetches inside HTTPFetcher.Fetch; unknown stages are dropped.
const (
	StageThreads = "threads"
	StageMetrics = "metrics"
	StageProcess = "process"
)

type stageStats struct {
	total      atomic.Uint64
	errors     atomic.Uint64
	lastDurNs  atomic.Int64
	lastSuccNs atomic.Int64 // unix nanoseconds; 0 = never
}

// Recorder accumulates scrape statistics. A nil receiver is a valid no-op,
// so callers that don't care about self-metrics need no nil-checks.
type Recorder struct {
	version   string
	goversion string

	threads stageStats
	metrics stageStats
	process stageStats
}

func New(version string) *Recorder {
	return &Recorder{
		version:   version,
		goversion: runtime.Version(),
	}
}

func (r *Recorder) Record(stage string, duration time.Duration, err error) {
	if r == nil {
		return
	}
	var s *stageStats
	switch stage {
	case StageThreads:
		s = &r.threads
	case StageMetrics:
		s = &r.metrics
	case StageProcess:
		s = &r.process
	default:
		return
	}
	s.total.Add(1)
	s.lastDurNs.Store(int64(duration))
	if err != nil {
		s.errors.Add(1)
		return
	}
	s.lastSuccNs.Store(time.Now().UnixNano())
}

type StageSnapshot struct {
	Stage         string
	Total         uint64
	Errors        uint64
	LastDuration  time.Duration
	LastSuccessAt time.Time // zero = no success yet
}

type Snapshot struct {
	Version   string
	GoVersion string
	Stages    []StageSnapshot // canonical order: metrics, process, threads
}

func (r *Recorder) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{}
	}
	return Snapshot{
		Version:   r.version,
		GoVersion: r.goversion,
		Stages: []StageSnapshot{
			snapshotStage(StageMetrics, &r.metrics),
			snapshotStage(StageProcess, &r.process),
			snapshotStage(StageThreads, &r.threads),
		},
	}
}

func snapshotStage(name string, s *stageStats) StageSnapshot {
	out := StageSnapshot{
		Stage:        name,
		Total:        s.total.Load(),
		Errors:       s.errors.Load(),
		LastDuration: time.Duration(s.lastDurNs.Load()),
	}
	if ts := s.lastSuccNs.Load(); ts > 0 {
		out.LastSuccessAt = time.Unix(0, ts)
	}
	return out
}
