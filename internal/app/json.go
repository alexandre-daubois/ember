package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
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
	Hosts     []jsonHost              `json:"hosts,omitempty"`
}

type jsonHost struct {
	Host           string             `json:"host"`
	RPS            float64            `json:"rps"`
	AvgTime        float64            `json:"avgTime"`
	ErrorRate      float64            `json:"errorRate,omitempty"`
	InFlight       float64            `json:"inFlight"`
	P50            *float64           `json:"p50,omitempty"`
	P90            *float64           `json:"p90,omitempty"`
	P95            *float64           `json:"p95,omitempty"`
	P99            *float64           `json:"p99,omitempty"`
	TTFBP50        *float64           `json:"ttfbP50,omitempty"`
	TTFBP90        *float64           `json:"ttfbP90,omitempty"`
	TTFBP95        *float64           `json:"ttfbP95,omitempty"`
	TTFBP99        *float64           `json:"ttfbP99,omitempty"`
	StatusCodes    map[int]float64    `json:"statusCodes,omitempty"`
	MethodRates    map[string]float64 `json:"methodRates,omitempty"`
	AvgRequestSize float64            `json:"avgRequestSize,omitempty"`
}

type jsonDerived struct {
	RPS       float64  `json:"rps"`
	AvgTime   float64  `json:"avgTime"`
	ErrorRate float64  `json:"errorRate,omitempty"`
	P50       *float64 `json:"p50,omitempty"`
	P95       *float64 `json:"p95,omitempty"`
	P99       *float64 `json:"p99,omitempty"`
}

func runJSON(ctx context.Context, f fetcher.Fetcher, interval time.Duration, once bool, log *slog.Logger) {
	enc := json.NewEncoder(os.Stdout)
	var state model.State

	poll := func() {
		snap, err := f.Fetch(ctx)
		if err != nil {
			log.Error("fetch failed", "err", err)
			return
		}
		state.Update(snap)
		_ = enc.Encode(buildJSONOutput(snap, &state))
	}

	poll()

	if once {
		return
	}

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

func buildJSONOutput(snap *fetcher.Snapshot, state *model.State) jsonOutput {
	out := jsonOutput{
		Threads:   snap.Threads,
		Metrics:   snap.Metrics,
		Process:   snap.Process,
		FetchedAt: snap.FetchedAt,
		Errors:    snap.Errors,
		Derived: &jsonDerived{
			RPS:       state.Derived.RPS,
			AvgTime:   state.Derived.AvgTime,
			ErrorRate: state.Derived.ErrorRate,
		},
	}
	if state.Derived.HasPercentiles {
		out.Derived.P50 = &state.Derived.P50
		out.Derived.P95 = &state.Derived.P95
		out.Derived.P99 = &state.Derived.P99
	}
	for _, hd := range state.HostDerived {
		jh := jsonHost{
			Host:           hd.Host,
			RPS:            hd.RPS,
			AvgTime:        hd.AvgTime,
			ErrorRate:      hd.ErrorRate,
			InFlight:       hd.InFlight,
			StatusCodes:    hd.StatusCodes,
			MethodRates:    hd.MethodRates,
			AvgRequestSize: hd.AvgRequestSize,
		}
		if hd.HasPercentiles {
			jh.P50 = &hd.P50
			jh.P90 = &hd.P90
			jh.P95 = &hd.P95
			jh.P99 = &hd.P99
		}
		if hd.HasTTFB {
			jh.TTFBP50 = &hd.TTFBP50
			jh.TTFBP90 = &hd.TTFBP90
			jh.TTFBP95 = &hd.TTFBP95
			jh.TTFBP99 = &hd.TTFBP99
		}
		out.Hosts = append(out.Hosts, jh)
	}

	sanitizeForJSON(&out)

	return out
}

func sanitizeForJSON(out *jsonOutput) {
	out.Metrics.DurationBuckets = sanitizeBuckets(out.Metrics.DurationBuckets)
	for _, h := range out.Metrics.Hosts {
		h.DurationBuckets = sanitizeBuckets(h.DurationBuckets)
		h.TTFBBuckets = sanitizeBuckets(h.TTFBBuckets)
	}
}

func sanitizeBuckets(buckets []fetcher.HistogramBucket) []fetcher.HistogramBucket {
	clean := make([]fetcher.HistogramBucket, 0, len(buckets))
	for _, b := range buckets {
		if math.IsInf(b.UpperBound, 0) || math.IsNaN(b.UpperBound) {
			continue
		}
		clean = append(clean, b)
	}
	return clean
}
