//go:build !windows

package app

import (
	"bytes"
	"log/slog"
	"math"
	"syscall"
	"testing"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDumpState_WithData(t *testing.T) {
	snap := &fetcher.Snapshot{
		Metrics: fetcher.MetricsSnapshot{
			Workers:        map[string]*fetcher.WorkerMetrics{},
			HasHTTPMetrics: true,
			DurationBuckets: []fetcher.HistogramBucket{
				{UpperBound: 0.005, CumulativeCount: 50},
				{UpperBound: math.Inf(1), CumulativeCount: 100},
			},
		},
		Process:   fetcher.ProcessMetrics{CPUPercent: 5.0, RSS: 64 * 1024 * 1024},
		FetchedAt: time.Now(),
	}
	var state model.State
	state.Update(snap)

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	dumpState(&state, log)

	output := buf.String()
	assert.Contains(t, output, "state dump")
	assert.Contains(t, output, "cpuPercent")
}

func TestDumpState_NoData(t *testing.T) {
	var state model.State

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))

	dumpState(&state, log)

	assert.Contains(t, buf.String(), "no data")
}

func TestDumpSignal_DeliversUntilStopped(t *testing.T) {
	ch, stop := dumpSignal()
	require.NotNil(t, ch)

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR1))
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("SIGUSR1 was not delivered on the dump channel")
	}

	stop()
	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGUSR1))
	select {
	case <-ch:
		t.Fatal("the channel must go quiet once delivery is stopped")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestReloadSignal_DeliversUntilStopped(t *testing.T) {
	ch, stop := reloadSignal()
	require.NotNil(t, ch)
	defer stop()

	require.NoError(t, syscall.Kill(syscall.Getpid(), syscall.SIGHUP))
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("SIGHUP was not delivered on the reload channel")
	}
}
