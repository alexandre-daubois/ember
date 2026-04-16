package instrumentation

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecorder_NilSafe(t *testing.T) {
	var r *Recorder
	r.Record(StageThreads, time.Second, nil)
	assert.Equal(t, Snapshot{}, r.Snapshot())
}

func TestRecorder_NewSnapshotIsZeroValued(t *testing.T) {
	r := New("1.2.3")
	snap := r.Snapshot()
	assert.Equal(t, "1.2.3", snap.Version)
	assert.Equal(t, runtime.Version(), snap.GoVersion)
	require.Len(t, snap.Stages, 3)
	for _, s := range snap.Stages {
		assert.Zero(t, s.Total)
		assert.Zero(t, s.Errors)
		assert.True(t, s.LastSuccessAt.IsZero())
	}
}

func TestRecorder_RecordSuccessAndError(t *testing.T) {
	r := New("v")
	before := time.Now()
	r.Record(StageMetrics, 250*time.Millisecond, nil)
	r.Record(StageThreads, 50*time.Millisecond, errors.New("boom"))

	snap := r.Snapshot()
	metrics := findStage(t, snap, StageMetrics)
	assert.Equal(t, uint64(1), metrics.Total)
	assert.Zero(t, metrics.Errors)
	assert.Equal(t, 250*time.Millisecond, metrics.LastDuration)
	assert.False(t, metrics.LastSuccessAt.Before(before))

	threads := findStage(t, snap, StageThreads)
	assert.Equal(t, uint64(1), threads.Total, "total counts failures too")
	assert.Equal(t, uint64(1), threads.Errors)
	assert.Equal(t, 50*time.Millisecond, threads.LastDuration)
	assert.True(t, threads.LastSuccessAt.IsZero(), "no success yet")
}

func TestRecorder_UnknownStageDropped(t *testing.T) {
	r := New("v")
	r.Record("ghost", time.Second, nil)
	for _, s := range r.Snapshot().Stages {
		assert.Zero(t, s.Total, "stage %q must remain at zero", s.Stage)
	}
}

func TestRecorder_ConcurrentRecordIsRaceFree(t *testing.T) {
	r := New("v")

	const goroutines = 16
	const perGoroutine = 200
	stages := []string{StageThreads, StageMetrics, StageProcess}

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		stage := stages[g%len(stages)]
		go func() {
			defer wg.Done()
			for i := range perGoroutine {
				var err error
				if i%5 == 0 {
					err = errors.New("x")
				}
				r.Record(stage, time.Duration(i)*time.Microsecond, err)
			}
		}()
	}
	wg.Wait()

	var total, errs uint64
	for _, s := range r.Snapshot().Stages {
		total += s.Total
		errs += s.Errors
	}
	assert.Equal(t, uint64(goroutines*perGoroutine), total)
	assert.Equal(t, uint64(goroutines*perGoroutine/5), errs)
}

func findStage(t *testing.T, snap Snapshot, name string) StageSnapshot {
	t.Helper()
	for _, s := range snap.Stages {
		if s.Stage == name {
			return s
		}
	}
	t.Fatalf("stage %q not found", name)
	return StageSnapshot{}
}
