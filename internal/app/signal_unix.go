//go:build !windows

package app

import (
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"os/signal"
	"syscall"

	"github.com/alexandre-daubois/ember/internal/fetcher"
	"github.com/alexandre-daubois/ember/internal/model"
)

// dumpSignal returns a channel that receives a value each time SIGUSR1 is sent to the process.
func dumpSignal() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGUSR1)
	return ch
}

func dumpState(state *model.State, log *slog.Logger) {
	if state.Current == nil {
		log.Info("dump requested but no data available yet")
		return
	}

	out := buildJSONOutput(state.Current, state)
	sanitizeForJSON(&out)
	b, err := json.Marshal(out)
	if err != nil {
		log.Error("dump failed", "err", err)
		return
	}

	log.Info("state dump (SIGUSR1)", "snapshot", string(b))
}

// sanitizeForJSON removes +Inf and NaN values that encoding/json cannot serialize.
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
