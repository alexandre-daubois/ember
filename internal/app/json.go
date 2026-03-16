package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
)

type jsonOutput struct {
	Threads   fetcher.ThreadsResponse `json:"threads"`
	Metrics   fetcher.MetricsSnapshot `json:"metrics"`
	Process   fetcher.ProcessMetrics  `json:"process"`
	FetchedAt time.Time               `json:"fetchedAt"`
	Errors    []string                `json:"errors,omitempty"`
	Derived   *jsonDerived            `json:"derived,omitempty"`
}

type jsonDerived struct {
	RPS     float64  `json:"rps"`
	AvgTime float64  `json:"avgTime"`
	P50     *float64 `json:"p50,omitempty"`
	P95     *float64 `json:"p95,omitempty"`
	P99     *float64 `json:"p99,omitempty"`
}

func runJSON(ctx context.Context, f *fetcher.HTTPFetcher, interval time.Duration) {
	enc := json.NewEncoder(os.Stdout)
	var state model.State

	poll := func() {
		snap, err := f.Fetch(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			return
		}
		state.Update(snap)

		out := jsonOutput{
			Threads:   snap.Threads,
			Metrics:   snap.Metrics,
			Process:   snap.Process,
			FetchedAt: snap.FetchedAt,
			Errors:    snap.Errors,
			Derived: &jsonDerived{
				RPS:     state.Derived.RPS,
				AvgTime: state.Derived.AvgTime,
			},
		}
		if state.Derived.HasPercentiles {
			out.Derived.P50 = &state.Derived.P50
			out.Derived.P95 = &state.Derived.P95
			out.Derived.P99 = &state.Derived.P99
		}
		enc.Encode(out)
	}

	poll()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll()
		}
	}
}
